package v1alpha1

import (
	"context"
	"fmt"

	"github.com/gin-gonic/gin"
	"go.uber.org/zap"
	admissionv1 "k8s.io/api/admission/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/client-go/kubernetes"

	quotav1alpha1 "github.com/powerhome/pac-quota-controller/api/v1alpha1"
	"github.com/powerhome/pac-quota-controller/pkg/kubernetes/namespace"
	"github.com/powerhome/pac-quota-controller/pkg/kubernetes/quota"
)

// ClusterResourceQuotaWebhook handles webhook requests for ClusterResourceQuota resources
type ClusterResourceQuotaWebhook struct {
	client    kubernetes.Interface
	crqClient *quota.CRQClient
	logger    *zap.Logger
}

// NewClusterResourceQuotaWebhook creates a new ClusterResourceQuotaWebhook
func NewClusterResourceQuotaWebhook(
	k8sClient kubernetes.Interface,
	crqClient *quota.CRQClient,
	logger *zap.Logger,
) *ClusterResourceQuotaWebhook {
	if logger == nil {
		logger = zap.NewNop()
	}
	logger = logger.Named("crq-webhook")
	return &ClusterResourceQuotaWebhook{
		client:    k8sClient,
		crqClient: crqClient,
		logger:    logger,
	}
}

// Handle handles the webhook request for ClusterResourceQuota
func (h *ClusterResourceQuotaWebhook) Handle(c *gin.Context) {
	runWebhook(c, h.logger, webhookConfig{
		name: "clusterresourcequota",
		expectedGVK: &metav1.GroupVersionKind{
			Group:   "quota.powerapp.cloud",
			Version: "v1alpha1",
			Kind:    "ClusterResourceQuota",
		},
		requireNamespace: false,
	}, h.validate)
}

func (h *ClusterResourceQuotaWebhook) validate(
	ctx context.Context,
	req *admissionv1.AdmissionRequest,
) ([]string, error) {
	var crq quotav1alpha1.ClusterResourceQuota
	if err := decodeAdmissionObject(req.Object.Raw, &crq, "ClusterResourceQuota"); err != nil {
		return nil, err
	}

	switch req.Operation {
	case admissionv1.Create, admissionv1.Update:
		return nil, h.validateOperation(ctx, &crq)
	default:
		// Unknown operations (e.g. DELETE) are intentionally allowed; the
		// ValidatingWebhookConfiguration only registers CREATE/UPDATE so this
		// branch is unreachable in production but kept defensive.
		h.logger.Info("Allowing unsupported CRQ operation",
			zap.String("operation", string(req.Operation)))
		return nil, nil
	}
}

// validateOperation is a shared helper for create/update validation
func (h *ClusterResourceQuotaWebhook) validateOperation(
	ctx context.Context,
	crq *quotav1alpha1.ClusterResourceQuota,
) error {
	if h.crqClient == nil {
		return fmt.Errorf("CRQ client not available for validation")
	}

	validator := namespace.NewNamespaceValidator(h.client, h.crqClient)
	if err := validator.ValidateCRQNamespaceConflicts(ctx, crq); err != nil {
		return err
	}
	return nil
}
