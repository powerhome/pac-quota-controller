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

package v1alpha1

import (
	"context"
	"fmt"

	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/resource"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/types"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
	logf "sigs.k8s.io/controller-runtime/pkg/log"
	"sigs.k8s.io/controller-runtime/pkg/webhook"
	"sigs.k8s.io/controller-runtime/pkg/webhook/admission"

	quotav1alpha1 "github.com/powerhome/pac-quota-controller/api/v1alpha1"
	"github.com/powerhome/pac-quota-controller/pkg/kubernetes/storage"
)

// nolint:unused
// log is for logging in this package.
var persistentvolumeclaimlog = logf.Log.WithName("persistentvolumeclaim-resource")

// SetupPersistentVolumeClaimWebhookWithManager registers the webhook for PersistentVolumeClaim in the manager.
func SetupPersistentVolumeClaimWebhookWithManager(mgr ctrl.Manager) error {
	storageCalculator := &storage.StorageResourceCalculator{
		Client: mgr.GetClient(),
	}

	return ctrl.NewWebhookManagedBy(mgr).For(&corev1.PersistentVolumeClaim{}).
		WithValidator(&PersistentVolumeClaimCustomValidator{
			Client:            mgr.GetClient(),
			StorageCalculator: storageCalculator,
		}).
		WithDefaulter(&PersistentVolumeClaimCustomDefaulter{}).
		Complete()
}

// TODO(user): EDIT THIS FILE!  THIS IS SCAFFOLDING FOR YOU TO OWN!

// +kubebuilder:webhook:path=/mutate--v1-persistentvolumeclaim,mutating=true,failurePolicy=fail,sideEffects=None,groups="",resources=persistentvolumeclaims,verbs=create;update,versions=v1,name=mpersistentvolumeclaim-v1.kb.io,admissionReviewVersions=v1

// PersistentVolumeClaimCustomDefaulter struct is responsible for setting default values on the custom resource of the
// Kind PersistentVolumeClaim when those are created or updated.
//
// NOTE: The +kubebuilder:object:generate=false marker prevents controller-gen from generating DeepCopy methods,
// as it is used only for temporary operations and does not need to be deeply copied.
type PersistentVolumeClaimCustomDefaulter struct {
	// TODO(user): Add more fields as needed for defaulting
}

var _ webhook.CustomDefaulter = &PersistentVolumeClaimCustomDefaulter{}

// Default implements webhook.CustomDefaulter so a webhook will be registered for the Kind PersistentVolumeClaim.
func (d *PersistentVolumeClaimCustomDefaulter) Default(_ context.Context, obj runtime.Object) error {
	persistentvolumeclaim, ok := obj.(*corev1.PersistentVolumeClaim)

	if !ok {
		return fmt.Errorf("expected an PersistentVolumeClaim object but got %T", obj)
	}
	persistentvolumeclaimlog.Info("Defaulting for PersistentVolumeClaim", "name", persistentvolumeclaim.GetName())

	// TODO(user): fill in your defaulting logic.

	return nil
}

// TODO(user): change verbs to "verbs=create;update;delete" if you want to enable deletion validation.
// NOTE: The 'path' attribute must follow a specific pattern and should not be modified directly here.
// Modifying the path for an invalid path can cause API server errors; failing to locate the webhook.
// +kubebuilder:webhook:path=/validate--v1-persistentvolumeclaim,mutating=false,failurePolicy=fail,sideEffects=None,groups="",resources=persistentvolumeclaims,verbs=create;update,versions=v1,name=vpersistentvolumeclaim-v1.kb.io,admissionReviewVersions=v1

// PersistentVolumeClaimCustomValidator struct is responsible for validating the PersistentVolumeClaim resource
// when it is created, updated, or deleted.
//
// NOTE: The +kubebuilder:object:generate=false marker prevents controller-gen from generating DeepCopy methods,
// as this struct is used only for temporary operations and does not need to be deeply copied.
type PersistentVolumeClaimCustomValidator struct {
	Client            client.Client
	StorageCalculator *storage.StorageResourceCalculator
}

