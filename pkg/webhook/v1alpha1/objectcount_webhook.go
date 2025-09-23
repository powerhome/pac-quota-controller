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
	"k8s.io/client-go/kubernetes"

	"github.com/powerhome/pac-quota-controller/pkg/kubernetes/objectcount"
	"github.com/powerhome/pac-quota-controller/pkg/kubernetes/quota"
	"k8s.io/apimachinery/pkg/api/resource"
)

// ObjectCountWebhook handles webhook requests for Object count resources
// It enforces object count quotas for objects and subtypes.
type ObjectCountWebhook struct {
	client                kubernetes.Interface
	objectCountCalculator *objectcount.ObjectCountCalculator
	crqClient             *quota.CRQClient
	log                   *zap.Logger
}

// NewObjectCountWebhook creates a new ObjectCountWebhook
func NewObjectCountWebhook(
	k8sClient kubernetes.Interface,
	crqClient *quota.CRQClient,
	log *zap.Logger) *ObjectCountWebhook {
	return &ObjectCountWebhook{
		client:                k8sClient,
		objectCountCalculator: objectcount.NewObjectCountCalculator(k8sClient),
		crqClient:             crqClient,
		log:                   log,
	}
}

// Handle handles the webhook request for Object
func (h *ObjectCountWebhook) Handle(c *gin.Context) {
	var admissionReview admissionv1.AdmissionReview
	if err := c.ShouldBindJSON(&admissionReview); err != nil {
		h.log.Error("Failed to bind admission review", zap.Error(err))
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}
	h.log.Info("Received request for ObjectCountWebhook", zap.String("resource", admissionReview.Request.Resource.Resource))

	// Check for malformed requests (like {}) that don't have proper AdmissionReview structure
	if admissionReview.Kind == "" && admissionReview.APIVersion == "" && admissionReview.Request == nil {
		h.log.Error("Malformed admission review request")
		c.JSON(http.StatusBadRequest, gin.H{"error": "Malformed admission review request"})
		return
	}

	// Check for missing namespace in the request
	if admissionReview.Request.Namespace == "" {
		h.log.Info("Admission review request namespace is empty")
		admissionReview.Response = &admissionv1.AdmissionResponse{
			Allowed: false,
			Result: &metav1.Status{
				Code:    http.StatusBadRequest,
				Message: "Missing admission request namespace",
			},
		}
		c.JSON(http.StatusOK, admissionReview)
		return
	}

	admissionReview.Response = &admissionv1.AdmissionResponse{
		UID: admissionReview.Request.UID,
	}

	crqKey := admissionReview.Request.Resource.Resource
	if admissionReview.Request.Resource.Group != "" {
		crqKey = crqKey + "." + admissionReview.Request.Resource.Group
	}
	resourceName := corev1.ResourceName(crqKey)
	h.log.Info("Determined resource name for CRQ", zap.String("resourceName", string(resourceName)))
	namespace := admissionReview.Request.Namespace
	var warnings []string
	var err error
	ctx := c.Request.Context()
	switch admissionReview.Request.Operation {
	case admissionv1.Create:
		h.log.Info("Validating ObjectCount on create",
			zap.String("name", resourceName.String()),
			zap.String("namespace", namespace))
		warnings, err = h.validateCreate(ctx, namespace, resourceName)
	case admissionv1.Update:
		h.log.Info("Validating ObjectCount on update",
			zap.String("name", resourceName.String()),
			zap.String("namespace", namespace))
		warnings, err = h.validateUpdate(ctx, namespace, resourceName)
	default:
		h.log.Info("Unsupported operation", zap.String("operation", string(admissionReview.Request.Operation)))
		admissionReview.Response.Allowed = false
		admissionReview.Response.Result = &metav1.Status{
			Code:    http.StatusBadRequest,
			Message: fmt.Sprintf("Operation %s is not supported for Service", admissionReview.Request.Operation),
		}
		c.JSON(http.StatusOK, admissionReview)
		return
	}
	if err != nil {
		h.log.Error("Validation failed", zap.Error(err))
		admissionReview.Response.Allowed = false
		admissionReview.Response.Result = &metav1.Status{
			Code:    http.StatusForbidden,
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

func (h *ObjectCountWebhook) validateCreate(ctx context.Context, namespace string, resourceName corev1.ResourceName) ([]string, error) {
	return h.validateObjectOperation(ctx, namespace, resourceName, "creation")
}

func (h *ObjectCountWebhook) validateUpdate(ctx context.Context, namespace string, resourceName corev1.ResourceName) ([]string, error) {
	return h.validateObjectOperation(ctx, namespace, resourceName, "update")
}

// validateObjectOperation is a shared function for both create and update validation
func (h *ObjectCountWebhook) validateObjectOperation(ctx context.Context, namespace string, resourceName corev1.ResourceName, operation string) ([]string, error) {
	if resourceName == "" {
		h.log.Info("Skipping CRQ validation for nil object on " + operation)
		return nil, nil
	}
	err := h.validateResourceQuota(ctx, namespace, resourceName, resource.MustParse("1"))
	if err != nil {
		return nil, err
	}
	h.log.Info("Object CRQ validation passed",
		zap.String("object", resourceName.String()),
		zap.String("namespace", namespace),
		zap.String("operation", operation))
	return nil, nil
}

// validateResourceQuota validates if a resource operation would exceed any applicable ClusterResourceQuota
func (h *ObjectCountWebhook) validateResourceQuota(
	ctx context.Context,
	namespace string,
	resourceName corev1.ResourceName,
	requestedQuantity resource.Quantity,
) error {
	ns, err := h.client.CoreV1().Namespaces().Get(ctx, namespace, metav1.GetOptions{})
	if err != nil {
		return fmt.Errorf("failed to get namespace %s: %w", namespace, err)
	}

	return validateCRQResourceQuotaWithNamespace(ctx, h.crqClient, h.client, ns, resourceName, requestedQuantity,
		func(ns string, rn corev1.ResourceName) (resource.Quantity, error) {
			return h.calculateCurrentUsage(ctx, ns, rn)
		}, h.log)
}

// calculateCurrentUsage calculates the current usage of a resource in a namespace
func (h *ObjectCountWebhook) calculateCurrentUsage(ctx context.Context, namespace string,
	resourceName corev1.ResourceName) (resource.Quantity, error) {
	return h.objectCountCalculator.CalculateUsage(ctx, namespace, resourceName)
}
