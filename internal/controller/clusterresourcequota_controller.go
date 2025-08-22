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

package controller

import (
	"context"
	"fmt"
	"slices"
	"sort"
	"strings"

	quotav1alpha1 "github.com/powerhome/pac-quota-controller/api/v1alpha1"
	"github.com/powerhome/pac-quota-controller/pkg/kubernetes/pod"
	"github.com/powerhome/pac-quota-controller/pkg/kubernetes/quota"
	"github.com/powerhome/pac-quota-controller/pkg/kubernetes/storage"
	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/api/resource"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/labels"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/client-go/kubernetes"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/builder"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/event"
	"sigs.k8s.io/controller-runtime/pkg/handler"
	logf "sigs.k8s.io/controller-runtime/pkg/log"
	"sigs.k8s.io/controller-runtime/pkg/predicate"
	"sigs.k8s.io/controller-runtime/pkg/reconcile"
)

var log = logf.Log.WithName("clusterresourcequota-controller")

// resourceUpdatePredicate implements a custom predicate function to filter resource updates.
// It's designed to trigger reconciliation only on meaningful changes, such as spec updates
// or pod phase changes to/from terminal states, while ignoring noisy status-only updates.
// Pods going from pending to running or from running to pending
// are not considered terminal and do not trigger reconciliation.
// AKA should be accounted for the resource usage
type resourceUpdatePredicate struct {
	predicate.Funcs
}

// Update implements the update event filter.
func (resourceUpdatePredicate) Update(e event.UpdateEvent) bool {
	if e.ObjectOld == nil || e.ObjectNew == nil {
		// Invalid event, ignore
		return false
	}

	// Always reconcile if the object's generation changes (i.e., spec was updated).
	if e.ObjectNew.GetGeneration() != e.ObjectOld.GetGeneration() {
		return true
	}

	// Special handling for Pods: reconcile if the pod transitions to or from a terminal state.
	// This is important for releasing quota resources when a pod completes.
	if podOld, ok := e.ObjectOld.(*corev1.Pod); ok {
		if podNew, ok := e.ObjectNew.(*corev1.Pod); ok {
			if pod.IsPodTerminal(podOld) != pod.IsPodTerminal(podNew) {
				return true
			}
		}
	}

	// For all other cases, if the generation hasn't changed, ignore the update event.
	// This prevents reconciliation loops caused by the controller's own status updates on the CRQ
	// or other insignificant status changes on watched resources.
	return false
}

// Delete implements the delete event filter.
func (resourceUpdatePredicate) Delete(e event.DeleteEvent) bool {
	if e.Object == nil {
		// Invalid event, ignore
		return false
	}

	// Trigger reconciliation on pod deletions
	if _, ok := e.Object.(*corev1.Pod); ok {
		return true
	}

	return false
}

// ClusterResourceQuotaReconciler reconciles a ClusterResourceQuota object
type ClusterResourceQuotaReconciler struct {
	client.Client
	Scheme                   *runtime.Scheme
	crqClient                quota.CRQClientInterface
	ComputeCalculator        *pod.PodResourceCalculator
	StorageCalculator        *storage.StorageResourceCalculator
	ExcludeNamespaceLabelKey string
	ExcludedNamespaces       []string
}

// isNamespaceExcluded checks if a namespace should be ignored by the controller.
// It checks if the namespace is the controller's own namespace, in the excluded list, or has the exclusion label.
func (r *ClusterResourceQuotaReconciler) isNamespaceExcluded(ns *corev1.Namespace) bool {
	if slices.Contains(r.ExcludedNamespaces, ns.Name) {
		return true
	}
	if r.ExcludeNamespaceLabelKey == "" {
		return false
	}
	_, hasLabel := ns.Labels[r.ExcludeNamespaceLabelKey]
	return hasLabel
}

