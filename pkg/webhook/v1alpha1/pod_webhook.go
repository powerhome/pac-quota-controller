/*
Copyright 2025.

Licensed under the Apache License, Version 2.0 (the "License");
you may not use this file except in compliance with the License.
You may obtain a copy of the License at

    http://www.apache.org/licenses/LICENSE-2.0

Unless required by applicable law or agreed to in writing, software
distributed under the License is distributed on an "AS IS" BASIS,
WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
See the License for the specific language governing permissions and
limitations under the License.
*/

package v1alpha1

import (
	"context"
	"fmt"
	"net/http"

	"github.com/gin-gonic/gin"
	"go.uber.org/zap"
	admissionv1 "k8s.io/api/admission/v1"
	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/resource"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/runtime/serializer"
	"k8s.io/client-go/kubernetes"

	"github.com/powerhome/pac-quota-controller/pkg/kubernetes/pod"
	"github.com/powerhome/pac-quota-controller/pkg/kubernetes/quota"
	"github.com/powerhome/pac-quota-controller/pkg/kubernetes/usage"
)

// PodWebhook handles webhook requests for Pod resources
type PodWebhook struct {
	client        kubernetes.Interface
	podCalculator *pod.PodResourceCalculator
	crqClient     *quota.CRQClient
	log           *zap.Logger
}

// NewPodWebhook creates a new PodWebhook
func NewPodWebhook(c kubernetes.Interface, log *zap.Logger) *PodWebhook {
	return &PodWebhook{
		client:        c,
		podCalculator: pod.NewPodResourceCalculator(c),
		crqClient:     nil, // Will be set when controller-runtime client is available
		log:           log,
	}
}

// Handle handles the webhook request for Pod
func (h *PodWebhook) Handle(c *gin.Context) {
	var admissionReview admissionv1.AdmissionReview
	if err := c.ShouldBindJSON(&admissionReview); err != nil {
		h.log.Error("Failed to bind admission review", zap.Error(err))
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}

	// Check for malformed requests (like {}) that don't have proper AdmissionReview structure
	if admissionReview.Kind == "" && admissionReview.APIVersion == "" && admissionReview.Request == nil {
		h.log.Error("Malformed admission review request")
		c.JSON(http.StatusBadRequest, gin.H{"error": "Malformed admission review request"})
		return
	}

	// Validate the request first
	if admissionReview.Request == nil {
		h.log.Error("Admission review request is nil")
		admissionReview.Response = &admissionv1.AdmissionResponse{
			Allowed: false,
			Result: &metav1.Status{
				Message: "Admission review request is nil",
			},
		}
		c.JSON(http.StatusOK, admissionReview)
		return
	}

	// Set the response type
	admissionReview.Response = &admissionv1.AdmissionResponse{
		UID: admissionReview.Request.UID,
	}

	// Check if this is for the correct resource
	expectedGVK := metav1.GroupVersionKind{
		Group:   "",
		Version: "v1",
		Kind:    "Pod",
	}
	if admissionReview.Request.Kind != expectedGVK {
		h.log.Error("Unexpected resource kind",
			zap.String("expected", fmt.Sprintf("%v", expectedGVK)),
			zap.String("got", fmt.Sprintf("%v", admissionReview.Request.Kind)))
		admissionReview.Response.Allowed = false
		admissionReview.Response.Result = &metav1.Status{
			Message: fmt.Sprintf("Unexpected resource kind: expected %v, got %v", expectedGVK, admissionReview.Request.Kind),
		}
		c.JSON(http.StatusOK, admissionReview)
		return
	}

	// Decode the object
	var podObj corev1.Pod
	if err := runtime.DecodeInto(
		serializer.NewCodecFactory(runtime.NewScheme()).UniversalDeserializer(),
		admissionReview.Request.Object.Raw,
		&podObj,
	); err != nil {
		h.log.Error("Failed to decode Pod", zap.Error(err))
		admissionReview.Response.Allowed = false
		admissionReview.Response.Result = &metav1.Status{
			Message: fmt.Sprintf("Failed to decode Pod: %v", err),
		}
		c.JSON(http.StatusOK, admissionReview)
		return
	}

	// Validate based on operation
	var warnings []string
	var err error

	switch admissionReview.Request.Operation {
	case admissionv1.Create:
		h.log.Info("Validating Pod on create",
			zap.String("name", podObj.GetName()),
			zap.String("namespace", podObj.GetNamespace()))
		warnings, err = h.validateCreate(&podObj)
	case admissionv1.Update:
		h.log.Info("Validating Pod on update",
			zap.String("name", podObj.GetName()),
			zap.String("namespace", podObj.GetNamespace()))
		warnings, err = h.validateUpdate(&podObj)
	default:
		h.log.Error("Unsupported operation", zap.String("operation", string(admissionReview.Request.Operation)))
		admissionReview.Response.Allowed = false
		admissionReview.Response.Result = &metav1.Status{
			Message: fmt.Sprintf("Unsupported operation: %s", admissionReview.Request.Operation),
		}
		c.JSON(http.StatusOK, admissionReview)
		return
	}

	if err != nil {
		h.log.Error("Validation failed", zap.Error(err))
		admissionReview.Response.Allowed = false
		admissionReview.Response.Result = &metav1.Status{
			Message: err.Error(),
		}
	} else {
		admissionReview.Response.Allowed = true
		if len(warnings) > 0 {
			admissionReview.Response.Warnings = warnings
		}
	}

	c.JSON(http.StatusOK, admissionReview)
}

