package v1alpha1

import (
	"context"
	"fmt"

	"github.com/gin-gonic/gin"
	"go.uber.org/zap"
	admissionv1 "k8s.io/api/admission/v1"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"

	"github.com/powerhome/pac-quota-controller/pkg/kubernetes/quota"
	"github.com/powerhome/pac-quota-controller/pkg/kubernetes/usage"
)

// ServiceWebhook handles webhook requests for Service resources.
// It enforces object count quotas for services and subtypes.
type ServiceWebhook struct {
	crqClient *quota.CRQClient
	logger    *zap.Logger
}

// NewServiceWebhook creates a new ServiceWebhook
func NewServiceWebhook(
	crqClient *quota.CRQClient,
	logger *zap.Logger,
) *ServiceWebhook {
	if logger == nil {
		logger = zap.NewNop()
	}
	logger = logger.Named("service-webhook")
	return &ServiceWebhook{
		crqClient: crqClient,
		logger:    logger,
	}
}

// Handle handles the webhook request for Service
func (h *ServiceWebhook) Handle(c *gin.Context) {
	runWebhook(c, h.logger, webhookConfig{
		name:             "service",
		expectedGVK:      &metav1.GroupVersionKind{Group: "", Version: "v1", Kind: "Service"},
		requireNamespace: true,
	}, h.validate)
}

func (h *ServiceWebhook) validate(ctx context.Context, req *admissionv1.AdmissionRequest) ([]string, error) {
	switch req.Operation {
	case admissionv1.Create, admissionv1.Update:
	default:
		return nil, unsupportedOperationError(req.Operation, "Service")
	}

	var svc corev1.Service
	if err := decodeAdmissionObject(req.Object.Raw, &svc, "Service"); err != nil {
		return nil, err
	}

	var oldSvc *corev1.Service
	if req.Operation == admissionv1.Update && len(req.OldObject.Raw) > 0 {
		var s corev1.Service
		if err := decodeAdmissionObject(req.OldObject.Raw, &s, "Service"); err != nil {
			return nil, err
		}
		oldSvc = &s
	}

	return h.validateOperation(ctx, &svc, oldSvc, req.Operation)
}

// validateOperation runs per-resource count checks. On Update, charges +1
// only for resources the new service belongs to that the old service did not.
func (h *ServiceWebhook) validateOperation(
	ctx context.Context,
	svc *corev1.Service,
	oldSvc *corev1.Service,
	op admissionv1.Operation,
) ([]string, error) {
	crq := resolveCRQForNamespace(ctx, h.crqClient, h.logger, svc.Namespace)
	if crq == nil {
		return nil, nil
	}

	correlationID := quota.GetCorrelationID(ctx)

	already := map[corev1.ResourceName]bool{}
	if oldSvc != nil {
		for _, r := range serviceQuotaResources(oldSvc) {
			already[r] = true
		}
	}

	for _, r := range serviceQuotaResources(svc) {
		if already[r] {
			continue
		}
		if err := validateCRQStatusUsage(crq, r, oneQuantity, h.logger, correlationID); err != nil {
			return nil, fmt.Errorf("ClusterResourceQuota service count validation failed for %s: %w", r, err)
		}
	}

	logValidationPassed(h.logger, "Service", svc.Namespace, op, zap.String("service", svc.Name))
	return nil, nil
}

func serviceQuotaResources(svc *corev1.Service) []corev1.ResourceName {
	out := []corev1.ResourceName{usage.ResourceServices}
	switch svc.Spec.Type {
	case corev1.ServiceTypeLoadBalancer:
		out = append(out, usage.ResourceServicesLoadBalancers)
	case corev1.ServiceTypeNodePort:
		out = append(out, usage.ResourceServicesNodePorts)
	}
	return out
}
