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
	"k8s.io/client-go/kubernetes"

	"github.com/powerhome/pac-quota-controller/pkg/kubernetes/quota"
	"github.com/powerhome/pac-quota-controller/pkg/metrics"

	quotav1alpha1 "github.com/powerhome/pac-quota-controller/api/v1alpha1"
	"github.com/powerhome/pac-quota-controller/pkg/kubernetes/namespace"
)

// statusError carries an HTTP status code alongside an error message so callbacks
// can signal client errors (e.g. decode failures, unsupported operations) vs
// quota-violation denials, which use http.StatusForbidden by default.
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

// validateFn is the per-request callback invoked by runWebhook after all
// structural checks pass. It is responsible for decoding the request body and
// running the actual quota validation.
type validateFn func(ctx context.Context, req *admissionv1.AdmissionRequest) ([]string, error)

// runWebhook is the single entry point used by every admission webhook handler.
// It handles JSON binding, request shape validation, metrics, GVK checks and
// response writing so that each concrete webhook only needs to provide a
// validate callback.
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
	if !cfg.requireNamespace {
		ns = review.Request.Name
	}
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
		logger.Error("Validation failed", zap.Error(err))
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

// decodeAdmissionObject decodes raw bytes into the supplied object using the
// universal deserializer, returning a 400-coded statusError on failure so
// runWebhook surfaces the expected client-error response.
func decodeAdmissionObject(raw []byte, into runtime.Object, kind string) error {
	if err := runtime.DecodeInto(
		serializer.NewCodecFactory(runtime.NewScheme()).UniversalDeserializer(),
		raw,
		into,
	); err != nil {
		return newStatusErrorf(http.StatusBadRequest, "Unable to decode %s object: %v", kind, err)
	}
	return nil
}

// unsupportedOperationError builds the standard "operation not supported" error
// for webhooks that only accept CREATE/UPDATE.
func unsupportedOperationError(op admissionv1.Operation, resourceType string) error {
	return newStatusErrorf(http.StatusBadRequest, "Operation %s is not supported for %s", op, resourceType)
}

