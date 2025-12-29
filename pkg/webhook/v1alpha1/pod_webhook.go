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
	"github.com/powerhome/pac-quota-controller/pkg/metrics"
	"github.com/prometheus/client_golang/prometheus"
)

// PodWebhook handles webhook requests for Pod resources
type PodWebhook struct {
	client        kubernetes.Interface
	podCalculator pod.PodResourceCalculator
	crqClient     *quota.CRQClient
	logger        *zap.Logger
}

// NewPodWebhook creates a new PodWebhook
func NewPodWebhook(
	k8sClient kubernetes.Interface,
	crqClient *quota.CRQClient,
	logger *zap.Logger,
) *PodWebhook {
	if logger == nil {
		logger = zap.NewNop()
	}
	return &PodWebhook{
		client:        k8sClient,
		podCalculator: *pod.NewPodResourceCalculator(k8sClient, logger),
		crqClient:     crqClient,
		logger:        logger,
	}
}

// Handle handles the webhook request for Pod
func (h *PodWebhook) Handle(c *gin.Context) {
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

	if namespace := admissionReview.Request.Namespace; namespace == "" {
		h.logger.Info("Admission review request namespace is empty")
		admissionReview.Response = &admissionv1.AdmissionResponse{
			UID:     admissionReview.Request.UID,
			Allowed: false,
			Result: &metav1.Status{
				Code:    http.StatusBadRequest,
				Message: "Namespace is required for object count validation",
			},
		}
		c.JSON(http.StatusOK, admissionReview)
		return
	}

	// Metrics: start timer and increment validation count
	operation := string(admissionReview.Request.Operation)
	webhookName := "pod"
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
		Kind:    "Pod",
	}
	if admissionReview.Request.Kind != expectedGVK {
		h.logger.Error("Unexpected resource type",
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
	var podObj corev1.Pod
	if err := runtime.DecodeInto(
		serializer.NewCodecFactory(runtime.NewScheme()).UniversalDeserializer(),
		admissionReview.Request.Object.Raw,
		&podObj,
	); err != nil {
		h.logger.Error("Failed to decode Pod", zap.Error(err))
		admissionReview.Response.Allowed = false
		admissionReview.Response.Result = &metav1.Status{
			Code:    http.StatusBadRequest,
			Message: "Unable to decode Pod object",
		}
		c.JSON(http.StatusOK, admissionReview)
		return
	}

	// Validate based on operation
	var warnings []string
	var err error

	ctx := c.Request.Context()
	warnings, err = handleWebhookOperation(
		h.logger,
		admissionReview.Request.Operation,
		func() ([]string, error) { return h.validateCreate(ctx, &podObj) },
		func() ([]string, error) { return h.validateUpdate(ctx, &podObj) },
		c,
		&admissionReview,
		"Pod",
	)

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

func (h *PodWebhook) validateCreate(ctx context.Context, podObj *corev1.Pod) ([]string, error) {
	return h.validatePodOperation(ctx, podObj, OperationCreate)
}

func (h *PodWebhook) validateUpdate(ctx context.Context, podObj *corev1.Pod) ([]string, error) {
	return h.validatePodOperation(ctx, podObj, OperationUpdate)
}

// validatePodOperation is a shared function for both create and update validation
func (h *PodWebhook) validatePodOperation(
	ctx context.Context,
	podObj *corev1.Pod,
	operation operation,
) ([]string, error) {
	// Handle nil pod case
	if podObj == nil {
		h.logger.Info("Skipping CRQ validation for nil pod on " + string(operation))
		return nil, nil
	}

	// Calculate the resource usage for this pod
	podUsage := pod.CalculatePodUsage(podObj, usage.ResourceRequestsCPU)
	if !podUsage.IsZero() {
		// Validate CPU requests
		if err := h.validateResourceQuota(ctx, podObj.Namespace, usage.ResourceRequestsCPU, podUsage); err != nil {
			return nil, fmt.Errorf("ClusterResourceQuota CPU requests validation failed: %w", err)
		}
	}

	podUsage = pod.CalculatePodUsage(podObj, usage.ResourceRequestsMemory)
	if !podUsage.IsZero() {
		// Validate memory requests
		if err := h.validateResourceQuota(ctx, podObj.Namespace, usage.ResourceRequestsMemory, podUsage); err != nil {
			return nil, fmt.Errorf("ClusterResourceQuota memory requests validation failed: %w", err)
		}
	}

	podUsage = pod.CalculatePodUsage(podObj, usage.ResourceLimitsCPU)
	if !podUsage.IsZero() {
		// Validate CPU limits
		if err := h.validateResourceQuota(ctx, podObj.Namespace, usage.ResourceLimitsCPU, podUsage); err != nil {
			return nil, fmt.Errorf("ClusterResourceQuota CPU limits validation failed: %w", err)
		}
	}

	podUsage = pod.CalculatePodUsage(podObj, usage.ResourceLimitsMemory)
	if !podUsage.IsZero() {
		// Validate memory limits
		if err := h.validateResourceQuota(ctx, podObj.Namespace, usage.ResourceLimitsMemory, podUsage); err != nil {
			return nil, fmt.Errorf("ClusterResourceQuota memory limits validation failed: %w", err)
		}
	}

	// Validate pod count (always 1 for a new pod)
	podCount := resource.NewQuantity(1, resource.DecimalSI)
	if err := h.validateResourceQuota(ctx, podObj.Namespace, usage.ResourcePods, *podCount); err != nil {
		return nil, fmt.Errorf("ClusterResourceQuota pod count validation failed: %w", err)
	}

	h.logger.Debug("Pod CRQ validation passed",
		zap.String("pod", podObj.Name),
		zap.String("namespace", podObj.Namespace),
		zap.String("operation", string(operation)),
	)
	return nil, nil
}

// validateResourceQuota validates if a resource operation would exceed any applicable ClusterResourceQuota
func (h *PodWebhook) validateResourceQuota(
	ctx context.Context,
	namespace string,
	resourceName corev1.ResourceName,
	requestedQuantity resource.Quantity,
) error {
	// Fetch the actual namespace object with labels
	ns, err := h.client.CoreV1().Namespaces().Get(ctx, namespace, metav1.GetOptions{})
	if err != nil {
		return fmt.Errorf("failed to get namespace %s: %w", namespace, err)
	}

	return validateCRQResourceQuotaWithNamespace(ctx, h.crqClient, h.client, ns, resourceName, requestedQuantity,
		func(ns string, rn corev1.ResourceName) (resource.Quantity, error) {
			return h.calculateCurrentUsage(ctx, ns, rn)
		}, h.logger)
}

// calculateCurrentUsage calculates the current usage of a resource in a namespace
func (h *PodWebhook) calculateCurrentUsage(ctx context.Context, namespace string,
	resourceName corev1.ResourceName) (resource.Quantity, error) {
	switch resourceName {
	case usage.ResourceRequestsCPU, usage.ResourceRequestsMemory, usage.ResourceLimitsCPU, usage.ResourceLimitsMemory:
		return h.podCalculator.CalculateUsage(ctx, namespace, resourceName)
	case usage.ResourcePods:
		count, err := h.podCalculator.CalculatePodCount(ctx, namespace)
		if err != nil {
			return resource.Quantity{}, err
		}
		return *resource.NewQuantity(count, resource.DecimalSI), nil
	default:
		return resource.Quantity{}, fmt.Errorf("unsupported resource type: %s", resourceName)
	}
}
