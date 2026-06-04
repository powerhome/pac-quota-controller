package v1alpha1

import (
	"context"
	"fmt"

	"github.com/gin-gonic/gin"
	"go.uber.org/zap"
	admissionv1 "k8s.io/api/admission/v1"
	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/resource"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/client-go/kubernetes"

	"github.com/powerhome/pac-quota-controller/pkg/kubernetes/pod"
	"github.com/powerhome/pac-quota-controller/pkg/kubernetes/quota"
	"github.com/powerhome/pac-quota-controller/pkg/kubernetes/usage"
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
	logger = logger.Named("pod-webhook")
	return &PodWebhook{
		client:        k8sClient,
		podCalculator: *pod.NewPodResourceCalculator(k8sClient, logger),
		crqClient:     crqClient,
		logger:        logger,
	}
}

// Handle handles the webhook request for Pod
//
// TODO: add Dynamic Resource Allocation (DRA) quota enforcement. DRA quotas
// belong on resourceclaims.resource.k8s.io rather than Pod, but pods may
// indirectly consume claims and should be revisited once the upstream API
// stabilizes.
func (h *PodWebhook) Handle(c *gin.Context) {
	runWebhook(c, h.logger, webhookConfig{
		name:             "pod",
		expectedGVK:      &metav1.GroupVersionKind{Group: "", Version: "v1", Kind: "Pod"},
		requireNamespace: true,
	}, h.validate)
}

// TODO: the []string return is a future-proofing placeholder for admission
// warnings. Once any validator actually emits warnings, plumb them through
// runWebhook into AdmissionResponse.Warnings.
func (h *PodWebhook) validate(ctx context.Context, req *admissionv1.AdmissionRequest) ([]string, error) {
	switch req.Operation {
	case admissionv1.Create, admissionv1.Update:
	default:
		return nil, unsupportedOperationError(req.Operation, "Pod")
	}

	var podObj corev1.Pod
	if err := decodeAdmissionObject(req.Object.Raw, &podObj, "Pod"); err != nil {
		return nil, err
	}

	return h.validateOperation(ctx, &podObj, req.Operation)
}

// validateOperation is shared between create and update validation.
func (h *PodWebhook) validateOperation(
	ctx context.Context,
	podObj *corev1.Pod,
	op admissionv1.Operation,
) ([]string, error) {
	if podObj == nil {
		h.logger.Info("Skipping CRQ validation for nil pod on " + string(op))
		return nil, nil
	}

	podUsage := pod.CalculatePodUsage(podObj, usage.ResourceRequestsCPU)
	if !podUsage.IsZero() {
		if err := validateAgainstCRQ(
			ctx, h.client, h.crqClient, h.logger,
			podObj.Namespace, usage.ResourceRequestsCPU, podUsage, h.calculateCurrentUsage,
		); err != nil {
			return nil, fmt.Errorf("ClusterResourceQuota CPU requests validation failed: %w", err)
		}
	}

	podUsage = pod.CalculatePodUsage(podObj, usage.ResourceRequestsMemory)
	if !podUsage.IsZero() {
		if err := validateAgainstCRQ(
			ctx, h.client, h.crqClient, h.logger,
			podObj.Namespace, usage.ResourceRequestsMemory, podUsage, h.calculateCurrentUsage,
		); err != nil {
			return nil, fmt.Errorf("ClusterResourceQuota memory requests validation failed: %w", err)
		}
	}

	podUsage = pod.CalculatePodUsage(podObj, usage.ResourceLimitsCPU)
	if !podUsage.IsZero() {
		if err := validateAgainstCRQ(
			ctx, h.client, h.crqClient, h.logger,
			podObj.Namespace, usage.ResourceLimitsCPU, podUsage, h.calculateCurrentUsage,
		); err != nil {
			return nil, fmt.Errorf("ClusterResourceQuota CPU limits validation failed: %w", err)
		}
	}

	podUsage = pod.CalculatePodUsage(podObj, usage.ResourceLimitsMemory)
	if !podUsage.IsZero() {
		if err := validateAgainstCRQ(
			ctx, h.client, h.crqClient, h.logger,
			podObj.Namespace, usage.ResourceLimitsMemory, podUsage, h.calculateCurrentUsage,
		); err != nil {
			return nil, fmt.Errorf("ClusterResourceQuota memory limits validation failed: %w", err)
		}
	}

	podCount := resource.NewQuantity(1, resource.DecimalSI)
	if err := validateAgainstCRQ(
		ctx, h.client, h.crqClient, h.logger,
		podObj.Namespace, usage.ResourcePods, *podCount, h.calculateCurrentUsage,
	); err != nil {
		return nil, fmt.Errorf("ClusterResourceQuota pod count validation failed: %w", err)
	}

	h.logger.Debug("Pod CRQ validation passed",
		zap.String("pod", podObj.Name),
		zap.String("namespace", podObj.Namespace),
		zap.String("operation", string(op)),
	)
	return nil, nil
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
