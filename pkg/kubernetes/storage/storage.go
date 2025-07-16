/*
Copyright 2025.

Licensed under the Apache License, Version 2.0 (the "License");
you may not use this file except in compliance with the License.
You may obtain a copy of the License at

    http://www.apache.org/licenses/LICENSE-2.0

Unless required by applicable law or agreed to in writing, software
distributed under the License is distributed on an "AS IS" BASIS,
WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
See the License for the specific language governing permissions and
limitations under the License.
*/

package storage

import (
	"context"
	"fmt"

	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/resource"
	"sigs.k8s.io/controller-runtime/pkg/client"
	logf "sigs.k8s.io/controller-runtime/pkg/log"
)

var log = logf.Log.WithName("storage-resource-calculator")

// StorageResourceCalculator provides methods for calculating storage resource usage
// from PersistentVolumeClaims only. Ephemeral storage calculation is handled by the pod package.
type StorageResourceCalculator struct {
	client.Client
}

// NewStorageResourceCalculator creates a new instance of StorageResourceCalculator.
func NewStorageResourceCalculator(client client.Client) *StorageResourceCalculator {
	return &StorageResourceCalculator{
		Client: client,
	}
}

// CalculateStorageUsage calculates the total storage usage for a given namespace.
// It lists all PersistentVolumeClaims in the namespace and sums their storage requests.
// This implements the same logic as Kubernetes ResourceQuota for storage resources.
func (c *StorageResourceCalculator) CalculateStorageUsage(ctx context.Context, namespace string) (resource.Quantity, error) {
	log.Info("Calculating storage usage", "namespace", namespace)

	// List all PVCs in the namespace
	pvcList := &corev1.PersistentVolumeClaimList{}
	listOpts := &client.ListOptions{
		Namespace: namespace,
	}

	if err := c.List(ctx, pvcList, listOpts); err != nil {
		return resource.Quantity{}, fmt.Errorf("failed to list PVCs in namespace %s: %w", namespace, err)
	}

	// Sum up storage requests from all PVCs
	totalUsage := resource.NewQuantity(0, resource.BinarySI)
	for _, pvc := range pvcList.Items {
		storageRequest := getPVCStorageRequest(&pvc)
		totalUsage.Add(storageRequest)

		log.V(1).Info("PVC storage request",
			"namespace", namespace,
			"pvc", pvc.Name,
			"request", storageRequest.String(),
			"phase", pvc.Status.Phase)
	}

	log.Info("Storage usage calculation completed",
		"namespace", namespace,
		"totalUsage", totalUsage.String(),
		"pvcCount", len(pvcList.Items))

	return *totalUsage, nil
}

// CalculateStorageClassUsage calculates storage usage for a specific storage class in a namespace.
// This implements Kubernetes ResourceQuota storage class specific quotas:
// <storage-class-name>.storageclass.storage.k8s.io/requests.storage
func (c *StorageResourceCalculator) CalculateStorageClassUsage(ctx context.Context, namespace, storageClass string) (resource.Quantity, error) {
	log.Info("Calculating storage class usage", "namespace", namespace, "storageClass", storageClass)

	// List all PVCs in the namespace
	pvcList := &corev1.PersistentVolumeClaimList{}
	listOpts := &client.ListOptions{
		Namespace: namespace,
	}

	if err := c.List(ctx, pvcList, listOpts); err != nil {
		return resource.Quantity{}, fmt.Errorf("failed to list PVCs in namespace %s: %w", namespace, err)
	}

	// Sum up storage requests from PVCs with matching storage class
	totalUsage := resource.NewQuantity(0, resource.BinarySI)
	for _, pvc := range pvcList.Items {
		pvcStorageClass := getPVCStorageClass(&pvc)
		if pvcStorageClass == storageClass {
			storageRequest := getPVCStorageRequest(&pvc)
			totalUsage.Add(storageRequest)

			log.V(1).Info("PVC storage class request",
				"namespace", namespace,
				"pvc", pvc.Name,
				"storageClass", storageClass,
				"request", storageRequest.String(),
				"phase", pvc.Status.Phase)
		}
	}

	log.Info("Storage class usage calculation completed",
		"namespace", namespace,
		"storageClass", storageClass,
		"totalUsage", totalUsage.String())

	return *totalUsage, nil
}

// CalculateStorageClassCount calculates the count of PVCs for a specific storage class in a namespace.
// This implements Kubernetes ResourceQuota storage class specific quotas:
// <storage-class-name>.storageclass.storage.k8s.io/persistentvolumeclaims
func (c *StorageResourceCalculator) CalculateStorageClassCount(ctx context.Context, namespace, storageClass string) (int64, error) {
	log.Info("Calculating storage class PVC count", "namespace", namespace, "storageClass", storageClass)

	// List all PVCs in the namespace
	pvcList := &corev1.PersistentVolumeClaimList{}
	listOpts := &client.ListOptions{
		Namespace: namespace,
	}

	if err := c.List(ctx, pvcList, listOpts); err != nil {
		return 0, fmt.Errorf("failed to list PVCs in namespace %s: %w", namespace, err)
	}

	// Count PVCs with matching storage class
	var count int64
	for _, pvc := range pvcList.Items {
		pvcStorageClass := getPVCStorageClass(&pvc)
		if pvcStorageClass == storageClass {
			count++

			log.V(1).Info("PVC storage class match",
				"namespace", namespace,
				"pvc", pvc.Name,
				"storageClass", storageClass,
				"phase", pvc.Status.Phase)
		}
	}

	log.Info("Storage class PVC count calculation completed",
		"namespace", namespace,
		"storageClass", storageClass,
		"count", count)

	return count, nil
}

// getPVCStorageRequest extracts the storage request from a PersistentVolumeClaim.
// If no storage request is specified, it returns a zero quantity.
// This follows the same logic as Kubernetes ResourceQuota for storage calculation.
func getPVCStorageRequest(pvc *corev1.PersistentVolumeClaim) resource.Quantity {
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
	if pvc.Spec.StorageClassName == nil {
		return ""
	}
	return *pvc.Spec.StorageClassName
}