var _ webhook.CustomValidator = &PersistentVolumeClaimCustomValidator{}

// ValidateCreate implements webhook.CustomValidator so a webhook will be registered for the type PersistentVolumeClaim.
func (v *PersistentVolumeClaimCustomValidator) ValidateCreate(ctx context.Context, obj runtime.Object) (admission.Warnings, error) {
	persistentvolumeclaim, ok := obj.(*corev1.PersistentVolumeClaim)
	if !ok {
		return nil, fmt.Errorf("expected a PersistentVolumeClaim object but got %T", obj)
	}
	persistentvolumeclaimlog.Info("Validation for PersistentVolumeClaim upon creation", "name", persistentvolumeclaim.GetName())

	// Check if any ClusterResourceQuota applies to this namespace and would be exceeded
	if err := v.validateStorageQuota(ctx, persistentvolumeclaim); err != nil {
		persistentvolumeclaimlog.Error(err, "PVC creation blocked due to quota violation",
			"pvc", persistentvolumeclaim.GetName(),
			"namespace", persistentvolumeclaim.GetNamespace())
		return nil, err
	}

	return nil, nil
}

// ValidateUpdate implements webhook.CustomValidator so a webhook will be registered for the type PersistentVolumeClaim.
func (v *PersistentVolumeClaimCustomValidator) ValidateUpdate(ctx context.Context, oldObj, newObj runtime.Object) (admission.Warnings, error) {
	persistentvolumeclaim, ok := newObj.(*corev1.PersistentVolumeClaim)
	if !ok {
		return nil, fmt.Errorf("expected a PersistentVolumeClaim object for the newObj but got %T", newObj)
	}
	persistentvolumeclaimlog.Info("Validation for PersistentVolumeClaim upon update", "name", persistentvolumeclaim.GetName())

	// Check if any ClusterResourceQuota applies to this namespace and would be exceeded
	if err := v.validateStorageQuota(ctx, persistentvolumeclaim); err != nil {
		persistentvolumeclaimlog.Error(err, "PVC update blocked due to quota violation",
			"pvc", persistentvolumeclaim.GetName(),
			"namespace", persistentvolumeclaim.GetNamespace())
		return nil, err
	}

	return nil, nil
}

// ValidateDelete implements webhook.CustomValidator so a webhook will be registered for the type PersistentVolumeClaim.
func (v *PersistentVolumeClaimCustomValidator) ValidateDelete(ctx context.Context, obj runtime.Object) (admission.Warnings, error) {
	persistentvolumeclaim, ok := obj.(*corev1.PersistentVolumeClaim)
	if !ok {
		return nil, fmt.Errorf("expected a PersistentVolumeClaim object but got %T", obj)
	}
	persistentvolumeclaimlog.Info("Validation for PersistentVolumeClaim upon deletion", "name", persistentvolumeclaim.GetName())

	// TODO(user): fill in your validation logic upon object deletion.

	return nil, nil
}

// validateStorageQuota checks if creating the PVC would exceed any ClusterResourceQuota limits.
func (v *PersistentVolumeClaimCustomValidator) validateStorageQuota(ctx context.Context, pvc *corev1.PersistentVolumeClaim) error {
	// Get the storage request for this PVC
	storageRequest := getStorageRequest(pvc)
	if storageRequest.IsZero() {
		// No storage request, nothing to validate
		return nil
	}

	// List all ClusterResourceQuotas
	crqList := &quotav1alpha1.ClusterResourceQuotaList{}
	if err := v.Client.List(ctx, crqList); err != nil {
		return fmt.Errorf("failed to list ClusterResourceQuotas: %w", err)
	}

	// Check each CRQ to see if it applies to this namespace and would be exceeded
	for _, crq := range crqList.Items {
		if err := v.checkClusterResourceQuota(ctx, &crq, pvc, storageRequest); err != nil {
			return err
		}
	}

	return nil
}

