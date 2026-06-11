package v1alpha1

import (
	"context"

	"github.com/gin-gonic/gin"
	"go.uber.org/zap"
	admissionv1 "k8s.io/api/admission/v1"
	corev1 "k8s.io/api/core/v1"

	"github.com/powerhome/pac-quota-controller/pkg/kubernetes/quota"
)

// ObjectCountWebhook handles webhook requests for Object count resources.
// It enforces object count quotas for objects and subtypes.
type ObjectCountWebhook struct {
	crqClient *quota.CRQClient
	logger    *zap.Logger
}

// NewObjectCountWebhook creates a new ObjectCountWebhook
func NewObjectCountWebhook(
	crqClient *quota.CRQClient,
	logger *zap.Logger,
) *ObjectCountWebhook {
	if logger == nil {
		logger = zap.NewNop()
	}
	logger = logger.Named("objectcount-webhook")
	return &ObjectCountWebhook{
		crqClient: crqClient,
		logger:    logger,
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
	// Chart only subscribes vobjectcount to CREATE since object counts cannot
	// change on UPDATE; this guard is a defensive seatbelt in case the chart
	// drifts and the apiserver forwards an unexpected verb.
	if req.Operation != admissionv1.Create {
		return nil, unsupportedOperationError(req.Operation, "ObjectCount")
	}

	crqKey := req.Resource.Resource
	if req.Resource.Group != "" {
		crqKey = crqKey + "." + req.Resource.Group
	}
	resourceName := corev1.ResourceName(crqKey)

	return h.validateOperation(ctx, req.Namespace, resourceName, req.Operation)
}

// validateOperation is shared between create and update validation.
func (h *ObjectCountWebhook) validateOperation(
	ctx context.Context,
	namespace string,
	resourceName corev1.ResourceName,
	op admissionv1.Operation,
) ([]string, error) {
	if resourceName == "" {
		h.logger.Info("Skipping CRQ validation for empty resource name on " + string(op))
		return nil, nil
	}
	if err := validateAgainstCRQ(
		ctx, h.crqClient, h.logger,
		namespace, resourceName, oneQuantity,
	); err != nil {
		return nil, err
	}
	logValidationPassed(h.logger, "Object", namespace, op, zap.String("object", resourceName.String()))
	return nil, nil
}