// Reconcile is part of the main kubernetes reconciliation loop which aims to
// move the current state of the cluster closer to the desired state.
// It implements the logic to select namespaces, calculate aggregate usage,
// and update the ClusterResourceQuota status.
//
// For more details, check Reconcile and its Result here:
// - https://pkg.go.dev/sigs.k8s.io/controller-runtime@v0.21.0/pkg/reconcile
func (r *ClusterResourceQuotaReconciler) Reconcile(ctx context.Context, req ctrl.Request) (ctrl.Result, error) {
	log := log.WithValues("clusterresourcequota", req.NamespacedName)
	log.Info("Reconciling ClusterResourceQuota")

	// Fetch the ClusterResourceQuota instance
	crq := &quotav1alpha1.ClusterResourceQuota{}
	if err := r.Get(ctx, req.NamespacedName, crq); err != nil {
		if errors.IsNotFound(err) {
			// Object not found, likely deleted, return without error
			log.Info("ClusterResourceQuota resource not found. Ignoring since object must have been deleted")
			return ctrl.Result{}, nil
		}
		// Error reading the object - requeue the request
		log.Error(err, "Failed to get ClusterResourceQuota")
		return ctrl.Result{}, err
	}

	// Get the list of selected namespaces, filtering out excluded ones.
	var selectedNamespaces []string
	if crq.Spec.NamespaceSelector != nil {
		selector, err := metav1.LabelSelectorAsSelector(crq.Spec.NamespaceSelector)
		if err != nil {
			return ctrl.Result{}, fmt.Errorf("failed to create selector from CRQ spec: %w", err)
		}

		namespaceList := &corev1.NamespaceList{}
		listOpts := &client.ListOptions{
			LabelSelector: selector,
		}

		if err := r.List(ctx, namespaceList, listOpts); err != nil {
			log.Error(err, "Failed to list namespaces")
			return ctrl.Result{}, err
		}

		for _, ns := range namespaceList.Items {
			if r.isNamespaceExcluded(&ns) {
				continue
			}
			if selector.Matches(labels.Set(ns.Labels)) {
				selectedNamespaces = append(selectedNamespaces, ns.Name)
			}
		}
		sort.Strings(selectedNamespaces)
	}

	log.Info("Found namespaces matching selection criteria", "count", len(selectedNamespaces), "namespaces", selectedNamespaces)

	// Calculate aggregated resource usage across all selected namespaces
	totalUsage, usageByNamespace := r.calculateAndAggregateUsage(ctx, crq, selectedNamespaces)

	// Update the status of the ClusterResourceQuota
	if err := r.updateStatus(ctx, crq, totalUsage, usageByNamespace); err != nil {
		log.Error(err, "Failed to update ClusterResourceQuota status")
		return ctrl.Result{}, err
	}

	log.Info("Finished reconciliation")
	return ctrl.Result{}, nil
}