// checkClusterResourceQuota checks if a specific ClusterResourceQuota would be exceeded by this PVC.
func (v *PersistentVolumeClaimCustomValidator) checkClusterResourceQuota(ctx context.Context, crq *quotav1alpha1.ClusterResourceQuota, pvc *corev1.PersistentVolumeClaim, storageRequest resource.Quantity) error {
	// Check if this CRQ applies to the PVC's namespace
	namespace := &corev1.Namespace{}
	if err := v.Client.Get(ctx, types.NamespacedName{Name: pvc.GetNamespace()}, namespace); err != nil {
		return fmt.Errorf("failed to get namespace %s: %w", pvc.GetNamespace(), err)
	}

	// Check if namespace matches the selector
	if !v.namespaceMatchesSelector(namespace, crq.Spec.NamespaceSelector) {
		// This CRQ doesn't apply to this namespace
		return nil
	}

	// Check storage limits in the quota
	hard := crq.Spec.Hard
	if hard == nil {
		return nil
	}

	// Check for requests.storage limit
	if storageLimit, exists := hard[corev1.ResourceRequestsStorage]; exists {
		// Calculate current usage
		currentUsage, err := v.StorageCalculator.CalculateStorageUsage(ctx, pvc.GetNamespace())
		if err != nil {
			return fmt.Errorf("failed to calculate current storage usage: %w", err)
		}

		// Check if adding this PVC would exceed the limit
		newTotal := currentUsage.DeepCopy()
		newTotal.Add(storageRequest)

		if newTotal.Cmp(storageLimit) > 0 {
			return fmt.Errorf("PVC creation would exceed ClusterResourceQuota %s storage limit: current=%s, requested=%s, limit=%s",
				crq.GetName(), currentUsage.String(), storageRequest.String(), storageLimit.String())
		}
	}

	// Check for persistentvolumeclaims count limit
	if pvcCountLimit, exists := hard[corev1.ResourcePersistentVolumeClaims]; exists {
		// Count current PVCs in the namespace
		pvcList := &corev1.PersistentVolumeClaimList{}
		listOpts := &client.ListOptions{Namespace: pvc.GetNamespace()}
		if err := v.Client.List(ctx, pvcList, listOpts); err != nil {
			return fmt.Errorf("failed to list PVCs in namespace %s: %w", pvc.GetNamespace(), err)
		}

		currentCount := resource.NewQuantity(int64(len(pvcList.Items)), resource.DecimalSI)
		newCount := currentCount.DeepCopy()
		newCount.Add(resource.MustParse("1"))

		if newCount.Cmp(pvcCountLimit) > 0 {
			return fmt.Errorf("PVC creation would exceed ClusterResourceQuota %s PVC count limit: current=%d, limit=%s",
				crq.GetName(), len(pvcList.Items), pvcCountLimit.String())
		}
	}

	// Check for storage class specific quotas
	pvcStorageClass := getPVCStorageClass(pvc)
	if err := v.checkStorageClassQuotas(ctx, crq, pvc, pvcStorageClass, storageRequest); err != nil {
		return err
	}

	return nil
}

// namespaceMatchesSelector checks if a namespace matches the ClusterResourceQuota selector.
func (v *PersistentVolumeClaimCustomValidator) namespaceMatchesSelector(namespace *corev1.Namespace, selector *metav1.LabelSelector) bool {
	if selector == nil {
		return false
	}

	// Check label selector
	if selector.MatchLabels != nil {
		for key, value := range selector.MatchLabels {
			if namespaceValue, exists := namespace.Labels[key]; !exists || namespaceValue != value {
				return false
			}
		}
	}

	// TODO: Handle MatchExpressions if needed
	// For now, we only support MatchLabels

	return true
}

