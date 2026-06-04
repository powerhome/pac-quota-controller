package v1alpha1

import (
	"context"

	"github.com/gin-gonic/gin"
	"go.uber.org/zap"
	admissionv1 "k8s.io/api/admission/v1"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/client-go/kubernetes"

	namespaceutil "github.com/powerhome/pac-quota-controller/pkg/kubernetes/namespace"
	"github.com/powerhome/pac-quota-controller/pkg/kubernetes/quota"
)

// NamespaceWebhook handles webhook requests for Namespace resources
type NamespaceWebhook struct {
	client    kubernetes.Interface
	crqClient *quota.CRQClient
	logger    *zap.Logger
}

// NewNamespaceWebhook creates a new NamespaceWebhook
func NewNamespaceWebhook(
	k8sClient kubernetes.Interface,
	crqClient *quota.CRQClient,
	logger *zap.Logger,
) *NamespaceWebhook {
	if logger == nil {
		logger = zap.NewNop()
	}
	logger = logger.Named("namespace-webhook")
	return &NamespaceWebhook{
		client:    k8sClient,
		crqClient: crqClient,
		logger:    logger,
	}
}

// Handle handles the webhook request for Namespace
func (h *NamespaceWebhook) Handle(c *gin.Context) {
	runWebhook(c, h.logger, webhookConfig{
		name:             "namespace",
		expectedGVK:      &metav1.GroupVersionKind{Group: "", Version: "v1", Kind: "Namespace"},
		requireNamespace: false,
	}, h.validate)
}

// TODO: the []string return is a future-proofing placeholder for admission
// warnings. Once any validator actually emits warnings, plumb them through
// runWebhook into AdmissionResponse.Warnings.
func (h *NamespaceWebhook) validate(ctx context.Context, req *admissionv1.AdmissionRequest) ([]string, error) {
	switch req.Operation {
	case admissionv1.Create, admissionv1.Update:
	default:
		return nil, unsupportedOperationError(req.Operation, "Namespace")
	}

	var ns corev1.Namespace
	if err := decodeAdmissionObject(req.Object.Raw, &ns, "Namespace"); err != nil {
		return nil, err
	}

	h.logger.Debug("Validating namespace for CRQ conflicts",
		zap.String("namespace", ns.Name),
		zap.String("operation", string(req.Operation)))
	return nil, h.validateOperation(ctx, &ns)
}

// validateOperation checks if the namespace would conflict with existing CRQs
func (h *NamespaceWebhook) validateOperation(ctx context.Context, ns *corev1.Namespace) error {
	if h.crqClient == nil {
		h.logger.Info("No CRQ client available, skipping CRQ validation",
			zap.String("namespace", ns.Name))
		return nil
	}

	validator := namespaceutil.NewNamespaceValidator(h.client, h.crqClient)
	return validator.ValidateNamespaceAgainstCRQs(ctx, ns)
}
