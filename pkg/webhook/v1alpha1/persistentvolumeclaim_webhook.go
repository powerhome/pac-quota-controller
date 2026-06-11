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
	"github.com/powerhome/pac-quota-controller/pkg/kubernetes/storage"
	"github.com/powerhome/pac-quota-controller/pkg/kubernetes/usage"
)

// PersistentVolumeClaimWebhook handles webhook requests for PersistentVolumeClaim resources
type PersistentVolumeClaimWebhook struct {
	crqClient *quota.CRQClient
	logger    *zap.Logger
}

// NewPersistentVolumeClaimWebhook creates a new PersistentVolumeClaimWebhook
func NewPersistentVolumeClaimWebhook(
	crqClient *quota.CRQClient,
	logger *zap.Logger,
) *PersistentVolumeClaimWebhook {
	if logger == nil {
		logger = zap.NewNop()
	}
	logger = logger.Named("pvc-webhook")
	return &PersistentVolumeClaimWebhook{
		crqClient: crqClient,
		logger:    logger,
	}
}

// Handle handles the webhook request for PersistentVolumeClaim
func (h *PersistentVolumeClaimWebhook) Handle(c *gin.Context) {
	runWebhook(c, h.logger, webhookConfig{
		name:             "persistentvolumeclaim",
		expectedGVK:      &metav1.GroupVersionKind{Group: "", Version: "v1", Kind: "PersistentVolumeClaim"},
		requireNamespace: true,
	}, h.validate)
}

// TODO: the []string return is a future-proofing placeholder for admission
// warnings. Once any validator actually emits warnings, plumb them through
// runWebhook into AdmissionResponse.Warnings.
func (h *PersistentVolumeClaimWebhook) validate(
	ctx context.Context,
	req *admissionv1.AdmissionRequest,
) ([]string, error) {
	switch req.Operation {
	case admissionv1.Create, admissionv1.Update:
	default:
		return nil, unsupportedOperationError(req.Operation, "PersistentVolumeClaim")
	}

	var pvc corev1.PersistentVolumeClaim
	if err := decodeAdmissionObject(req.Object.Raw, &pvc, "PersistentVolumeClaim"); err != nil {
		return nil, err
	}

	var oldPVC *corev1.PersistentVolumeClaim
	if req.Operation == admissionv1.Update && len(req.OldObject.Raw) > 0 {
		var p corev1.PersistentVolumeClaim
		if err := decodeAdmissionObject(req.OldObject.Raw, &p, "PersistentVolumeClaim"); err != nil {
			return nil, err
		}
		oldPVC = &p
	}

	return nil, h.validateOperation(ctx, &pvc, oldPVC, req.Operation)
}

func (h *PersistentVolumeClaimWebhook) validateOperation(
	ctx context.Context,
	pvc *corev1.PersistentVolumeClaim,
	oldPVC *corev1.PersistentVolumeClaim,
	op admissionv1.Operation,
) error {
	crq := resolveCRQForNamespace(ctx, h.crqClient, h.logger, pvc.Namespace)
	if crq == nil {
		return nil
	}

	correlationID := quota.GetCorrelationID(ctx)
	storageDelta := storage.GetPVCStorageRequest(pvc)
	if oldPVC != nil {
		storageDelta.Sub(storage.GetPVCStorageRequest(oldPVC))
	}

	type check struct {
		resource corev1.ResourceName
		quantity resource.Quantity
		errFmt   string
	}
	checks := []check{
		{usage.ResourceRequestsStorage, storageDelta, "ClusterResourceQuota storage validation failed: %w"},
	}
	if pvc.Spec.StorageClassName != nil && *pvc.Spec.StorageClassName != "" {
		storageClass := *pvc.Spec.StorageClassName
		checks = append(checks, check{
			corev1.ResourceName(fmt.Sprintf("%s.storageclass.storage.k8s.io/requests.storage", storageClass)),
			storageDelta,
			fmt.Sprintf("ClusterResourceQuota storage class '%s' storage validation failed: %%w", storageClass),
		})
	}
	// Count checks only apply on Create; Update never adds or removes a PVC.
	if oldPVC == nil {
		checks = append(checks, check{
			usage.ResourcePersistentVolumeClaims, oneQuantity,
			"ClusterResourceQuota PVC count validation failed: %w",
		})
		if pvc.Spec.StorageClassName != nil && *pvc.Spec.StorageClassName != "" {
			storageClass := *pvc.Spec.StorageClassName
			checks = append(checks, check{
				corev1.ResourceName(fmt.Sprintf("%s.storageclass.storage.k8s.io/persistentvolumeclaims", storageClass)),
				oneQuantity,
				fmt.Sprintf("ClusterResourceQuota storage class '%s' PVC count validation failed: %%w", storageClass),
			})
		}
	}

	for _, c := range checks {
		// Skip zero-or-negative deltas: API rejects PVC shrink in practice, but
		// tests can inject one and we don't want to charge negative quota.
		if c.quantity.Sign() <= 0 {
			continue
		}
		if err := validateCRQStatusUsage(crq, c.resource, c.quantity, h.logger, correlationID); err != nil {
			return fmt.Errorf(c.errFmt, err)
		}
	}

	logValidationPassed(h.logger, "PVC", pvc.Namespace, op,
		zap.String("pvc", pvc.Name),
		zap.String("storage_delta", storageDelta.String()))
	return nil
}