func (h *PodWebhook) validateCreate(podObj *corev1.Pod) ([]string, error) {
	return h.validatePodOperation(podObj, "creation")
}

func (h *PodWebhook) validateUpdate(podObj *corev1.Pod) ([]string, error) {
	return h.validatePodOperation(podObj, "update")
}

// validatePodOperation is a shared function for both create and update validation
func (h *PodWebhook) validatePodOperation(podObj *corev1.Pod, operation string) ([]string, error) {
	// Handle nil pod case
	if podObj == nil {
		h.log.Info("Skipping CRQ validation for nil pod on " + operation)
		return nil, nil
	}

	// Calculate the resource usage for this pod
	podUsage := pod.CalculatePodUsage(podObj, usage.ResourceRequestsCPU)
	if !podUsage.IsZero() {
		// Validate CPU requests
		if err := h.validateResourceQuota(podObj.Namespace, usage.ResourceRequestsCPU, podUsage); err != nil {
			return nil, fmt.Errorf("CPU requests quota validation failed: %w", err)
		}
	}

	podUsage = pod.CalculatePodUsage(podObj, usage.ResourceRequestsMemory)
	if !podUsage.IsZero() {
		// Validate memory requests
		if err := h.validateResourceQuota(podObj.Namespace, usage.ResourceRequestsMemory, podUsage); err != nil {
			return nil, fmt.Errorf("memory requests quota validation failed: %w", err)
		}
	}

	podUsage = pod.CalculatePodUsage(podObj, usage.ResourceLimitsCPU)
	if !podUsage.IsZero() {
		// Validate CPU limits
		if err := h.validateResourceQuota(podObj.Namespace, usage.ResourceLimitsCPU, podUsage); err != nil {
			return nil, fmt.Errorf("CPU limits quota validation failed: %w", err)
		}
	}

	podUsage = pod.CalculatePodUsage(podObj, usage.ResourceLimitsMemory)
	if !podUsage.IsZero() {
		// Validate memory limits
		if err := h.validateResourceQuota(podObj.Namespace, usage.ResourceLimitsMemory, podUsage); err != nil {
			return nil, fmt.Errorf("memory limits quota validation failed: %w", err)
		}
	}

	// Validate pod count (always 1 for a new pod)
	podCount := resource.NewQuantity(1, resource.DecimalSI)
	if err := h.validateResourceQuota(podObj.Namespace, usage.ResourcePods, *podCount); err != nil {
		return nil, fmt.Errorf("pod count quota validation failed: %w", err)
	}

	h.log.Info("Pod CRQ validation passed",
		zap.String("pod", podObj.Name),
		zap.String("namespace", podObj.Namespace),
		zap.String("operation", operation))
	return nil, nil
}

// validateResourceQuota validates if a resource operation would exceed any applicable ClusterResourceQuota
func (h *PodWebhook) validateResourceQuota(
	namespace string,
	resourceName corev1.ResourceName,
	requestedQuantity resource.Quantity,
) error {
	// Fetch the actual namespace object with labels
	ns, err := h.client.CoreV1().Namespaces().Get(context.Background(), namespace, metav1.GetOptions{})
	if err != nil {
		return fmt.Errorf("failed to get namespace %s: %w", namespace, err)
	}

	return validateCRQResourceQuotaWithNamespace(h.crqClient, ns, resourceName, requestedQuantity,
		h.calculateCurrentUsage, h.log)
}

// calculateCurrentUsage calculates the current usage of a resource in a namespace
func (h *PodWebhook) calculateCurrentUsage(namespace string,
	resourceName corev1.ResourceName) (resource.Quantity, error) {
	switch resourceName {
	case usage.ResourceRequestsCPU, usage.ResourceRequestsMemory, usage.ResourceLimitsCPU, usage.ResourceLimitsMemory:
		return h.podCalculator.CalculateUsage(context.Background(), namespace, resourceName)
	case usage.ResourcePods:
		count, err := h.podCalculator.CalculatePodCount(context.Background(), namespace)
		if err != nil {
			return resource.Quantity{}, err
		}
		return *resource.NewQuantity(count, resource.DecimalSI), nil
	default:
		return resource.Quantity{}, fmt.Errorf("unsupported resource type: %s", resourceName)
	}
}

// SetCRQClient sets the CRQ client for quota validation
func (h *PodWebhook) SetCRQClient(crqClient *quota.CRQClient) {
	h.crqClient = crqClient
}
