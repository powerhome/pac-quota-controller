package v1alpha1

import (
	"context"

	"github.com/gin-gonic/gin"
	"go.uber.org/zap"
	admissionv1 "k8s.io/api/admission/v1"
	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/resource"
	"k8s.io/client-go/kubernetes"

	"github.com/powerhome/pac-quota-controller/pkg/kubernetes/objectcount"
	"github.com/powerhome/pac-quota-controller/pkg/kubernetes/quota"
)

// ObjectCountWebhook handles webhook requests for Object count resources.
// It enforces object count quotas for objects and subtypes.
type ObjectCountWebhook struct {
	client                kubernetes.Interface
	objectCountCalculator *objectcount.ObjectCountCalculator
	crqClient             *quota.CRQClient
	logger                *zap.Logger
}

// NewObjectCountWebhook creates a new ObjectCountWebhook
func NewObjectCountWebhook(
	k8sClient kubernetes.Interface,
	crqClient *quota.CRQClient,
	logger *zap.Logger,
) *ObjectCountWebhook {
	if logger == nil {
		logger = zap.NewNop()
	}
	return &ObjectCountWebhook{
		client:                k8sClient,
		objectCountCalculator: objectcount.NewObjectCountCalculator(k8sClient, logger),
		crqClient:             crqClient,
		logger:                logger,
	}
}

// Handle handles the webhook request for object count resources.
// Unlike the other webhooks, ObjectCount intentionally skips the GVK check
// because it serves many different object kinds via a single endpoint and
// derives the resource name from AdmissionRequest.Resource instead.
func (h *ObjectCountWebhook) Handle(c *gin.Context) {
	runWebhook(c, h.logger, webhookConfig{
		name:             "objectcount",
		expectedGVK:      nil,
		requireNamespace: true,
	}, h.validate)
}

func (h *ObjectCountWebhook) validate(ctx context.Context, req *admissionv1.AdmissionRequest) ([]string, error) {
	crqKey := req.Resource.Resource
	if req.Resource.Group != "" {
		crqKey = crqKey + "." + req.Resource.Group
	}
	resourceName := corev1.ResourceName(crqKey)

	switch req.Operation {
	case admissionv1.Create:
		return h.validateObjectOperation(ctx, req.Namespace, resourceName, OperationCreate)
	case admissionv1.Update:
		return h.validateObjectOperation(ctx, req.Namespace, resourceName, OperationUpdate)
	default:
		return nil, unsupportedOperationError(req.Operation, "ObjectCount")
	}
}

// validateObjectOperation is shared between create and update validation.
func (h *ObjectCountWebhook) validateObjectOperation(
	ctx context.Context,
	namespace string,
	resourceName corev1.ResourceName,
	op operation,
) ([]string, error) {
	if resourceName == "" {
		h.logger.Info("Skipping CRQ validation for empty resource name on " + string(op))
		return nil, nil
	}
	if err := h.validateResourceQuota(ctx, namespace, resourceName, resource.MustParse("1")); err != nil {
		return nil, err
	}
	h.logger.Debug("Object CRQ validation passed",
		zap.String("object", resourceName.String()),
		zap.String("namespace", namespace),
		zap.String("operation", string(op)))
	return nil, nil
}

func (h *ObjectCountWebhook) validateResourceQuota(
	ctx context.Context,
	namespace string,
	resourceName corev1.ResourceName,
	requested resource.Quantity,
) error {
	return validateAgainstCRQ(ctx, h.client, h.crqClient, h.logger,
		namespace, resourceName, requested,
		func(ns string, rn corev1.ResourceName) (resource.Quantity, error) {
			return h.objectCountCalculator.CalculateUsage(ctx, ns, rn)
		})
}
