package v1alpha1

import (
	"context"
	"fmt"
	"net/http"

	"github.com/gin-gonic/gin"
	"github.com/prometheus/client_golang/prometheus"
	"go.uber.org/zap"
	admissionv1 "k8s.io/api/admission/v1"
	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/resource"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/runtime/serializer"
	"k8s.io/apimachinery/pkg/types"

	quotav1alpha1 "github.com/powerhome/pac-quota-controller/api/v1alpha1"
	"github.com/powerhome/pac-quota-controller/pkg/kubernetes/quota"
	"github.com/powerhome/pac-quota-controller/pkg/metrics"
)

// statusError carries an HTTP status code so callbacks can distinguish client
// errors (decode, unsupported op) from quota denials (default 403).
type statusError struct {
	code int
	msg  string
}

func (e *statusError) Error() string { return e.msg }

func newStatusErrorf(code int, format string, args ...any) error {
	return &statusError{code: code, msg: fmt.Sprintf(format, args...)}
}

// webhookConfig parameterizes runWebhook for each concrete webhook.
type webhookConfig struct {
	// name is the value used for the "webhook" metric label.
	name string
	// expectedGVK, when non-nil, is asserted against the AdmissionRequest.Kind.
	expectedGVK *metav1.GroupVersionKind
	// requireNamespace rejects requests with an empty namespace (used for
	// namespaced resources; cluster-scoped webhooks set this to false).
	requireNamespace bool
}

// validateFn is the per-request callback invoked by runWebhook after structural checks.
type validateFn func(ctx context.Context, req *admissionv1.AdmissionRequest) ([]string, error)

// runWebhook is the shared entry point for every admission handler: JSON
// binding, request validation, metrics, GVK check, and response writing.
func runWebhook(c *gin.Context, logger *zap.Logger, cfg webhookConfig, validate validateFn) {
	var review admissionv1.AdmissionReview
	if err := c.ShouldBindJSON(&review); err != nil {
		logger.Error("Failed to bind admission review", zap.Error(err))
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}

	if review.Request == nil {
		logger.Error("Malformed admission review request")
		c.JSON(http.StatusBadRequest, http.StatusBadRequest)
		return
	}

	review.Response = &admissionv1.AdmissionResponse{UID: review.Request.UID}

	if cfg.requireNamespace && review.Request.Namespace == "" {
		logger.Info("Admission review request namespace is empty")
		review.Response.Allowed = false
		review.Response.Result = &metav1.Status{
			Code:    http.StatusBadRequest,
			Message: fmt.Sprintf("Namespace is required for %s validation", cfg.name),
		}
		c.JSON(http.StatusOK, review)
		return
	}

	op := string(review.Request.Operation)
	ns := review.Request.Namespace
	metrics.WebhookValidationCount.WithLabelValues(cfg.name, op, ns).Inc()
	timer := prometheus.NewTimer(metrics.WebhookValidationDuration.WithLabelValues(cfg.name, op, ns))
	defer timer.ObserveDuration()

	if cfg.expectedGVK != nil && review.Request.Kind != *cfg.expectedGVK {
		logger.Error("Unexpected resource type",
			zap.String("expected", cfg.expectedGVK.Kind),
			zap.String("got", review.Request.Kind.Kind))
		review.Response.Allowed = false
		review.Response.Result = &metav1.Status{
			Code:    http.StatusBadRequest,
			Message: fmt.Sprintf("Expected %s resource, got %s", cfg.expectedGVK.Kind, review.Request.Kind.Kind),
		}
		c.JSON(http.StatusOK, review)
		return
	}

	warnings, err := validate(c.Request.Context(), review.Request)
	if err != nil {
		code := http.StatusForbidden
		if se, ok := err.(*statusError); ok {
			code = se.code
		}
		logger.Info("Admission denied",
			zap.String("webhook", cfg.name),
			zap.String("operation", op),
			zap.String("kind", review.Request.Kind.Kind),
			zap.String("resource", review.Request.Resource.Resource),
			zap.String("namespace", review.Request.Namespace),
			zap.String("name", review.Request.Name),
			zap.Int("code", code),
			zap.Error(err))
		review.Response.Allowed = false
		review.Response.Result = &metav1.Status{
			Code:    int32(code),
			Message: err.Error(),
		}
		metrics.WebhookAdmissionDecision.WithLabelValues(cfg.name, op, "denied", ns).Inc()
	} else {
		review.Response.Allowed = true
		if len(warnings) > 0 {
			review.Response.Warnings = warnings
		}
		metrics.WebhookAdmissionDecision.WithLabelValues(cfg.name, op, "allowed", ns).Inc()
	}

	c.JSON(http.StatusOK, review)
}

// decodeAdmissionObject decodes raw bytes into obj, returning a 400-coded
// statusError on failure.
func decodeAdmissionObject(raw []byte, into runtime.Object, kind string) error {
	if err := runtime.DecodeInto(webhookDecoder, raw, into); err != nil {
		return newStatusErrorf(http.StatusBadRequest, "Unable to decode %s object: %v", kind, err)
	}
	return nil
}

// Shared scheme + decoder for admission decoding. The universal deserializer
// does not require types to be registered, so a single empty scheme is safe
// to share across all webhooks and avoids per-request allocations.
var (
	webhookScheme  = runtime.NewScheme()
	webhookDecoder = serializer.NewCodecFactory(webhookScheme).UniversalDeserializer()

	oneQuantity = *resource.NewQuantity(1, resource.DecimalSI)
)

