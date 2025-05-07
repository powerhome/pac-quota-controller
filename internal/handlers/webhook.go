package handlers

import (
	"context"
	"encoding/json"
	"net/http"
	"time"

	"github.com/powerhouse/pac-quota-controller/internal/models"
	"github.com/powerhouse/pac-quota-controller/internal/services"
	"github.com/powerhouse/pac-quota-controller/pkg/errors"
	"github.com/powerhouse/pac-quota-controller/pkg/logging"
	"github.com/powerhouse/pac-quota-controller/pkg/metrics"
	"go.uber.org/zap"
	admissionv1 "k8s.io/api/admission/v1"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

var (
	logger = logging.NewLogger()
)

// HandleWebhook handles admission review requests
func HandleWebhook(w http.ResponseWriter, r *http.Request) {
	startTime := time.Now()
	resourceType := "unknown"

	// Record metrics
	defer func() {
		duration := time.Since(startTime).Seconds()
		metrics.WebhookRequestDuration.WithLabelValues(resourceType).Observe(duration)
	}()

	// Parse admission review request
	var admissionReview admissionv1.AdmissionReview
	if err := json.NewDecoder(r.Body).Decode(&admissionReview); err != nil {
		logger.Error("failed to decode admission review request", zap.Error(err))
		http.Error(w, "failed to decode admission review request", http.StatusBadRequest)
		return
	}

	// Set response type
	admissionReview.Response = &admissionv1.AdmissionResponse{
		UID: admissionReview.Request.UID,
	}

	// Handle different resource types
	switch admissionReview.Request.Kind.Kind {
	case "Pod":
		resourceType = "pod"
		handlePodAdmission(admissionReview)
	case "ClusterResourceQuota":
		resourceType = "clusterresourcequota"
		handleClusterResourceQuotaAdmission(admissionReview)
	default:
		logger.Error("unsupported resource kind",
			zap.String("kind", admissionReview.Request.Kind.Kind),
			zap.String("group", admissionReview.Request.Kind.Group),
			zap.String("version", admissionReview.Request.Kind.Version))
		admissionReview.Response.Allowed = false
		admissionReview.Response.Result = &metav1.Status{
			Status:  "Failure",
			Message: "unsupported resource kind",
			Reason:  metav1.StatusReasonBadRequest,
			Code:    http.StatusBadRequest,
		}
	}

	// Send response
	w.Header().Set("Content-Type", "application/json")
	if err := json.NewEncoder(w).Encode(admissionReview); err != nil {
		logger.Error("failed to encode admission review response", zap.Error(err))
		http.Error(w, "failed to encode admission review response", http.StatusInternalServerError)
		return
	}
}

func handlePodAdmission(admissionReview admissionv1.AdmissionReview) {
	// Default to allowing the request
	admissionReview.Response.Allowed = true

	var podResources *models.PodResourceRequest

	// Parse the Pod object
	var pod corev1.Pod
	if err := json.Unmarshal(admissionReview.Request.Object.Raw, &pod); err != nil {
		logger.Error("failed to parse Pod", zap.Error(err))
		admissionReview.Response.Allowed = false
		admissionReview.Response.Result = &metav1.Status{
			Status:  "Failure",
			Message: "failed to parse Pod",
			Reason:  metav1.StatusReasonInvalid,
			Code:    http.StatusBadRequest,
		}
		errors.IncrementErrorCount("PodParseError")
		return
	}

	// Populate podResources using the existing factory function
	podResources = models.NewPodResourceRequestFromPod(&pod)

	// Create quota service
	quotaService, err := services.NewQuotaService()
	if err != nil {
		logger.Error("failed to create quota service", zap.Error(err))
		admissionReview.Response.Allowed = false
		admissionReview.Response.Result = &metav1.Status{
			Status:  "Failure",
			Message: "failed to create quota service",
			Reason:  metav1.StatusReasonInternalError,
			Code:    http.StatusInternalServerError,
		}
		errors.IncrementErrorCount("QuotaServiceCreationError")
		return
	}

	// Validate pod against quotas
	if err := quotaService.ValidatePodAgainstQuotas(context.Background(), podResources, pod.Namespace); err != nil {
		logger.Warn("pod admission denied",
			zap.String("namespace", pod.Namespace),
			zap.String("name", pod.Name),
			zap.Error(err))

		admissionReview.Response.Allowed = false
		admissionReview.Response.Result = &metav1.Status{
			Status:  "Failure",
			Message: err.Error(),
			Reason:  metav1.StatusReasonForbidden,
			Code:    http.StatusForbidden,
		}
		errors.IncrementErrorCount("ResourceLimitExceeded")
		return
	}

	logger.Info("pod admission allowed",
		zap.String("namespace", pod.Namespace),
		zap.String("name", pod.Name))
}

func handleClusterResourceQuotaAdmission(admissionReview admissionv1.AdmissionReview) {
	// Default to allowing the request
	admissionReview.Response.Allowed = true

	// Parse the ClusterResourceQuota object
	var quota models.ClusterResourceQuota
	if err := json.Unmarshal(admissionReview.Request.Object.Raw, &quota); err != nil {
		logger.Error("failed to parse ClusterResourceQuota", zap.Error(err))
		admissionReview.Response.Allowed = false
		admissionReview.Response.Result = &metav1.Status{
			Status:  "Failure",
			Message: "failed to parse ClusterResourceQuota",
			Reason:  metav1.StatusReasonInvalid,
			Code:    http.StatusBadRequest,
		}
		errors.IncrementErrorCount("QuotaParseError")
		return
	}

	// Create quota service
	quotaService, err := services.NewQuotaService()
	if err != nil {
		logger.Error("failed to create quota service", zap.Error(err))
		admissionReview.Response.Allowed = false
		admissionReview.Response.Result = &metav1.Status{
			Status:  "Failure",
			Message: "failed to create quota service",
			Reason:  metav1.StatusReasonInternalError,
			Code:    http.StatusInternalServerError,
		}
		errors.IncrementErrorCount("QuotaServiceCreationError")
		return
	}

	// Validate the quota
	if err := quotaService.ValidateQuotaCreation(context.Background(), &quota); err != nil {
		logger.Error("quota validation failed", zap.Error(err))
		admissionReview.Response.Allowed = false
		admissionReview.Response.Result = &metav1.Status{
			Status:  "Failure",
			Message: err.Error(),
			Reason:  metav1.StatusReasonInvalid,
			Code:    http.StatusUnprocessableEntity,
		}
		errors.IncrementErrorCount("QuotaValidationFailed")
		return
	}

	logger.Info("ClusterResourceQuota validation completed successfully",
		zap.String("name", quota.Name))
}

// HandleHealthz handles health check requests
func HandleHealthz(w http.ResponseWriter, r *http.Request) {
	w.WriteHeader(http.StatusOK)
}

// HandleReadyz handles readiness check requests
func HandleReadyz(w http.ResponseWriter, r *http.Request) {
	w.WriteHeader(http.StatusOK)
}