// calculateAndAggregateUsage calculates the current resource usage for the given CRQ.
func (r *ClusterResourceQuotaReconciler) calculateAndAggregateUsage(
	ctx context.Context,
	crq *quotav1alpha1.ClusterResourceQuota,
	namespaces []string,
) (quotav1alpha1.ResourceList, []quotav1alpha1.ResourceQuotaStatusByNamespace) {
	log.Info("Calculating resource usage", "crq", crq.Name)

	totalUsage := make(quotav1alpha1.ResourceList)
	usageByNamespace := make([]quotav1alpha1.ResourceQuotaStatusByNamespace, len(namespaces))
	nsIndexMap := make(map[string]int)

	// Initialize maps for efficient lookup
	for i, nsName := range namespaces {
		usageByNamespace[i] = quotav1alpha1.ResourceQuotaStatusByNamespace{
			Namespace: nsName,
			Status: quotav1alpha1.ResourceQuotaStatus{
				Used: make(quotav1alpha1.ResourceList),
			},
		}
		nsIndexMap[nsName] = i
	}

	// Iterate over each resource defined in the CRQ spec
	for resourceName := range crq.Spec.Hard {
		// Initialize total usage for this resource
		totalUsage[resourceName] = resource.Quantity{}

		// Detect storage class–scoped quota resources
		resourceStr := string(resourceName)
		if strings.HasSuffix(resourceStr, ".storageclass.storage.k8s.io/requests.storage") {
			// Example: fast-ssd.storageclass.storage.k8s.io/requests.storage
			storageClass := strings.TrimSuffix(resourceStr, ".storageclass.storage.k8s.io/requests.storage")
			for _, nsName := range namespaces {
				var currentUsage resource.Quantity
				if r.StorageCalculator != nil {
					usage, err := r.StorageCalculator.CalculateStorageClassUsage(ctx, nsName, storageClass)
					if err != nil {
						log.Error(err, "Failed to calculate storage class usage", "resource", resourceName, "namespace", nsName, "storageClass", storageClass)
						currentUsage = resource.MustParse("0")
					} else {
						currentUsage = usage
					}
				} else {
					log.Error(nil, "StorageCalculator is nil", "namespace", nsName, "resource", resourceName)
					currentUsage = resource.MustParse("0")
				}
				nsIndex := nsIndexMap[nsName]
				usageByNamespace[nsIndex].Status.Used[resourceName] = currentUsage
				if existing, exists := totalUsage[resourceName]; exists {
					existing.Add(currentUsage)
					totalUsage[resourceName] = existing
				} else {
					totalUsage[resourceName] = currentUsage
				}
			}
			continue
		}
		if strings.HasSuffix(resourceStr, ".storageclass.storage.k8s.io/persistentvolumeclaims") {
			// Example: fast-ssd.storageclass.storage.k8s.io/persistentvolumeclaims
			storageClass := strings.TrimSuffix(resourceStr, ".storageclass.storage.k8s.io/persistentvolumeclaims")
			for _, nsName := range namespaces {
				var currentCount int64
				if r.StorageCalculator != nil {
					count, err := r.StorageCalculator.CalculateStorageClassCount(ctx, nsName, storageClass)
					if err != nil {
						log.Error(err, "Failed to calculate storage class PVC count", "resource", resourceName, "namespace", nsName, "storageClass", storageClass)
						currentCount = 0
					} else {
						currentCount = count
					}
				} else {
					log.Error(nil, "StorageCalculator is nil", "namespace", nsName, "resource", resourceName)
					currentCount = 0
				}
				nsIndex := nsIndexMap[nsName]
				usageByNamespace[nsIndex].Status.Used[resourceName] = *resource.NewQuantity(currentCount, resource.DecimalSI)
				if existing, exists := totalUsage[resourceName]; exists {
					existing.Add(*resource.NewQuantity(currentCount, resource.DecimalSI))
					totalUsage[resourceName] = existing
				} else {
					totalUsage[resourceName] = *resource.NewQuantity(currentCount, resource.DecimalSI)
				}
			}
			continue
		}

		for _, nsName := range namespaces {
			var currentUsage resource.Quantity

			// Dispatch to the correct calculation function based on the resource type
			switch resourceName {
			case corev1.ResourcePods, corev1.ResourceServices, corev1.ResourceConfigMaps, corev1.ResourceSecrets, corev1.ResourcePersistentVolumeClaims:
				currentUsage = r.calculateObjectCount(ctx, nsName, resourceName)
			case corev1.ResourceRequestsCPU, corev1.ResourceRequestsMemory, corev1.ResourceLimitsCPU, corev1.ResourceLimitsMemory:
				currentUsage = r.calculateComputeResources(ctx, nsName, resourceName)
			case corev1.ResourceRequestsStorage:
				currentUsage = r.calculateStorageResources(ctx, nsName, resourceName)
			default:
				// Handle extended resources (hugepages, GPUs, etc.) via compute calculator
				// Extended resources are typically consumed by pods, so they should be calculated
				// using the compute resource calculator
				// TODO: fix this, temporary workaround
				if r.isComputeResource(resourceName) {
					currentUsage = r.calculateComputeResources(ctx, nsName, resourceName)
				} else {
					log.Info("Unsupported resource type for quota calculation", "resource", resourceName)
					continue
				}
			}
			// Update usage for the specific namespace
			nsIndex := nsIndexMap[nsName]
			usageByNamespace[nsIndex].Status.Used[resourceName] = currentUsage

			// Aggregate total usage correctly
			// Since resource.Quantity has pointer receiver methods, we need to be careful
			// about how we handle the aggregation
			if existing, exists := totalUsage[resourceName]; exists {
				existing.Add(currentUsage)
				totalUsage[resourceName] = existing
			} else {
				totalUsage[resourceName] = currentUsage
			}
		}
	}

	log.Info("Usage calculation finished.")
	return totalUsage, usageByNamespace
}

