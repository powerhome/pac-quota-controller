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

// Handle handles the webhook request for Pod.
//
// DRA: when resource.k8s.io stabilizes, enforce resourceClaim quota via a
// separate webhook on resourceclaims.resource.k8s.io (CREATE). Pod.spec.resourceClaims
// is immutable so widening the Pod rule is not the right design.
func (h *PodWebhook) Handle(c *gin.Context) {
	runWebhook(c, h.logger, webhookConfig{
		name:             "pod",
		expectedGVK:      &metav1.GroupVersionKind{Group: "", Version: "v1", Kind: "Pod"},
		requireNamespace: true,
	}, h.validate)
}

func (h *PodWebhook) validate(ctx context.Context, req *admissionv1.AdmissionRequest) ([]string, error) {
	var podObj corev1.Pod
	if err := decodeAdmissionObject(req.Object.Raw, &podObj, "Pod"); err != nil {
		return nil, err
	}

	switch req.Operation {
	case admissionv1.Create:
		return h.validatePodOperation(ctx, &podObj, nil, OperationCreate)
	case admissionv1.Update:
		var oldPod *corev1.Pod
		if len(req.OldObject.Raw) > 0 {
			var p corev1.Pod
			if err := decodeAdmissionObject(req.OldObject.Raw, &p, "Pod"); err != nil {
				return nil, err
			}
			oldPod = &p
		}
		return h.validatePodOperation(ctx, &podObj, oldPod, OperationUpdate)
	default:
		return nil, unsupportedOperationError(req.Operation, "Pod")
	}
}

func (h *PodWebhook) validateCreate(ctx context.Context, p *corev1.Pod) ([]string, error) {
	return h.validatePodOperation(ctx, p, nil, OperationCreate)
}

func (h *PodWebhook) validateUpdate(ctx context.Context, p *corev1.Pod) ([]string, error) {
	return h.validatePodOperation(ctx, p, nil, OperationUpdate)
}

// validatePodOperation validates a pod against CRQ limits. For UPDATE (pods/resize),
// the cluster's current usage already includes the pre-resize pod and the pre-resize
// pod is already counted, so we charge only the positive delta (new - old) per
// compute resource and skip the +1 pod-count charge.
func (h *PodWebhook) validatePodOperation(
	ctx context.Context,
	newPod, oldPod *corev1.Pod,
	op operation,
) ([]string, error) {
	if newPod == nil {
		h.logger.Info("Skipping CRQ validation for nil pod on " + string(op))
		return nil, nil
	}

	checks := []struct {
		resource corev1.ResourceName
		label    string
	}{
		{usage.ResourceRequestsCPU, "CPU requests"},
		{usage.ResourceRequestsMemory, "memory requests"},
		{usage.ResourceLimitsCPU, "CPU limits"},
		{usage.ResourceLimitsMemory, "memory limits"},
	}
	for _, c := range checks {
		delta := pod.CalculatePodUsage(newPod, c.resource)
		if oldPod != nil {
			oldUsage := pod.CalculatePodUsage(oldPod, c.resource)
			delta.Sub(oldUsage)
		}
		if delta.Sign() <= 0 {
			continue
		}
		if err := h.validateResourceQuota(ctx, newPod.Namespace, c.resource, delta); err != nil {
			return nil, fmt.Errorf("ClusterResourceQuota %s validation failed: %w", c.label, err)
		}
	}

	if op == OperationCreate {
		podCount := resource.NewQuantity(1, resource.DecimalSI)
		if err := h.validateResourceQuota(ctx, newPod.Namespace, usage.ResourcePods, *podCount); err != nil {
			return nil, fmt.Errorf("ClusterResourceQuota pod count validation failed: %w", err)
		}
	}

	h.logger.Debug("Pod CRQ validation passed",
		zap.String("pod", newPod.Name),
		zap.String("namespace", newPod.Namespace),
		zap.String("operation", string(op)),
	)
	return nil, nil
}

func (h *PodWebhook) validateResourceQuota(
	ctx context.Context,
	namespace string,
	resourceName corev1.ResourceName,
	requested resource.Quantity,
) error {
	return validateAgainstCRQ(ctx, h.client, h.crqClient, h.logger,
		namespace, resourceName, requested,
		func(ns string, rn corev1.ResourceName) (resource.Quantity, error) {
			return h.calculateCurrentUsage(ctx, ns, rn)
		})
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
