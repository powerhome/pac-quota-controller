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
	"fmt"
	"net/http"

	"github.com/gin-gonic/gin"
	"go.uber.org/zap"
	admissionv1 "k8s.io/api/admission/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/runtime/serializer"
	"k8s.io/client-go/kubernetes"

	quotav1alpha1 "github.com/powerhome/pac-quota-controller/api/v1alpha1"
	"github.com/powerhome/pac-quota-controller/pkg/kubernetes/namespace"
)

// ClusterResourceQuotaWebhook handles webhook requests for ClusterResourceQuota resources
type ClusterResourceQuotaWebhook struct {
	client kubernetes.Interface
	log    *zap.Logger
}

// NewClusterResourceQuotaWebhook creates a new ClusterResourceQuotaWebhook
func NewClusterResourceQuotaWebhook(c kubernetes.Interface, log *zap.Logger) *ClusterResourceQuotaWebhook {
	return &ClusterResourceQuotaWebhook{
		client: c,
		log:    log,
	}
}

// Handle handles the webhook request for ClusterResourceQuota
func (h *ClusterResourceQuotaWebhook) Handle(c *gin.Context) {
	var admissionReview admissionv1.AdmissionReview
	if err := c.ShouldBindJSON(&admissionReview); err != nil {
		h.log.Error("Failed to bind admission review", zap.Error(err))
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
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
		Group:   "quota.powerapp.cloud",
		Version: "v1alpha1",
		Kind:    "ClusterResourceQuota",
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
	var crq quotav1alpha1.ClusterResourceQuota
	if err := runtime.DecodeInto(
		serializer.NewCodecFactory(runtime.NewScheme()).UniversalDeserializer(),
		admissionReview.Request.Object.Raw,
		&crq,
	); err != nil {
		h.log.Error("Failed to decode ClusterResourceQuota", zap.Error(err))
		admissionReview.Response.Allowed = false
		admissionReview.Response.Result = &metav1.Status{
			Message: fmt.Sprintf("Failed to decode ClusterResourceQuota: %v", err),
		}
		c.JSON(http.StatusOK, admissionReview)
		return
	}

	// Validate based on operation
	var warnings []string
	var err error

	switch admissionReview.Request.Operation {
	case admissionv1.Create:
		h.log.Info("Validating ClusterResourceQuota on create",
			zap.String("name", crq.GetName()))
		warnings, err = h.validateCreate(&crq)
	case admissionv1.Update:
		h.log.Info("Validating ClusterResourceQuota on update",
			zap.String("name", crq.GetName()))
		warnings, err = h.validateUpdate(&crq)
	case admissionv1.Delete:
		h.log.Info("Validating ClusterResourceQuota on delete",
			zap.String("name", crq.GetName()))
		warnings, err = h.validateDelete()
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

func (h *ClusterResourceQuotaWebhook) validateCreate(
	crq *quotav1alpha1.ClusterResourceQuota,
) ([]string, error) {
	return namespace.ValidateNamespaceOwnership(h.client, crq)
}

func (h *ClusterResourceQuotaWebhook) validateUpdate(
	crq *quotav1alpha1.ClusterResourceQuota,
) ([]string, error) {
	return namespace.ValidateNamespaceOwnership(h.client, crq)
}

func (h *ClusterResourceQuotaWebhook) validateDelete() ([]string, error) {
	// No validation needed for delete operations
	return nil, nil
}