// calculateObjectCount calculates the usage for object count quotas.
func (r *ClusterResourceQuotaReconciler) calculateObjectCount(_ context.Context, ns string, resourceName corev1.ResourceName) resource.Quantity {
	// TODO: Implement listing and counting for the specific object type (e.g., Pods, Services).
	// This will involve creating a client.ObjectList for the correct type and listing
	// it with a namespace filter.
	log.Info("Placeholder: Calculating object count", "resource", resourceName, "namespace", ns)
	return resource.MustParse("0")
}

// calculateComputeResources calculates the usage for compute resource quotas (CPU/Memory).
func (r *ClusterResourceQuotaReconciler) calculateComputeResources(ctx context.Context, ns string, resourceName corev1.ResourceName) resource.Quantity {
	usage, err := r.ComputeCalculator.CalculateUsage(ctx, ns, resourceName)
	if err != nil {
		log.Error(err, "Failed to calculate compute resources", "resource", resourceName, "namespace", ns)
		return resource.MustParse("0")
	}
	return usage
}

// calculateStorageResources calculates the usage for storage resource quotas.
func (r *ClusterResourceQuotaReconciler) calculateStorageResources(ctx context.Context, ns string, resourceName corev1.ResourceName) resource.Quantity {
	if r.StorageCalculator == nil {
		log.Error(nil, "StorageCalculator is nil", "namespace", ns, "resource", resourceName)
		return resource.MustParse("0")
	}

	usage, err := r.StorageCalculator.CalculateUsage(ctx, ns, resourceName)
	if err != nil {
		log.Error(err, "Failed to calculate storage resources", "resource", resourceName, "namespace", ns)
		return resource.MustParse("0")
	}
	return usage
}

// updateStatus updates the status of the ClusterResourceQuota object.
func (r *ClusterResourceQuotaReconciler) updateStatus(
	ctx context.Context,
	crq *quotav1alpha1.ClusterResourceQuota,
	totalUsage quotav1alpha1.ResourceList,
	usageByNamespace []quotav1alpha1.ResourceQuotaStatusByNamespace,
) error {
	crqCopy := crq.DeepCopy()
	crqCopy.Status.Total.Hard = crq.Spec.Hard
	crqCopy.Status.Total.Used = totalUsage
	crqCopy.Status.Namespaces = usageByNamespace

	// Use Patch instead of Update to avoid conflicts
	return r.Status().Patch(ctx, crqCopy, client.MergeFrom(crq))
}

// findQuotasForObject maps objects (including Namespaces and other namespaced resources) to ClusterResourceQuota requests
// that should be reconciled based on namespace selection criteria. This unified function handles both:
// - Namespace objects directly (when namespaces are created, updated, or deleted)
// - Other namespaced objects (Pods, Services, etc.) by first retrieving their namespace
// It excludes the controller's own namespace and any namespaces marked with the exclusion label.
func (r *ClusterResourceQuotaReconciler) findQuotasForObject(ctx context.Context, obj client.Object) []reconcile.Request {
	// Handle nil object gracefully
	if obj == nil {
		return nil
	}

	var ns *corev1.Namespace
	var err error

	// Handle Namespace objects directly
	if namespace, ok := obj.(*corev1.Namespace); ok {
		ns = namespace
	} else {
		// For cluster-scoped resources, return nil (no quota mapping needed)
		namespaceName := obj.GetNamespace()
		if namespaceName == "" {
			return nil
		}

		// For other objects, get the namespace they belong to
		ns = &corev1.Namespace{}
		if err = r.Get(ctx, types.NamespacedName{Name: namespaceName}, ns); err != nil {
			log.Error(err, "Failed to get namespace for object to check for exclusion", "object", client.ObjectKeyFromObject(obj))
			return nil
		}
	}

	if r.isNamespaceExcluded(ns) {
		return nil // Ignore events from excluded namespaces
	}

	// Get object GVK for better logging
	gvk := obj.GetObjectKind().GroupVersionKind()
	log.Info("Processing object event, finding relevant CRQs",
		"object", client.ObjectKeyFromObject(obj),
		"group", gvk.Group,
		"version", gvk.Version,
		"kind", gvk.Kind,
		"namespace", ns.Name)

	// Find which CRQ selects this namespace.
	crq, err := r.crqClient.GetCRQByNamespace(ctx, ns)
	if err != nil {
		log.Error(err, "Failed to get ClusterResourceQuota for namespace")
		return nil
	}

	if crq != nil {
		return []reconcile.Request{
			{
				NamespacedName: types.NamespacedName{
					Name: crq.Name,
				},
			},
		}
	}

	return nil
}

