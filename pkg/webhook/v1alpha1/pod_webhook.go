package v1alpha1

import (
	"context"
	"fmt"

	"github.com/gin-gonic/gin"
	"go.uber.org/zap"
	admissionv1 "k8s.io/api/admission/v1"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"

	"github.com/powerhome/pac-quota-controller/pkg/kubernetes/pod"
	"github.com/powerhome/pac-quota-controller/pkg/kubernetes/quota"
	"github.com/powerhome/pac-quota-controller/pkg/kubernetes/usage"
)

// PodWebhook handles webhook requests for Pod resources
type PodWebhook struct {
	crqClient *quota.CRQClient
	logger    *zap.Logger
}

// NewPodWebhook creates a new PodWebhook
func NewPodWebhook(
	crqClient *quota.CRQClient,
	logger *zap.Logger,
) *PodWebhook {
	if logger == nil {
		logger = zap.NewNop()
	}
	logger = logger.Named("pod-webhook")
	return &PodWebhook{
		crqClient: crqClient,
		logger:    logger,
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
	switch req.Operation {
	case admissionv1.Create, admissionv1.Update:
	default:
		return nil, unsupportedOperationError(req.Operation, "Pod")
	}

	var podObj corev1.Pod
	if err := decodeAdmissionObject(req.Object.Raw, &podObj, "Pod"); err != nil {
		return nil, err
	}

	var oldPod *corev1.Pod
	if req.Operation == admissionv1.Update && len(req.OldObject.Raw) > 0 {
		var p corev1.Pod
		if err := decodeAdmissionObject(req.OldObject.Raw, &p, "Pod"); err != nil {
			return nil, err
		}
		oldPod = &p
	}

	return h.validateOperation(ctx, &podObj, oldPod, req.Operation)
}

// validateOperation is shared between create and update validation.
// For UPDATE (pods/resize) the current namespace usage already includes the
// pre-resize pod, so we charge only the positive delta (new - old) per compute
// resource and skip the +1 pod-count charge.
func (h *PodWebhook) validateOperation(
	ctx context.Context,
	podObj *corev1.Pod,
	oldPod *corev1.Pod,
	op admissionv1.Operation,
) ([]string, error) {
	if podObj == nil {
		h.logger.Info("Skipping CRQ validation for nil pod on " + string(op))
		return nil, nil
	}

	crq := resolveCRQForNamespace(ctx, h.crqClient, h.logger, podObj.Namespace)
	if crq == nil {
		return nil, nil
	}

	correlationID := quota.GetCorrelationID(ctx)

	computeResources := []struct {
		resource corev1.ResourceName
		label    string
	}{
		{usage.ResourceRequestsCPU, "CPU requests"},
		{usage.ResourceRequestsMemory, "memory requests"},
		{usage.ResourceLimitsCPU, "CPU limits"},
		{usage.ResourceLimitsMemory, "memory limits"},
	}

	for _, c := range computeResources {
		delta := pod.CalculatePodUsage(podObj, c.resource)
		if oldPod != nil {
			delta.Sub(pod.CalculatePodUsage(oldPod, c.resource))
		}
		if delta.Sign() <= 0 {
			continue
		}
		if err := validateCRQStatusUsage(crq, c.resource, delta, h.logger, correlationID); err != nil {
			return nil, fmt.Errorf("ClusterResourceQuota %s validation failed: %w", c.label, err)
		}
	}

	if op == admissionv1.Create {
		if err := validateCRQStatusUsage(crq, usage.ResourcePods, oneQuantity, h.logger, correlationID); err != nil {
			return nil, fmt.Errorf("ClusterResourceQuota pod count validation failed: %w", err)
		}
	}

	logValidationPassed(h.logger, "Pod", podObj.Namespace, op, zap.String("pod", podObj.Name))
	return nil, nil
}