// getStorageRequest extracts the storage request from a PVC.
func getStorageRequest(pvc *corev1.PersistentVolumeClaim) resource.Quantity {
	if pvc.Spec.Resources.Requests == nil {
		return resource.Quantity{}
	}

	if storageRequest, ok := pvc.Spec.Resources.Requests[corev1.ResourceStorage]; ok {
		return storageRequest
	}

	return resource.Quantity{}
}

// getPVCStorageClass extracts the storage class from a PVC.
func getPVCStorageClass(pvc *corev1.PersistentVolumeClaim) string {
	if pvc.Spec.StorageClassName == nil {
		return ""
	}
	return *pvc.Spec.StorageClassName
}

// checkStorageClassQuotas checks storage class specific quota limits.
// Supports quotas like: <storage-class-name>.storageclass.storage.k8s.io/requests.storage
// and: <storage-class-name>.storageclass.storage.k8s.io/persistentvolumeclaims
func (v *PersistentVolumeClaimCustomValidator) checkStorageClassQuotas(ctx context.Context, crq *quotav1alpha1.ClusterResourceQuota, pvc *corev1.PersistentVolumeClaim, storageClass string, storageRequest resource.Quantity) error {
	hard := crq.Spec.Hard
	if hard == nil {
		return nil
	}

	// Iterate through all quota constraints to find storage class specific ones
	for resourceName, limit := range hard {
		resourceNameStr := string(resourceName)

		// Check for storage class specific storage quota: <storage-class>.storageclass.storage.k8s.io/requests.storage
		storageQuotaSuffix := ".storageclass.storage.k8s.io/requests.storage"
		if len(resourceNameStr) > len(storageQuotaSuffix) && resourceNameStr[len(resourceNameStr)-len(storageQuotaSuffix):] == storageQuotaSuffix {
			quotaStorageClass := resourceNameStr[:len(resourceNameStr)-len(storageQuotaSuffix)]

			// Only check if this PVC matches the storage class
			if storageClass == quotaStorageClass {
				currentUsage, err := v.StorageCalculator.CalculateStorageClassUsage(ctx, pvc.GetNamespace(), storageClass)
				if err != nil {
					return fmt.Errorf("failed to calculate storage class usage for %s: %w", storageClass, err)
				}

				newTotal := currentUsage.DeepCopy()
				newTotal.Add(storageRequest)

				if newTotal.Cmp(limit) > 0 {
					return fmt.Errorf("PVC creation would exceed ClusterResourceQuota %s storage class %s storage limit: current=%s, requested=%s, limit=%s",
						crq.GetName(), storageClass, currentUsage.String(), storageRequest.String(), limit.String())
				}
			}
		}

		// Check for storage class specific PVC count quota: <storage-class>.storageclass.storage.k8s.io/persistentvolumeclaims
		pvcCountQuotaSuffix := ".storageclass.storage.k8s.io/persistentvolumeclaims"
		if len(resourceNameStr) > len(pvcCountQuotaSuffix) && resourceNameStr[len(resourceNameStr)-len(pvcCountQuotaSuffix):] == pvcCountQuotaSuffix {
			quotaStorageClass := resourceNameStr[:len(resourceNameStr)-len(pvcCountQuotaSuffix)]

			// Only check if this PVC matches the storage class
			if storageClass == quotaStorageClass {
				currentCount, err := v.StorageCalculator.CalculateStorageClassCount(ctx, pvc.GetNamespace(), storageClass)
				if err != nil {
					return fmt.Errorf("failed to calculate storage class PVC count for %s: %w", storageClass, err)
				}

				newCount := currentCount + 1

				// Convert limit to int64 for comparison
				limitInt := limit.Value()
				if newCount > limitInt {
					return fmt.Errorf("PVC creation would exceed ClusterResourceQuota %s storage class %s PVC count limit: current=%d, limit=%d",
						crq.GetName(), storageClass, currentCount, limitInt)
				}
			}
		}
	}

	return nil
}
