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

	return h.validateOperation(ctx, &svc, req.Operation)
}

// validateOperation is shared between create and update validation.
func (h *ServiceWebhook) validateOperation(
	ctx context.Context,
	svc *corev1.Service,
	op admissionv1.Operation,
) ([]string, error) {
	crq := resolveCRQForNamespace(ctx, h.crqClient, h.logger, svc.Namespace)
	if crq == nil {
		return nil, nil
	}

	correlationID := quota.GetCorrelationID(ctx)
	one := *resource.NewQuantity(1, resource.DecimalSI)

	resourceNames := []corev1.ResourceName{usage.ResourceServices}
	switch svc.Spec.Type {
	case corev1.ServiceTypeLoadBalancer:
		resourceNames = append(resourceNames, usage.ResourceServicesLoadBalancers)
	case corev1.ServiceTypeNodePort:
		resourceNames = append(resourceNames, usage.ResourceServicesNodePorts)
	}

	for _, rn := range resourceNames {
		if err := validateCRQStatusUsage(crq, rn, one, h.logger, correlationID); err != nil {
			return nil, fmt.Errorf("ClusterResourceQuota service count validation failed for %s: %w", rn, err)
		}
	}

	h.logger.Debug("Service CRQ validation passed",
		zap.String("service", svc.Name),
		zap.String("namespace", svc.Namespace),
		zap.String("operation", string(op)))
	return nil, nil
}