// unsupportedOperationError builds the standard 400 error for webhooks that only accept CREATE/UPDATE.
func unsupportedOperationError(op admissionv1.Operation, resourceType string) error {
	return newStatusErrorf(http.StatusBadRequest, "Operation %s is not supported for %s", op, resourceType)
}

// validateAgainstCRQ checks whether admitting `requested` of `resourceName` in
// `namespaceName` would exceed the matching CRQ hard limit. It fails open
// (admits + emits a metric) on any lookup or status-population miss.
func validateAgainstCRQ(
	ctx context.Context,
	crqClient *quota.CRQClient,
	logger *zap.Logger,
	namespaceName string,
	resourceName corev1.ResourceName,
	requested resource.Quantity,
) error {
	crq := resolveCRQForNamespace(ctx, crqClient, logger, namespaceName)
	if crq == nil {
		return nil
	}
	return validateCRQStatusUsage(crq, resourceName, requested, logger, quota.GetCorrelationID(ctx))
}

// validateCRQStatusUsage compares an in-memory CRQ status against a request.
// Split from validateAgainstCRQ so multi-resource handlers can resolve the
// CRQ once. crq must be non-nil.
func validateCRQStatusUsage(
	crq *quotav1alpha1.ClusterResourceQuota,
	resourceName corev1.ResourceName,
	requested resource.Quantity,
	logger *zap.Logger,
	correlationID string,
) error {
	quotaLimit, exists := crq.Spec.Hard[resourceName]
	if !exists {
		logger.Debug("No quota limit defined for resource, allowing operation",
			zap.String("correlation_id", correlationID),
			zap.String("resource", string(resourceName)),
			zap.String("crq_name", crq.Name))
		return nil
	}

	currentUsage, ok := crq.Status.Total.Used[resourceName]
	if !ok {
		// Fail-open: controller has not aggregated this resource yet (cold start / new key).
		logger.Info("CRQ status missing usage for resource - allowing operation",
			zap.String("correlation_id", correlationID),
			zap.String("resource", string(resourceName)),
			zap.String("crq_name", crq.Name))
		metrics.WebhookStatusMissing.WithLabelValues(crq.Name, string(resourceName)).Inc()
		return nil
	}

	totalUsage := currentUsage.DeepCopy()
	totalUsage.Add(requested)

	logger.Debug("Quota validation check",
		zap.String("correlation_id", correlationID),
		zap.String("resource", string(resourceName)),
		zap.String("current_usage", currentUsage.String()),
		zap.String("requested_quantity", requested.String()),
		zap.String("total_usage", totalUsage.String()),
		zap.String("quota_limit", quotaLimit.String()),
		zap.String("crq_name", crq.Name))

	if totalUsage.Cmp(quotaLimit) > 0 {
		logger.Info("Resource quota would be exceeded",
			zap.String("correlation_id", correlationID),
			zap.String("resource", string(resourceName)),
			zap.String("current_usage", currentUsage.String()),
			zap.String("requested_quantity", requested.String()),
			zap.String("total_usage", totalUsage.String()),
			zap.String("quota_limit", quotaLimit.String()),
			zap.String("crq_name", crq.Name))

		return fmt.Errorf(
			"ClusterResourceQuota '%s' %s limit exceeded: requested %s, current usage %s, "+
				"quota limit %s, total would be %s",
			crq.Name, resourceName, requested.String(), currentUsage.String(),
			quotaLimit.String(), totalUsage.String())
	}

	logger.Debug("CRQ validation passed",
		zap.String("correlation_id", correlationID),
		zap.String("resource", string(resourceName)),
		zap.String("requested_quantity", requested.String()),
		zap.String("crq_name", crq.Name))
	return nil
}

// resolveCRQForNamespace returns the matching CRQ from the cache or nil on
// any miss/error (fail-open). Lookup outcomes are tracked via WebhookCRQLookup.
func resolveCRQForNamespace(
	ctx context.Context,
	crqClient *quota.CRQClient,
	logger *zap.Logger,
	namespaceName string,
) *quotav1alpha1.ClusterResourceQuota {
	correlationID := quota.GetCorrelationID(ctx)

	if crqClient == nil {
		metrics.WebhookCRQLookup.WithLabelValues("no_client").Inc()
		return nil
	}

	ns := &corev1.Namespace{}
	if err := crqClient.Client.Get(ctx, types.NamespacedName{Name: namespaceName}, ns); err != nil {
		logger.Error("Failed to get namespace - allowing operation",
			zap.String("correlation_id", correlationID),
			zap.String("namespace", namespaceName),
			zap.Error(err))
		metrics.WebhookCRQLookup.WithLabelValues("namespace_error").Inc()
		return nil
	}

	crq, err := crqClient.GetCRQByNamespace(ctx, ns)
	if err != nil {
		logger.Error("Failed to get CRQ for namespace - allowing operation",
			zap.String("correlation_id", correlationID),
			zap.String("namespace", ns.Name),
			zap.Error(err))
		metrics.WebhookCRQLookup.WithLabelValues("crq_error").Inc()
		return nil
	}

	if crq == nil {
		metrics.WebhookCRQLookup.WithLabelValues("not_found").Inc()
		return nil
	}

	metrics.WebhookCRQLookup.WithLabelValues("found").Inc()
	return crq
}
