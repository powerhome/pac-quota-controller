package v1alpha1

import (
	"context"
	"fmt"
	"strings"

	"github.com/gin-gonic/gin"
	"go.uber.org/zap"
	admissionv1 "k8s.io/api/admission/v1"
	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/resource"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/client-go/kubernetes"

	"github.com/powerhome/pac-quota-controller/pkg/kubernetes/quota"
	"github.com/powerhome/pac-quota-controller/pkg/kubernetes/storage"
	"github.com/powerhome/pac-quota-controller/pkg/kubernetes/usage"
)

// PersistentVolumeClaimWebhook handles webhook requests for PersistentVolumeClaim resources
type PersistentVolumeClaimWebhook struct {
	client            kubernetes.Interface
	storageCalculator storage.StorageResourceCalculator
	crqClient         *quota.CRQClient
	logger            *zap.Logger
}

// NewPersistentVolumeClaimWebhook creates a new PersistentVolumeClaimWebhook
func NewPersistentVolumeClaimWebhook(
	k8sClient kubernetes.Interface,
	crqClient *quota.CRQClient,
	logger *zap.Logger,
) *PersistentVolumeClaimWebhook {
	if logger == nil {
		logger = zap.NewNop()
	}
	logger = logger.Named("pvc-webhook")
	return &PersistentVolumeClaimWebhook{
		client:            k8sClient,
		storageCalculator: *storage.NewStorageResourceCalculator(k8sClient, logger),
		crqClient:         crqClient,
		logger:            logger,
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

	return nil, h.validateOperation(ctx, &pvc)
}

func (h *PersistentVolumeClaimWebhook) validateOperation(
	ctx context.Context,
	pvc *corev1.PersistentVolumeClaim,
) error {
	storageRequest := getStorageRequest(pvc)

	if err := validateAgainstCRQ(
		ctx, h.client, h.crqClient, h.logger,
		pvc.Namespace, usage.ResourceRequestsStorage, storageRequest, h.calculateCurrentUsage,
	); err != nil {
		return fmt.Errorf("ClusterResourceQuota storage validation failed: %w", err)
	}

	pvcCount := resource.NewQuantity(1, resource.DecimalSI)
	if err := validateAgainstCRQ(
		ctx, h.client, h.crqClient, h.logger,
		pvc.Namespace, usage.ResourcePersistentVolumeClaims, *pvcCount, h.calculateCurrentUsage,
	); err != nil {
		return fmt.Errorf("ClusterResourceQuota PVC count validation failed: %w", err)
	}

	if pvc.Spec.StorageClassName != nil && *pvc.Spec.StorageClassName != "" {
		storageClass := *pvc.Spec.StorageClassName

		storageClassStorageResource := corev1.ResourceName(fmt.Sprintf(
			"%s.storageclass.storage.k8s.io/requests.storage", storageClass))
		if err := validateAgainstCRQ(
			ctx, h.client, h.crqClient, h.logger,
			pvc.Namespace, storageClassStorageResource, storageRequest, h.calculateCurrentUsage,
		); err != nil {
			return fmt.Errorf("ClusterResourceQuota storage class '%s' storage validation failed: %w", storageClass, err)
		}

		storageClassCountResource := corev1.ResourceName(fmt.Sprintf(
			"%s.storageclass.storage.k8s.io/persistentvolumeclaims", storageClass))
		if err := validateAgainstCRQ(
			ctx, h.client, h.crqClient, h.logger,
			pvc.Namespace, storageClassCountResource, *pvcCount, h.calculateCurrentUsage,
		); err != nil {
			return fmt.Errorf("ClusterResourceQuota storage class '%s' PVC count validation failed: %w", storageClass, err)
		}

		h.logger.Debug("PVC storage class specific CRQ validation passed",
			zap.String("pvc", pvc.Name),
			zap.String("namespace", pvc.Namespace),
			zap.String("storageClass", storageClass),
			zap.String("storageRequest", storageRequest.String()))
	}

	h.logger.Debug("PVC CRQ validation passed",
		zap.String("pvc", pvc.Name),
		zap.String("namespace", pvc.Namespace),
		zap.String("storageRequest", storageRequest.String()))
	return nil
}

// calculateCurrentUsage calculates the current usage of a resource in a namespace
func (h *PersistentVolumeClaimWebhook) calculateCurrentUsage(ctx context.Context, namespace string,
	resourceName corev1.ResourceName) (resource.Quantity, error) {
	switch resourceName {
	case usage.ResourceRequestsStorage:
		return h.storageCalculator.CalculateUsage(ctx, namespace, resourceName)
	case usage.ResourcePersistentVolumeClaims:
		count, err := h.storageCalculator.CalculatePVCCount(ctx, namespace)
		if err != nil {
			return resource.Quantity{}, err
		}
		return *resource.NewQuantity(count, resource.DecimalSI), nil
	default:
		resourceStr := string(resourceName)
		if strings.HasSuffix(resourceStr, ".storageclass.storage.k8s.io/requests.storage") {
			storageClass := strings.TrimSuffix(resourceStr, ".storageclass.storage.k8s.io/requests.storage")
			return h.storageCalculator.CalculateStorageClassUsage(ctx, namespace, storageClass)
		}
		if strings.HasSuffix(resourceStr, ".storageclass.storage.k8s.io/persistentvolumeclaims") {
			storageClass := strings.TrimSuffix(resourceStr, ".storageclass.storage.k8s.io/persistentvolumeclaims")
			count, err := h.storageCalculator.CalculateStorageClassCount(ctx, namespace, storageClass)
			if err != nil {
				return resource.Quantity{}, err
			}
			return *resource.NewQuantity(count, resource.DecimalSI), nil
		}
		return resource.Quantity{}, fmt.Errorf("unsupported resource type: %s", resourceName)
	}
}

func getStorageRequest(pvc *corev1.PersistentVolumeClaim) resource.Quantity {
	if pvc.Spec.Resources.Requests != nil {
		if storageRequest, exists := pvc.Spec.Resources.Requests[corev1.ResourceStorage]; exists {
			return storageRequest
		}
	}
	return resource.Quantity{}
}