// validateAgainstCRQ is the shared "fetch namespace + delegate to the CRQ
// validator" wrapper used by every namespaced webhook. It looks up the target
// namespace, finds the CRQ that applies (if any), and verifies that adding
// requested to the current cross-namespace usage stays within the configured
// hard limit.
func validateAgainstCRQ(
	ctx context.Context,
	k8sClient kubernetes.Interface,
	crqClient *quota.CRQClient,
	logger *zap.Logger,
	namespaceName string,
	resourceName corev1.ResourceName,
	requested resource.Quantity,
	calculateCurrentUsage func(context.Context, string, corev1.ResourceName) (resource.Quantity, error),
) error {
	correlationID := quota.GetCorrelationID(ctx)

	if crqClient == nil {
		logger.Info("Skipping CRQ validation - no CRQ client available",
			zap.String("correlation_id", correlationID),
			zap.String("namespace", namespaceName),
			zap.String("resource", string(resourceName)),
			zap.String("requested_quantity", requested.String()))
		return nil
	}

	ns, err := k8sClient.CoreV1().Namespaces().Get(ctx, namespaceName, metav1.GetOptions{})
	if err != nil {
		return fmt.Errorf("failed to get namespace %s: %w", namespaceName, err)
	}

	logger.Debug("Starting CRQ validation",
		zap.String("correlation_id", correlationID),
		zap.String("namespace", ns.Name),
		zap.String("resource", string(resourceName)),
		zap.String("requested_quantity", requested.String()),
		zap.Any("namespace_labels", ns.Labels))

	crq, err := crqClient.GetCRQByNamespace(ctx, ns)
	if err != nil {
		// TODO: currently fail-open on CRQ-lookup errors (log + admit) to
		// avoid blocking unrelated workloads during transient API/informer issues.
		// Revisit once we are confident the CRQ informer is reliably available;
		logger.Error("Failed to get CRQ for namespace",
			zap.String("correlation_id", correlationID),
			zap.String("namespace", ns.Name),
			zap.Error(err))
		return nil
	}

	if crq == nil {
		logger.Debug("No CRQ applies to namespace, allowing operation",
			zap.String("correlation_id", correlationID),
			zap.String("namespace", ns.Name),
			zap.String("resource", string(resourceName)))
		return nil
	}

	quotaLimit, exists := crq.Spec.Hard[resourceName]
	if !exists {
		logger.Debug("No quota limit defined for resource, allowing operation",
			zap.String("correlation_id", correlationID),
			zap.String("namespace", ns.Name),
			zap.String("resource", string(resourceName)),
			zap.String("crq_name", crq.Name))
		return nil
	}

	currentUsage, err := calculateCRQCurrentUsage(ctx, k8sClient, crq, resourceName, calculateCurrentUsage, logger)
	if err != nil {
		logger.Error("Failed to calculate current usage across CRQ namespaces",
			zap.String("correlation_id", correlationID),
			zap.String("namespace", ns.Name),
			zap.String("resource", string(resourceName)),
			zap.Error(err))
		return fmt.Errorf("failed to calculate current usage: %w", err)
	}

	totalUsage := currentUsage.DeepCopy()
	totalUsage.Add(requested)

	logger.Debug("Quota validation check",
		zap.String("correlation_id", correlationID),
		zap.String("namespace", ns.Name),
		zap.String("resource", string(resourceName)),
		zap.String("current_usage", currentUsage.String()),
		zap.String("requested_quantity", requested.String()),
		zap.String("total_usage", totalUsage.String()),
		zap.String("quota_limit", quotaLimit.String()))

	if totalUsage.Cmp(quotaLimit) > 0 {
		logger.Info("Resource quota would be exceeded",
			zap.String("correlation_id", correlationID),
			zap.String("namespace", ns.Name),
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
		zap.String("namespace", ns.Name),
		zap.String("resource", string(resourceName)),
		zap.String("requested_quantity", requested.String()),
		zap.String("crq_name", crq.Name))
	return nil
}

// calculateCRQCurrentUsage sums the per-resource usage across every namespace
// that matches the CRQ selector. It's the cross-namespace half of
// validateAgainstCRQ.
func calculateCRQCurrentUsage(
	ctx context.Context,
	kubernetesClient kubernetes.Interface,
	crq *quotav1alpha1.ClusterResourceQuota,
	resourceName corev1.ResourceName,
	calculateCurrentUsage func(context.Context, string, corev1.ResourceName) (resource.Quantity, error),
	logger *zap.Logger,
) (resource.Quantity, error) {
	namespaceNames, err := namespace.GetSelectedNamespaces(ctx, kubernetesClient, crq)
	if err != nil {
		return resource.Quantity{}, fmt.Errorf("failed to get namespaces matching CRQ selector: %w", err)
	}

	logger.Debug("Calculating usage across CRQ namespaces",
		zap.String("crq", crq.Name),
		zap.String("resource", string(resourceName)),
		zap.Strings("namespaces", namespaceNames))

	totalUsage := resource.NewQuantity(0, resource.DecimalSI)
	for _, namespaceName := range namespaceNames {
		nsUsage, err := calculateCurrentUsage(ctx, namespaceName, resourceName)
		if err != nil {
			logger.Error("Failed to calculate usage for namespace",
				zap.String("namespace", namespaceName),
				zap.String("resource", string(resourceName)),
				zap.Error(err))
			return resource.Quantity{}, fmt.Errorf("failed to calculate usage for namespace %s: %w", namespaceName, err)
		}
		totalUsage.Add(nsUsage)

		logger.Debug("Namespace usage calculated",
			zap.String("namespace", namespaceName),
			zap.String("resource", string(resourceName)),
			zap.String("usage", nsUsage.String()))
	}

	logger.Debug("Total CRQ usage calculated",
		zap.String("crq", crq.Name),
		zap.String("resource", string(resourceName)),
		zap.String("total_usage", totalUsage.String()),
		zap.Strings("namespaces", namespaceNames))

	return *totalUsage, nil
}
