package v1alpha1

import (
	"context"
	"fmt"
	"net/http"

	"github.com/gin-gonic/gin"
	"go.uber.org/zap"
	admissionv1 "k8s.io/api/admission/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/runtime/serializer"
	"k8s.io/client-go/kubernetes"

	"github.com/powerhome/pac-quota-controller/pkg/kubernetes/quota"
	"github.com/powerhome/pac-quota-controller/pkg/kubernetes/services"
	"github.com/powerhome/pac-quota-controller/pkg/kubernetes/usage"
	"github.com/powerhome/pac-quota-controller/pkg/metrics"
	"github.com/prometheus/client_golang/prometheus"
	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/resource"
)

// ServiceWebhook handles webhook requests for Service resources
// It enforces object count quotas for services and subtypes.
type ServiceWebhook struct {
	client            kubernetes.Interface
	serviceCalculator services.ServiceResourceCalculator
	crqClient         *quota.CRQClient
	log               *zap.Logger
}

// NewServiceWebhook creates a new ServiceWebhook
func NewServiceWebhook(k8sClient kubernetes.Interface, crqClient *quota.CRQClient, log *zap.Logger) *ServiceWebhook {
	return &ServiceWebhook{
		client:            k8sClient,
		serviceCalculator: *services.NewServiceResourceCalculator(k8sClient),
		crqClient:         crqClient,
		log:               log,
	}
}

// Handle handles the webhook request for Service
func (h *ServiceWebhook) Handle(c *gin.Context) {
	var admissionReview admissionv1.AdmissionReview
	if err := c.ShouldBindJSON(&admissionReview); err != nil {
		h.log.Error("Failed to bind admission review", zap.Error(err))
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}

	// Metrics: start timer and increment validation count
	operation := string(admissionReview.Request.Operation)
	webhookName := "service"
	metrics.WebhookValidationCount.WithLabelValues(webhookName, operation).Inc()
	timer := prometheus.NewTimer(metrics.WebhookValidationDuration.WithLabelValues(webhookName, operation))
	defer timer.ObserveDuration()

	// Check for malformed requests (like {}) that don't have proper AdmissionReview structure
	if admissionReview.Kind == "" && admissionReview.APIVersion == "" && admissionReview.Request == nil {
		h.log.Error("Malformed admission review request")
		c.JSON(http.StatusBadRequest, gin.H{"error": "Malformed admission review request"})
		return
	}

	if admissionReview.Request == nil {
		h.log.Info("Admission review request is nil")
		admissionReview.Response = &admissionv1.AdmissionResponse{
			Allowed: false,
			Result: &metav1.Status{
				Code:    http.StatusBadRequest,
				Message: "Missing admission request",
			},
		}
		c.JSON(http.StatusOK, admissionReview)
		return
	}

	admissionReview.Response = &admissionv1.AdmissionResponse{
		UID: admissionReview.Request.UID,
	}

	// Only handle Service resources
	expectedGVK := metav1.GroupVersionKind{
		Group:   "",
		Version: "v1",
		Kind:    "Service",
	}
	if admissionReview.Request.Kind != expectedGVK {
		h.log.Info("Unexpected resource type", zap.String("got", admissionReview.Request.Kind.Kind))
		admissionReview.Response.Allowed = false
		admissionReview.Response.Result = &metav1.Status{
			Code:    http.StatusBadRequest,
			Message: fmt.Sprintf("Expected %s resource, got %s", expectedGVK.Kind, admissionReview.Request.Kind.Kind),
		}
		c.JSON(http.StatusOK, admissionReview)
		return
	}

	// Decode the Service object
	var svc corev1.Service
	if err := runtime.DecodeInto(
		serializer.NewCodecFactory(runtime.NewScheme()).UniversalDeserializer(),
		admissionReview.Request.Object.Raw,
		&svc,
	); err != nil {
		h.log.Error("Failed to decode Service", zap.Error(err))
		admissionReview.Response.Allowed = false
		admissionReview.Response.Result = &metav1.Status{
			Code:    http.StatusBadRequest,
			Message: "Unable to decode Service object",
		}
		c.JSON(http.StatusOK, admissionReview)
		return
	}

	var warnings []string
	var err error
	ctx := c.Request.Context()
	warnings, err = handleWebhookOperation(
		h.log,
		admissionReview.Request.Operation,
		svc.GetName(),
		svc.GetNamespace(),
		func() ([]string, error) { return h.validateCreate(ctx, &svc) },
		func() ([]string, error) { return h.validateUpdate(ctx, &svc) },
		c,
		&admissionReview,
		"Service",
	)

	if err != nil {
		h.log.Error("Validation failed", zap.Error(err))
		admissionReview.Response.Allowed = false
		admissionReview.Response.Result = &metav1.Status{
			Code:    http.StatusForbidden,
			Message: err.Error(),
		}
		metrics.WebhookAdmissionDecision.WithLabelValues(webhookName, operation, "denied").Inc()
	} else {
		admissionReview.Response.Allowed = true
		if len(warnings) > 0 {
			admissionReview.Response.Warnings = warnings
		}
		metrics.WebhookAdmissionDecision.WithLabelValues(webhookName, operation, "allowed").Inc()
	}

	c.JSON(http.StatusOK, admissionReview)
}

func (h *ServiceWebhook) validateCreate(ctx context.Context, svc *corev1.Service) ([]string, error) {
	return h.validateServiceOperation(ctx, svc, "creation")
}

func (h *ServiceWebhook) validateUpdate(ctx context.Context, svc *corev1.Service) ([]string, error) {
	return h.validateServiceOperation(ctx, svc, "update")
}

// validateServiceOperation is a shared function for both create and update validation
func (h *ServiceWebhook) validateServiceOperation(
	ctx context.Context,
	svc *corev1.Service,
	operation string,
) ([]string, error) {
	if svc == nil {
		h.log.Info("Skipping CRQ validation for nil service on " + operation)
		return nil, nil
	}

	// Determine the resource names to check (generic + subtype)
	var resourceName corev1.ResourceName
	switch svc.Spec.Type {
	case corev1.ServiceTypeLoadBalancer:
		resourceName = usage.ResourceServicesLoadBalancers
	case corev1.ServiceTypeNodePort:
		resourceName = usage.ResourceServicesNodePorts
	default:
		resourceName = usage.ResourceServices
	}
	resourceNames := []corev1.ResourceName{usage.ResourceServices, resourceName}

	for _, rn := range resourceNames {
		if rn == "" {
			continue
		}
		// Always +1 for the service being created/updated
		if err := h.validateResourceQuota(ctx, svc.Namespace, rn, *resource.NewQuantity(1, resource.DecimalSI)); err != nil {
			return nil, fmt.Errorf("ClusterResourceQuota service count validation failed for %s: %w", rn, err)
		}
	}

	h.log.Info("Service CRQ validation passed",
		zap.String("service", svc.Name),
		zap.String("namespace", svc.Namespace),
		zap.String("operation", operation))
	return nil, nil
}

// validateResourceQuota validates if a resource operation would exceed any applicable ClusterResourceQuota
func (h *ServiceWebhook) validateResourceQuota(
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
func (h *ServiceWebhook) calculateCurrentUsage(ctx context.Context, namespace string,
	resourceName corev1.ResourceName) (resource.Quantity, error) {
	switch resourceName {
	case usage.ResourceServices, usage.ResourceServicesLoadBalancers, usage.ResourceServicesNodePorts:
		return h.serviceCalculator.CalculateUsage(ctx, namespace, resourceName)
	default:
		return resource.Quantity{}, fmt.Errorf("unsupported resource type: %s", resourceName)
	}
}