// TODO: Improve this, temporary workaround to handle compute resources.
// isComputeResource determines if a resource type should be calculated using the compute calculator.
// This includes standard compute resources and extended resources (hugepages, GPUs, etc.)
func (r *ClusterResourceQuotaReconciler) isComputeResource(resourceName corev1.ResourceName) bool {
	resourceStr := string(resourceName)

	// Standard compute resources (already handled in switch above, but included for completeness)
	switch resourceName {
	case corev1.ResourceRequestsCPU, corev1.ResourceRequestsMemory, corev1.ResourceLimitsCPU, corev1.ResourceLimitsMemory, corev1.ResourceRequestsEphemeralStorage:
		return true
	}

	// Extended resources patterns
	// Hugepages resources follow the pattern "hugepages-<size>"
	if strings.HasPrefix(resourceStr, "hugepages-") {
		return true
	}

	// GPU resources typically contain "gpu" in the name
	if strings.Contains(strings.ToLower(resourceStr), "gpu") {
		return true
	}

	// Extended resources typically have domain-style names (contain dots)
	// Examples: nvidia.com/gpu, example.com/foo, intel.com/qat
	if strings.Contains(resourceStr, ".") {
		return true
	}

	// If we can't categorize it, assume it's not a compute resource
	return false
}

// SetupWithManager sets up the controller with the Manager.
func (r *ClusterResourceQuotaReconciler) SetupWithManager(mgr ctrl.Manager) error {
	k8sConfig := mgr.GetConfig()
	clientset, err := kubernetes.NewForConfig(k8sConfig)
	if err != nil {
		return fmt.Errorf("unable to create kubernetes clientset: %w", err)
	}
	if r.crqClient == nil {
		r.crqClient = quota.NewCRQClient(r.Client)
	}

	if r.StorageCalculator == nil {
		r.StorageCalculator = storage.NewStorageResourceCalculator(clientset)
	}
	if r.ComputeCalculator == nil {
		r.ComputeCalculator = pod.NewPodResourceCalculator(clientset)
	}

	log.Info("Setting up ClusterResourceQuota controller")

	// Predicate to filter out updates to status subresource
	// This prevents reconcile loops caused by status updates
	// Not sure about this one, but seems to reduce noise
	// Couldn't find much examples of this in the wild
	resourcePredicate := resourceUpdatePredicate{}

	b := ctrl.NewControllerManagedBy(mgr).
		For(&quotav1alpha1.ClusterResourceQuota{})

		// Watch for changes to tracked resources and trigger reconciliation for associated CRQs
	watchedObjectTypes := []struct {
		obj   client.Object
		preds []predicate.Predicate
	}{
		{&corev1.Namespace{}, nil},
		{&corev1.Pod{}, []predicate.Predicate{resourcePredicate}},
		{&corev1.PersistentVolumeClaim{}, nil},
	}
	for _, w := range watchedObjectTypes {
		b = b.Watches(
			w.obj,
			handler.EnqueueRequestsFromMapFunc(r.findQuotasForObject),
			builder.WithPredicates(w.preds...),
		)
	}

	return b.Named("clusterresourcequota").
		Complete(r)
}
