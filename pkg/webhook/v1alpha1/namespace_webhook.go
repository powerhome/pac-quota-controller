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

	namespaceutil "github.com/powerhome/pac-quota-controller/pkg/kubernetes/namespace"
	"github.com/powerhome/pac-quota-controller/pkg/kubernetes/quota"
	"github.com/powerhome/pac-quota-controller/pkg/metrics"
	"github.com/prometheus/client_golang/prometheus"
)

// NamespaceWebhook handles webhook requests for Namespace resources
type NamespaceWebhook struct {
	client    kubernetes.Interface
	crqClient *quota.CRQClient
	logger    *zap.Logger
}

// NewNamespaceWebhook creates a new NamespaceWebhook
func NewNamespaceWebhook(
	k8sClient kubernetes.Interface,
	crqClient *quota.CRQClient,
	logger *zap.Logger,
) *NamespaceWebhook {
	if logger == nil {
		logger = zap.NewNop()
	}
	return &NamespaceWebhook{
		client:    k8sClient,
		crqClient: crqClient,
		logger:    logger,
	}
}

// Handle handles the webhook request for Namespace
func (h *NamespaceWebhook) Handle(c *gin.Context) {
	var admissionReview admissionv1.AdmissionReview
	if err := c.ShouldBindJSON(&admissionReview); err != nil {
		h.logger.Error("Failed to bind admission review", zap.Error(err))
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}

	// Check for malformed requests
	if admissionReview.Request == nil {
		h.logger.Error("Malformed admission review request")
		c.JSON(http.StatusBadRequest, http.StatusBadRequest)
		return
	}

	// Metrics: start timer and increment validation count
	operation := string(admissionReview.Request.Operation)
	webhookName := "namespace"
	metrics.WebhookValidationCount.WithLabelValues(webhookName, operation).Inc()
	timer := prometheus.NewTimer(metrics.WebhookValidationDuration.WithLabelValues(webhookName, operation))
	defer timer.ObserveDuration()

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
		h.logger.Error("Unexpected resource kind",
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
		h.logger.Error("Failed to decode Namespace", zap.Error(err))
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
		err = h.validateCreate(c.Request.Context(), &namespace)
	case admissionv1.Update:
		err = h.validateUpdate(c.Request.Context(), &namespace)
	default:
		h.logger.Info("Unsupported operation", zap.String("operation", string(admissionReview.Request.Operation)))
		admissionReview.Response.Allowed = false
		admissionReview.Response.Result = &metav1.Status{
			Message: fmt.Sprintf("Unsupported operation: %s", admissionReview.Request.Operation),
		}
		c.JSON(http.StatusOK, admissionReview)
		return
	}

	if err != nil {
		h.logger.Error("Validation failed", zap.Error(err))
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

//nolint:unparam // This function is now properly implemented
func (h *NamespaceWebhook) validateCreate(ctx context.Context, namespace *corev1.Namespace) error {
	h.logger.Debug("Validating namespace for CRQ conflicts",
		zap.String("namespace", namespace.Name))

	return h.validateNamespaceAgainstCRQs(ctx, namespace)
}

//nolint:unparam // This function is now properly implemented
func (h *NamespaceWebhook) validateUpdate(ctx context.Context, namespace *corev1.Namespace) error {
	h.logger.Debug("Validating namespace update for CRQ conflicts",
		zap.String("namespace", namespace.Name))

	return h.validateNamespaceAgainstCRQs(ctx, namespace)
}

// validateNamespaceAgainstCRQs checks if the namespace would conflict with existing CRQs
func (h *NamespaceWebhook) validateNamespaceAgainstCRQs(ctx context.Context, ns *corev1.Namespace) error {
	if h.crqClient == nil {
		h.logger.Info("No CRQ client available, skipping CRQ validation",
			zap.String("namespace", ns.Name))
		return nil
	}

	validator := namespaceutil.NewNamespaceValidator(h.client, h.crqClient)
	return validator.ValidateNamespaceAgainstCRQs(ctx, ns)
}
