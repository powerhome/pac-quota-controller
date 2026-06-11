package storage

import (
	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/resource"
)

// CalculateStorageUsageFromPVCs calculates requests.storage usage from an already loaded pvc list.
func CalculateStorageUsageFromPVCs(
	pvcs []corev1.PersistentVolumeClaim,
	resourceName corev1.ResourceName,
) resource.Quantity {
	if resourceName != corev1.ResourceRequestsStorage {
		return resource.Quantity{}
	}

	totalUsage := resource.NewQuantity(0, resource.BinarySI)
	for i := range pvcs {
		totalUsage.Add(GetPVCStorageRequest(&pvcs[i]))
	}

	return *totalUsage
}

// CalculatePVCCountUsageFromPVCs calculates pvc object count from an already loaded pvc list.
func CalculatePVCCountUsageFromPVCs(pvcs []corev1.PersistentVolumeClaim) resource.Quantity {
	return *resource.NewQuantity(int64(len(pvcs)), resource.DecimalSI)
}

// CalculateStorageClassUsageFromPVCs calculates storage usage for a specific storage class from a loaded pvc list.
func CalculateStorageClassUsageFromPVCs(pvcs []corev1.PersistentVolumeClaim, storageClass string) resource.Quantity {
	totalUsage := resource.NewQuantity(0, resource.BinarySI)

	for i := range pvcs {
		if !PVCMatchesStorageClass(&pvcs[i], storageClass) {
			continue
		}
		totalUsage.Add(GetPVCStorageRequest(&pvcs[i]))
	}

	return *totalUsage
}

// CalculateStorageClassCountFromPVCs counts pvc objects for a specific storage class from a loaded pvc list.
func CalculateStorageClassCountFromPVCs(pvcs []corev1.PersistentVolumeClaim, storageClass string) int64 {
	var count int64
	for i := range pvcs {
		if PVCMatchesStorageClass(&pvcs[i], storageClass) {
			count++
		}
	}
	return count
}

// PVCMatchesStorageClass checks storage class name using both spec field and legacy annotation.
func PVCMatchesStorageClass(pvc *corev1.PersistentVolumeClaim, storageClass string) bool {
	if pvc == nil {
		return false
	}
	if pvc.Spec.StorageClassName != nil {
		return *pvc.Spec.StorageClassName == storageClass
	}
	if pvc.Annotations == nil {
		return false
	}
	return pvc.Annotations["volume.beta.kubernetes.io/storage-class"] == storageClass
}

// PVCStorageClass returns the storage class of the PVC, preferring
// spec.storageClassName and falling back to the legacy annotation. Returns
// "" when neither is set.
func PVCStorageClass(pvc *corev1.PersistentVolumeClaim) string {
	if pvc == nil {
		return ""
	}
	if pvc.Spec.StorageClassName != nil {
		return *pvc.Spec.StorageClassName
	}
	if pvc.Annotations != nil {
		return pvc.Annotations["volume.beta.kubernetes.io/storage-class"]
	}
	return ""
}

// GetPVCStorageRequest extracts the storage request from a PersistentVolumeClaim.
// If no storage request is specified, it returns a zero quantity.
// This follows the same logic as Kubernetes ResourceQuota for storage calculation.
func GetPVCStorageRequest(pvc *corev1.PersistentVolumeClaim) resource.Quantity {
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
