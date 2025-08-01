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
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/runtime/serializer"
	"k8s.io/client-go/kubernetes"
)

// NamespaceWebhook handles webhook requests for Namespace resources
type NamespaceWebhook struct {
	client kubernetes.Interface
	log    *zap.Logger
}

// NewNamespaceWebhook creates a new NamespaceWebhook
func NewNamespaceWebhook(c kubernetes.Interface, log *zap.Logger) *NamespaceWebhook {
	return &NamespaceWebhook{
		client: c,
		log:    log,
	}
}

// Handle handles the webhook request for Namespace
func (h *NamespaceWebhook) Handle(c *gin.Context) {
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
		Group:   "",
		Version: "v1",
		Kind:    "Namespace",
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
	var namespace corev1.Namespace
	if err := runtime.DecodeInto(
		serializer.NewCodecFactory(runtime.NewScheme()).UniversalDeserializer(),
		admissionReview.Request.Object.Raw,
		&namespace,
	); err != nil {
		h.log.Error("Failed to decode Namespace", zap.Error(err))
		admissionReview.Response.Allowed = false
		admissionReview.Response.Result = &metav1.Status{
			Message: fmt.Sprintf("Failed to decode Namespace: %v", err),
		}
		c.JSON(http.StatusOK, admissionReview)
		return
	}

	// Validate based on operation
	var warnings []string
	var err error

	switch admissionReview.Request.Operation {
	case admissionv1.Create:
		h.log.Info("Validating Namespace on create",
			zap.String("name", namespace.GetName()))
		err = h.validateCreate(c.Request.Context(), &namespace)
	case admissionv1.Update:
		h.log.Info("Validating Namespace on update",
			zap.String("name", namespace.GetName()))
		err = h.validateUpdate(c.Request.Context(), &namespace)
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

//nolint:unparam // This function will be implemented in the future
func (h *NamespaceWebhook) validateCreate(_ context.Context, namespace *corev1.Namespace) error {
	// For now, skip CRQ validation since we're removing controller-runtime dependencies
	// TODO: Implement CRQ validation using native Kubernetes client when needed
	h.log.Info("Skipping CRQ validation for namespace create",
		zap.String("namespace", namespace.Name))
	// This function will be implemented in the future
	return nil
}

//nolint:unparam // This function will be implemented in the future
func (h *NamespaceWebhook) validateUpdate(_ context.Context, namespace *corev1.Namespace) error {
	// For now, skip CRQ validation since we're removing controller-runtime dependencies
	// TODO: Implement CRQ validation using native Kubernetes client when needed
	h.log.Info("Skipping CRQ validation for namespace update",
		zap.String("namespace", namespace.Name))
	// This function will be implemented in the future
	return nil
}
