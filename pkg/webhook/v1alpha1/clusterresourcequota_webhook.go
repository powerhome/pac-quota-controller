package v1alpha1

import (
	"context"
	"fmt"
	"net/http"

	"github.com/gin-gonic/gin"
	"github.com/prometheus/client_golang/prometheus"
	"go.uber.org/zap"
	admissionv1 "k8s.io/api/admission/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/runtime/serializer"
	"k8s.io/client-go/kubernetes"

	quotav1alpha1 "github.com/powerhome/pac-quota-controller/api/v1alpha1"
	"github.com/powerhome/pac-quota-controller/pkg/kubernetes/namespace"
	"github.com/powerhome/pac-quota-controller/pkg/kubernetes/quota"
	"github.com/powerhome/pac-quota-controller/pkg/metrics"
)

// ClusterResourceQuotaWebhook handles webhook requests for ClusterResourceQuota resources
type ClusterResourceQuotaWebhook struct {
	client    kubernetes.Interface
	crqClient *quota.CRQClient
	logger    *zap.Logger
}

// NewClusterResourceQuotaWebhook creates a new ClusterResourceQuotaWebhook
func NewClusterResourceQuotaWebhook(
	k8sClient kubernetes.Interface,
	crqClient *quota.CRQClient,
	logger *zap.Logger,
) *ClusterResourceQuotaWebhook {
	if logger == nil {
		logger = zap.NewNop()
	}
	return &ClusterResourceQuotaWebhook{
		client:    k8sClient,
		crqClient: crqClient,
		logger:    logger,
	}
}

// Handle handles the webhook request for ClusterResourceQuota
func (h *ClusterResourceQuotaWebhook) Handle(c *gin.Context) {
	var admissionReview admissionv1.AdmissionReview
	if err := c.ShouldBindJSON(&admissionReview); err != nil {
		h.logger.Error("Invalid admission review request", zap.Error(err))
		c.JSON(http.StatusBadRequest, gin.H{
			"error":   "Invalid request format",
			"details": err.Error(),
		})
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
	webhookName := "clusterresourcequota"
	metrics.WebhookValidationCount.WithLabelValues(webhookName, operation).Inc()
	timer := prometheus.NewTimer(metrics.WebhookValidationDuration.WithLabelValues(webhookName, operation))
	defer timer.ObserveDuration()

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
		h.logger.Info("Unexpected resource type",
			zap.String("expected", expectedGVK.Kind),
			zap.String("got", admissionReview.Request.Kind.Kind))
		admissionReview.Response.Allowed = false
		admissionReview.Response.Result = &metav1.Status{
			Code:    http.StatusBadRequest,
			Message: fmt.Sprintf("Expected %s resource, got %s", expectedGVK.Kind, admissionReview.Request.Kind.Kind),
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
		h.logger.Error("Failed to decode ClusterResourceQuota", zap.Error(err))
		admissionReview.Response.Allowed = false
		admissionReview.Response.Result = &metav1.Status{
			Code:    http.StatusBadRequest,
			Message: "Unable to decode ClusterResourceQuota object",
		}
		c.JSON(http.StatusOK, admissionReview)
		return
	}

	// Validate based on operation

	var err error
	ctx := c.Request.Context()

	switch admissionReview.Request.Operation {
	case admissionv1.Create:
		err = h.validateCreate(ctx, &crq)
	case admissionv1.Update:
		err = h.validateUpdate(ctx, &crq)
	default:
		h.logger.Info("Unsupported operation", zap.String("operation", string(admissionReview.Request.Operation)))
		admissionReview.Response.Allowed = true
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
		metrics.WebhookAdmissionDecision.WithLabelValues(webhookName, operation, "allowed").Inc()
	}

	c.JSON(http.StatusOK, admissionReview)
}

// validateOperation is a shared helper for create/update validation
func (h *ClusterResourceQuotaWebhook) validateOperation(
	ctx context.Context,
	crq *quotav1alpha1.ClusterResourceQuota,
) error {
	if h.crqClient == nil {
		return fmt.Errorf("CRQ client not available for validation")
	}

	validator := namespace.NewNamespaceValidator(h.client, h.crqClient)
	if err := validator.ValidateCRQNamespaceConflicts(ctx, crq); err != nil {
		return err
	}

	// Service quota validation removed; handled by dedicated service webhook.
	return nil
}

func (h *ClusterResourceQuotaWebhook) validateCreate(
	ctx context.Context,
	crq *quotav1alpha1.ClusterResourceQuota,
) error {
	return h.validateOperation(ctx, crq)
}

func (h *ClusterResourceQuotaWebhook) validateUpdate(
	ctx context.Context,
	crq *quotav1alpha1.ClusterResourceQuota,
) error {
	return h.validateOperation(ctx, crq)
}
