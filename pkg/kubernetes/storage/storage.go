package storage

import (
	"context"
	"fmt"

	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/resource"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/client-go/kubernetes"

	"github.com/powerhome/pac-quota-controller/pkg/kubernetes/quota"
	"github.com/powerhome/pac-quota-controller/pkg/kubernetes/usage"
	"go.uber.org/zap"
)

// StorageResourceCalculator provides methods for calculating storage resource usage
// from PersistentVolumeClaims only. Ephemeral storage calculation is handled by the pod package.
type StorageResourceCalculator struct {
	usage.BaseResourceCalculator
	logger *zap.Logger
}

// NewStorageResourceCalculator creates a new instance of StorageResourceCalculator.
func NewStorageResourceCalculator(c kubernetes.Interface, logger *zap.Logger) *StorageResourceCalculator {
	if logger == nil {
		logger = zap.NewNop()
	}
	return &StorageResourceCalculator{
		BaseResourceCalculator: *usage.NewBaseResourceCalculator(c),
		logger:                 logger.Named("storage-calculator"),
	}
}

// CalculateStorageUsage calculates the total storage usage for a given namespace.
// It lists all PersistentVolumeClaims in the namespace and sums their storage requests.
// This implements the same logic as Kubernetes ResourceQuota for storage resources.
func (c *StorageResourceCalculator) CalculateStorageUsage(
	ctx context.Context, namespace string,
) (resource.Quantity, error) {

	// List all PVCs in the namespace
	pvcList, err := c.Client.CoreV1().PersistentVolumeClaims(namespace).List(ctx, metav1.ListOptions{})
	if err != nil {
		return resource.Quantity{}, fmt.Errorf("failed to list PVCs in namespace %s: %w", namespace, err)
	}

	// Sum up storage requests from all PVCs
	totalUsage := resource.NewQuantity(0, resource.BinarySI)
	for _, pvc := range pvcList.Items {
		storageRequest := getPVCStorageRequest(&pvc)
		totalUsage.Add(storageRequest)
	}

	correlationID := quota.GetCorrelationID(ctx)

	c.logger.Debug("Calculated storage usage",
		zap.String("correlation_id", correlationID),
		zap.String("namespace", namespace),
		zap.String("total_usage", totalUsage.String()),
		zap.Int("pvc_count", len(pvcList.Items)))

	return *totalUsage, nil
}

// CalculateUsage calculates the total usage for a specific resource in a namespace
func (c *StorageResourceCalculator) CalculateUsage(
	ctx context.Context, namespace string, resourceName corev1.ResourceName,
) (resource.Quantity, error) {
	// For storage resources, we only handle storage-related resources
	switch resourceName {
	case usage.ResourceRequestsStorage, usage.ResourceStorage:
		return c.CalculateStorageUsage(ctx, namespace)
	case usage.ResourcePersistentVolumeClaims:
		pvcCount, err := c.CalculatePVCCount(ctx, namespace)
		if err != nil {
			return resource.Quantity{}, err
		}
		return *resource.NewQuantity(pvcCount, resource.DecimalSI), nil
	default:
		// Return zero for non-storage resources
		return resource.Quantity{}, nil
	}
}

// CalculatePVCCount calculates the number of PersistentVolumeClaims in a namespace
func (c *StorageResourceCalculator) CalculatePVCCount(ctx context.Context, namespace string) (int64, error) {

	// List all PVCs in the namespace
	pvcList, err := c.Client.CoreV1().PersistentVolumeClaims(namespace).List(ctx, metav1.ListOptions{})
	if err != nil {
		return 0, fmt.Errorf("failed to list PVCs in namespace %s: %w", namespace, err)
	}

	correlationID := quota.GetCorrelationID(ctx)
	count := int64(len(pvcList.Items))

	c.logger.Debug("Calculated PVC count",
		zap.String("correlation_id", correlationID),
		zap.String("namespace", namespace),
		zap.Int64("pvc_count", count))

	return count, nil
}

// CalculateStorageClassUsage calculates storage usage for a specific storage class in a namespace.
// This implements Kubernetes ResourceQuota storage class specific quotas:
// <storage-class-name>.storageclass.storage.k8s.io/requests.storage
func (c *StorageResourceCalculator) CalculateStorageClassUsage(
	ctx context.Context, namespace, storageClass string,
) (resource.Quantity, error) {

	// List all PVCs in the namespace
	pvcList, err := c.Client.CoreV1().PersistentVolumeClaims(namespace).List(ctx, metav1.ListOptions{})
	if err != nil {
		return resource.Quantity{}, fmt.Errorf("failed to list PVCs in namespace %s: %w", namespace, err)
	}

	// Sum up storage requests from PVCs with matching storage class
	totalUsage := resource.NewQuantity(0, resource.BinarySI)
	for _, pvc := range pvcList.Items {
		pvcStorageClass := getPVCStorageClass(&pvc)
		if pvcStorageClass == storageClass {
			storageRequest := getPVCStorageRequest(&pvc)
			totalUsage.Add(storageRequest)
		}
	}

	return *totalUsage, nil
}

// CalculateStorageClassCount calculates the count of PVCs for a specific storage class in a namespace.
// This implements Kubernetes ResourceQuota storage class specific quotas:
// <storage-class-name>.storageclass.storage.k8s.io/persistentvolumeclaims
func (c *StorageResourceCalculator) CalculateStorageClassCount(
	ctx context.Context, namespace, storageClass string,
) (int64, error) {

	// List all PVCs in the namespace
	pvcList, err := c.Client.CoreV1().PersistentVolumeClaims(namespace).List(ctx, metav1.ListOptions{})
	if err != nil {
		return 0, fmt.Errorf("failed to list PVCs in namespace %s: %w", namespace, err)
	}

	// Count PVCs with matching storage class
	var count int64
	for _, pvc := range pvcList.Items {
		pvcStorageClass := getPVCStorageClass(&pvc)
		if pvcStorageClass == storageClass {
			count++
		}
	}

	return count, nil
}

// getPVCStorageRequest extracts the storage request from a PersistentVolumeClaim.
// If no storage request is specified, it returns a zero quantity.
// This follows the same logic as Kubernetes ResourceQuota for storage calculation.
func getPVCStorageRequest(pvc *corev1.PersistentVolumeClaim) resource.Quantity {
	if pvc == nil {
		return resource.Quantity{}
	}

	if pvc.Spec.Resources.Requests == nil {
		return resource.Quantity{}
	}

	if storageRequest, ok := pvc.Spec.Resources.Requests[corev1.ResourceStorage]; ok {
		return storageRequest
	}

	return resource.Quantity{}
}

// getPVCStorageClass extracts the storage class from a PersistentVolumeClaim.
// Returns empty string if no storage class is specified.
// This follows the same logic as Kubernetes ResourceQuota for storage class specific quotas.
func getPVCStorageClass(pvc *corev1.PersistentVolumeClaim) string {
	if pvc == nil {
		return ""
	}

	if pvc.Spec.StorageClassName == nil {
		return ""
	}
	return *pvc.Spec.StorageClassName
}
