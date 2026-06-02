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

	"github.com/powerhome/pac-quota-controller/pkg/kubernetes/quota"
	"github.com/powerhome/pac-quota-controller/pkg/kubernetes/services"
	"github.com/powerhome/pac-quota-controller/pkg/kubernetes/usage"
)

// ServiceWebhook handles webhook requests for Service resources.
// It enforces object count quotas for services and subtypes.
type ServiceWebhook struct {
	client            kubernetes.Interface
	serviceCalculator services.ServiceResourceCalculator
	crqClient         *quota.CRQClient
	logger            *zap.Logger
}

// NewServiceWebhook creates a new ServiceWebhook
func NewServiceWebhook(
	k8sClient kubernetes.Interface,
	crqClient *quota.CRQClient,
	logger *zap.Logger,
) *ServiceWebhook {
	if logger == nil {
		logger = zap.NewNop()
	}
	return &ServiceWebhook{
		client:            k8sClient,
		serviceCalculator: *services.NewServiceResourceCalculator(k8sClient, logger),
		crqClient:         crqClient,
		logger:            logger,
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
	var svc corev1.Service
	if err := decodeAdmissionObject(req.Object.Raw, &svc, "Service"); err != nil {
		return nil, err
	}

	switch req.Operation {
	case admissionv1.Create:
		return h.validateServiceOperation(ctx, &svc, OperationCreate)
	case admissionv1.Update:
		return h.validateServiceOperation(ctx, &svc, OperationUpdate)
	default:
		return nil, unsupportedOperationError(req.Operation, "Service")
	}
}

func (h *ServiceWebhook) validateCreate(ctx context.Context, s *corev1.Service) ([]string, error) {
	return h.validateServiceOperation(ctx, s, OperationCreate)
}

func (h *ServiceWebhook) validateUpdate(ctx context.Context, s *corev1.Service) ([]string, error) {
	return h.validateServiceOperation(ctx, s, OperationUpdate)
}

// validateServiceOperation is shared between create and update validation.
func (h *ServiceWebhook) validateServiceOperation(
	ctx context.Context,
	svc *corev1.Service,
	op operation,
) ([]string, error) {
	var subtype corev1.ResourceName
	switch svc.Spec.Type {
	case corev1.ServiceTypeLoadBalancer:
		subtype = usage.ResourceServicesLoadBalancers
	case corev1.ServiceTypeNodePort:
		subtype = usage.ResourceServicesNodePorts
	}
	resourceNames := []corev1.ResourceName{usage.ResourceServices}
	if subtype != "" {
		resourceNames = append(resourceNames, subtype)
	}

	for _, rn := range resourceNames {
		if err := h.validateResourceQuota(ctx, svc.Namespace, rn, *resource.NewQuantity(1, resource.DecimalSI)); err != nil {
			return nil, fmt.Errorf("ClusterResourceQuota service count validation failed for %s: %w", rn, err)
		}
	}

	h.logger.Debug("Service CRQ validation passed",
		zap.String("service", svc.Name),
		zap.String("namespace", svc.Namespace),
		zap.String("operation", string(op)))
	return nil, nil
}

func (h *ServiceWebhook) validateResourceQuota(
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

func (h *ServiceWebhook) calculateCurrentUsage(ctx context.Context, namespace string,
	resourceName corev1.ResourceName) (resource.Quantity, error) {
	switch resourceName {
	case usage.ResourceServices, usage.ResourceServicesLoadBalancers, usage.ResourceServicesNodePorts:
		return h.serviceCalculator.CalculateUsage(ctx, namespace, resourceName)
	default:
		return resource.Quantity{}, fmt.Errorf("unsupported resource type: %s", resourceName)
	}
}
