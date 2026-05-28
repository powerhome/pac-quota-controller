package controller

import (
	"context"
	"fmt"
	"strings"
	"time"

	quotav1alpha1 "github.com/powerhome/pac-quota-controller/api/v1alpha1"
	"github.com/powerhome/pac-quota-controller/pkg/kubernetes/pod"
	"github.com/powerhome/pac-quota-controller/pkg/kubernetes/services"
	"github.com/powerhome/pac-quota-controller/pkg/kubernetes/storage"
	"github.com/powerhome/pac-quota-controller/pkg/kubernetes/usage"
	"github.com/powerhome/pac-quota-controller/pkg/metrics"
	"github.com/prometheus/client_golang/prometheus"
	"go.uber.org/zap"
	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/resource"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

// calculateAndAggregateUsage calculates the current resource usage for the given CRQ.
func (r *ClusterResourceQuotaReconciler) calculateAndAggregateUsage(
	ctx context.Context,
	crq *quotav1alpha1.ClusterResourceQuota,
	namespaces []string,
) (quotav1alpha1.ResourceList, []quotav1alpha1.ResourceQuotaStatusByNamespace, error) {
	r.logger.Debug("Calculating resource usage", zap.String("crq", crq.Name))
	timer := prometheus.NewTimer(metrics.QuotaAggregationDuration.WithLabelValues(crq.Name))
	defer timer.ObserveDuration()

	totalUsage := make(quotav1alpha1.ResourceList)
	usageByNamespace := make([]quotav1alpha1.ResourceQuotaStatusByNamespace, len(namespaces))
	nsIndexMap := make(map[string]int)
	namespaceDirtyStates := r.buildNamespaceDirtyStates(crq.Name, namespaces)

	prefetchPlan := buildNamespacePrefetchPlan(
		r,
		crq,
		namespaces,
		namespaceDirtyStates,
	)

	resourceSnapshots := r.loadNamespaceResourceSnapshots(ctx, crq, namespaces, prefetchPlan)

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

	for resourceName := range crq.Spec.Hard {
		totalUsage[resourceName] = resource.Quantity{}

		handled, err := r.processStorageClassResourceUsage(
			ctx,
			crq,
			namespaces,
			nsIndexMap,
			resourceName,
			usageByNamespace,
			totalUsage,
			resourceSnapshots,
			namespaceDirtyStates,
		)
		if err != nil {
			return nil, nil, err
		}
		if handled {
			continue
		}

		if err := r.processStandardResourceUsage(
			ctx,
			crq,
			namespaces,
			nsIndexMap,
			resourceName,
			usageByNamespace,
			totalUsage,
			resourceSnapshots,
			namespaceDirtyStates,
		); err != nil {
			return nil, nil, err
		}
	}

	r.consumeNamespaceDirtyStates(crq.Name, namespaceDirtyStates)

	r.logger.Debug("Usage calculation finished.")
	return totalUsage, usageByNamespace, nil
}

func (r *ClusterResourceQuotaReconciler) buildNamespaceDirtyStates(
	quotaName string,
	namespaces []string,
) map[string]namespaceUsageDirtyState {
	namespaceDirtyStates := make(map[string]namespaceUsageDirtyState, len(namespaces))
	for i := range namespaces {
		nsName := namespaces[i]
		if nsName == "" {
			continue
		}

		dirtyState := namespaceUsageDirtyState{}
		if r.usageStateStore != nil {
			if currentState, found := r.usageStateStore.getNamespaceDirtyState(quotaName, nsName); found {
				dirtyState = currentState
			}
		}

		namespaceDirtyStates[nsName] = dirtyState
	}

	return namespaceDirtyStates
}

func (r *ClusterResourceQuotaReconciler) loadNamespaceResourceSnapshots(
	ctx context.Context,
	crq *quotav1alpha1.ClusterResourceQuota,
	namespaces []string,
	prefetchPlan namespacePrefetchPlan,
) map[string]namespaceResourceSnapshot {
	resourceSnapshots := map[string]namespaceResourceSnapshot{}
	if !shouldPrefetchNamespaceResources(crq.Spec.Hard) {
		return resourceSnapshots
	}

	currentSnapshots, err := r.prefetchNamespaceResourcesWithPlan(ctx, namespaces, prefetchPlan)
	if err != nil {
		r.logger.Warn(
			"Failed to prefetch namespace resources, falling back to calculators",
			zap.Error(err),
			zap.String("crq", crq.Name),
		)
		return nil
	}

	return currentSnapshots
}

func (r *ClusterResourceQuotaReconciler) processStorageClassResourceUsage(
	ctx context.Context,
	crq *quotav1alpha1.ClusterResourceQuota,
	namespaces []string,
	nsIndexMap map[string]int,
	resourceName corev1.ResourceName,
	usageByNamespace []quotav1alpha1.ResourceQuotaStatusByNamespace,
	totalUsage quotav1alpha1.ResourceList,
	resourceSnapshots map[string]namespaceResourceSnapshot,
	namespaceDirtyStates map[string]namespaceUsageDirtyState,
) (bool, error) {
	resourceStr := string(resourceName)
	if strings.HasSuffix(resourceStr, ".storageclass.storage.k8s.io/requests.storage") {
		storageClass := strings.TrimSuffix(resourceStr, ".storageclass.storage.k8s.io/requests.storage")
		for _, nsName := range namespaces {
			currentUsage, err := r.resolveStorageClassUsageWithCache(
				ctx,
				crq,
				nsName,
				resourceName,
				storageClass,
				resourceSnapshots,
				namespaceDirtyStates,
			)
			if err != nil {
				return true, err
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

		return true, nil
	}

	if strings.HasSuffix(resourceStr, ".storageclass.storage.k8s.io/persistentvolumeclaims") {
		storageClass := strings.TrimSuffix(resourceStr, ".storageclass.storage.k8s.io/persistentvolumeclaims")
		for _, nsName := range namespaces {
			currentCount, err := r.resolveStorageClassCountWithCache(
				ctx,
				crq,
				nsName,
				resourceName,
				storageClass,
				resourceSnapshots,
				namespaceDirtyStates,
			)
			if err != nil {
				return true, err
			}

			nsIndex := nsIndexMap[nsName]
			quantity := *resource.NewQuantity(currentCount, resource.DecimalSI)
			usageByNamespace[nsIndex].Status.Used[resourceName] = quantity
			if existing, exists := totalUsage[resourceName]; exists {
				existing.Add(quantity)
				totalUsage[resourceName] = existing
			} else {
				totalUsage[resourceName] = quantity
			}
		}

		return true, nil
	}

	return false, nil
}

func (r *ClusterResourceQuotaReconciler) processStandardResourceUsage(
	ctx context.Context,
	crq *quotav1alpha1.ClusterResourceQuota,
	namespaces []string,
	nsIndexMap map[string]int,
	resourceName corev1.ResourceName,
	usageByNamespace []quotav1alpha1.ResourceQuotaStatusByNamespace,
	totalUsage quotav1alpha1.ResourceList,
	resourceSnapshots map[string]namespaceResourceSnapshot,
	namespaceDirtyStates map[string]namespaceUsageDirtyState,
) error {
	for _, nsName := range namespaces {
		if nsName == "" {
			r.logger.Info("Skipping usage calculation for empty namespace name")
			continue
		}

		stepName := r.aggregationStepForResource(resourceName)
		stepStart := time.Now()

		currentUsage, calcErr := r.resolveNamespaceResourceUsageWithCache(
			ctx,
			crq,
			nsName,
			resourceName,
			resourceSnapshots,
			namespaceDirtyStates,
		)
		if calcErr != nil {
			metrics.QuotaAggregationStepDuration.WithLabelValues(crq.Name, stepName).Observe(time.Since(stepStart).Seconds())
			return calcErr
		}

		metrics.QuotaAggregationStepDuration.WithLabelValues(crq.Name, stepName).Observe(time.Since(stepStart).Seconds())

		nsIndex := nsIndexMap[nsName]
		usageByNamespace[nsIndex].Status.Used[resourceName] = currentUsage
		if existing, exists := totalUsage[resourceName]; exists {
			existing.Add(currentUsage)
			totalUsage[resourceName] = existing
		} else {
			totalUsage[resourceName] = currentUsage
		}
	}

	return nil
}

func (r *ClusterResourceQuotaReconciler) resolveStorageClassUsageWithCache(
	ctx context.Context,
	crq *quotav1alpha1.ClusterResourceQuota,
	nsName string,
	resourceName corev1.ResourceName,
	storageClass string,
	resourceSnapshots map[string]namespaceResourceSnapshot,
	namespaceDirtyStates map[string]namespaceUsageDirtyState,
) (resource.Quantity, error) {
	stepStart := time.Now()
	var currentUsage resource.Quantity
	var calcErr error

	if !namespaceDirtyForCategory(namespaceDirtyStates[nsName], resourceCategoryStorage) {
		if cachedUsage, found := cachedNamespaceResourceUsage(crq.Status.Namespaces, nsName, resourceName); found {
			currentUsage = cachedUsage
		} else {
			currentUsage, calcErr = r.resolveStorageClassUsage(ctx, nsName, storageClass, resourceName, resourceSnapshots)
		}
	} else {
		currentUsage, calcErr = r.resolveStorageClassUsage(ctx, nsName, storageClass, resourceName, resourceSnapshots)
	}

	metrics.QuotaAggregationStepDuration.WithLabelValues(crq.Name, "storage_class_usage").Observe(time.Since(stepStart).Seconds())
	if calcErr != nil {
		return resource.Quantity{}, calcErr
	}

	return currentUsage, nil
}

func (r *ClusterResourceQuotaReconciler) resolveStorageClassCountWithCache(
	ctx context.Context,
	crq *quotav1alpha1.ClusterResourceQuota,
	nsName string,
	resourceName corev1.ResourceName,
	storageClass string,
	resourceSnapshots map[string]namespaceResourceSnapshot,
	namespaceDirtyStates map[string]namespaceUsageDirtyState,
) (int64, error) {
	stepStart := time.Now()
	var currentCount int64
	var calcErr error

	if !namespaceDirtyForCategory(namespaceDirtyStates[nsName], resourceCategoryStorage) {
		if cachedUsage, found := cachedNamespaceResourceUsage(crq.Status.Namespaces, nsName, resourceName); found {
			currentCount = cachedUsage.Value()
		} else {
			currentCount, calcErr = r.resolveStorageClassCount(ctx, nsName, storageClass, resourceName, resourceSnapshots)
		}
	} else {
		currentCount, calcErr = r.resolveStorageClassCount(ctx, nsName, storageClass, resourceName, resourceSnapshots)
	}

	metrics.QuotaAggregationStepDuration.WithLabelValues(crq.Name, "storage_class_count").Observe(time.Since(stepStart).Seconds())
	if calcErr != nil {
		return 0, calcErr
	}

	return currentCount, nil
}

func (r *ClusterResourceQuotaReconciler) consumeNamespaceDirtyStates(
	quotaName string,
	namespaceDirtyStates map[string]namespaceUsageDirtyState,
) {
	if r.usageStateStore == nil {
		return
	}

	for nsName, dirtyState := range namespaceDirtyStates {
		if dirtyState.Compute {
			r.usageStateStore.consumeNamespaceComputeDirty(quotaName, nsName)
		}
		if dirtyState.Services {
			r.usageStateStore.consumeNamespaceServicesDirty(quotaName, nsName)
		}
		if dirtyState.Storage {
			r.usageStateStore.consumeNamespaceStorageDirty(quotaName, nsName)
		}
		if dirtyState.ObjectCount {
			r.usageStateStore.consumeNamespaceObjectCountDirty(quotaName, nsName)
		}
	}
}

func (r *ClusterResourceQuotaReconciler) resolveNamespaceResourceUsageWithCache(
	ctx context.Context,
	crq *quotav1alpha1.ClusterResourceQuota,
	nsName string,
	resourceName corev1.ResourceName,
	resourceSnapshots map[string]namespaceResourceSnapshot,
	namespaceDirtyStates map[string]namespaceUsageDirtyState,
) (resource.Quantity, error) {
	if !namespaceDirtyForResource(r, namespaceDirtyStates[nsName], resourceName) {
		if cachedUsage, found := cachedNamespaceResourceUsage(crq.Status.Namespaces, nsName, resourceName); found {
			return cachedUsage, nil
		}
	}

	return r.resolveNamespaceResourceUsage(ctx, nsName, resourceName, resourceSnapshots)
}

func (r *ClusterResourceQuotaReconciler) resolveStorageClassUsage(
	ctx context.Context,
	nsName, storageClass string,
	resourceName corev1.ResourceName,
	resourceSnapshots map[string]namespaceResourceSnapshot,
) (resource.Quantity, error) {
	if snapshot, ok := resourceSnapshots[nsName]; ok {
		return calculateStorageClassUsageFromPVCs(snapshot.PVCs, storageClass), nil
	}

	if r.StorageCalculator == nil {
		r.logger.Error("StorageCalculator is nil",
			zap.String("namespace", nsName), zap.Stringer("resource", resourceName))
		return resource.MustParse("0"), nil
	}

	storageUsage, err := r.StorageCalculator.CalculateStorageClassUsage(ctx, nsName, storageClass)
	if err != nil {
		return resource.Quantity{}, fmt.Errorf("failed to calculate storage class usage for %s in %s: %w",
			storageClass, nsName, err)
	}

	return storageUsage, nil
}

func (r *ClusterResourceQuotaReconciler) resolveStorageClassCount(
	ctx context.Context,
	nsName, storageClass string,
	resourceName corev1.ResourceName,
	resourceSnapshots map[string]namespaceResourceSnapshot,
) (int64, error) {
	if snapshot, ok := resourceSnapshots[nsName]; ok {
		return calculateStorageClassCountFromPVCs(snapshot.PVCs, storageClass), nil
	}

	if r.StorageCalculator == nil {
		r.logger.Error("StorageCalculator is nil",
			zap.String("namespace", nsName), zap.Stringer("resource", resourceName))
		return 0, nil
	}

	count, err := r.StorageCalculator.CalculateStorageClassCount(ctx, nsName, storageClass)
	if err != nil {
		return 0, fmt.Errorf("failed to calculate storage class PVC count for %s in %s: %w",
			storageClass, nsName, err)
	}

	return count, nil
}

func isServiceResource(resourceName corev1.ResourceName) bool {
	switch resourceName {
	case usage.ResourceServices, usage.ResourceServicesLoadBalancers, usage.ResourceServicesNodePorts:
		return true
	default:
		return false
	}
}

func cachedNamespaceResourceUsage(
	namespaceStatuses []quotav1alpha1.ResourceQuotaStatusByNamespace,
	namespace string,
	resourceName corev1.ResourceName,
) (resource.Quantity, bool) {
	for i := range namespaceStatuses {
		if namespaceStatuses[i].Namespace != namespace {
			continue
		}
		usageQty, found := namespaceStatuses[i].Status.Used[resourceName]
		if !found {
			return resource.Quantity{}, false
		}
		return usageQty, true
	}

	return resource.Quantity{}, false
}

const (
	resourceCategoryCompute     = "compute"
	resourceCategoryService     = "service"
	resourceCategoryStorage     = "storage"
	resourceCategoryObjectCount = "object_count"
)

func resourceCategoryForResource(r *ClusterResourceQuotaReconciler, resourceName corev1.ResourceName) string {
	if isServiceResource(resourceName) {
		return resourceCategoryService
	}
	if isStorageResource(resourceName) {
		return resourceCategoryStorage
	}
	if r.isComputeResource(resourceName) || resourceName == corev1.ResourcePods {
		return resourceCategoryCompute
	}
	return resourceCategoryObjectCount
}

func namespaceDirtyForCategory(dirtyState namespaceUsageDirtyState, category string) bool {
	switch category {
	case resourceCategoryCompute:
		return dirtyState.Compute
	case resourceCategoryService:
		return dirtyState.Services
	case resourceCategoryStorage:
		return dirtyState.Storage
	case resourceCategoryObjectCount:
		return dirtyState.ObjectCount
	default:
		return false
	}
}

func namespaceDirtyForResource(
	r *ClusterResourceQuotaReconciler,
	dirtyState namespaceUsageDirtyState,
	resourceName corev1.ResourceName,
) bool {
	return namespaceDirtyForCategory(dirtyState, resourceCategoryForResource(r, resourceName))
}

type namespacePrefetchPlan struct {
	pods     map[string]bool
	services map[string]bool
	pvcs     map[string]bool
}

func buildNamespacePrefetchPlan(
	r *ClusterResourceQuotaReconciler,
	crq *quotav1alpha1.ClusterResourceQuota,
	namespaces []string,
	namespaceDirtyStates map[string]namespaceUsageDirtyState,
) namespacePrefetchPlan {
	plan := namespacePrefetchPlan{
		pods:     make(map[string]bool, len(namespaces)),
		services: make(map[string]bool, len(namespaces)),
		pvcs:     make(map[string]bool, len(namespaces)),
	}

	hasCompute := hasComputeQuotaResources(r, crq.Spec.Hard)
	hasService := hasServiceQuotaResources(crq.Spec.Hard)
	hasStorage := hasStorageQuotaResources(crq.Spec.Hard)

	for i := range namespaces {
		nsName := namespaces[i]
		if nsName == "" {
			continue
		}

		if hasCompute {
			plan.pods[nsName] = namespaceDirtyStates[nsName].Compute || namespaceMissingCachedComputeUsage(r, crq, nsName)
		}
		if hasService {
			plan.services[nsName] = namespaceDirtyStates[nsName].Services || namespaceMissingCachedServiceUsage(crq, nsName)
		}
		if hasStorage {
			plan.pvcs[nsName] = namespaceDirtyStates[nsName].Storage || namespaceMissingCachedStorageUsage(crq, nsName)
		}
	}

	return plan
}

func hasComputeQuotaResources(r *ClusterResourceQuotaReconciler, hard quotav1alpha1.ResourceList) bool {
	for resourceName := range hard {
		if resourceName == corev1.ResourcePods || r.isComputeResource(resourceName) {
			return true
		}
	}
	return false
}

func hasStorageQuotaResources(hard quotav1alpha1.ResourceList) bool {
	for resourceName := range hard {
		if isStorageResource(resourceName) {
			return true
		}
	}
	return false
}

func isStorageResource(resourceName corev1.ResourceName) bool {
	resourceStr := string(resourceName)
	if resourceName == corev1.ResourceRequestsStorage || resourceName == usage.ResourcePersistentVolumeClaims {
		return true
	}
	return strings.HasSuffix(resourceStr, ".storageclass.storage.k8s.io/requests.storage") ||
		strings.HasSuffix(resourceStr, ".storageclass.storage.k8s.io/persistentvolumeclaims")
}

func namespaceMissingCachedComputeUsage(
	r *ClusterResourceQuotaReconciler,
	crq *quotav1alpha1.ClusterResourceQuota,
	namespace string,
) bool {
	for resourceName := range crq.Spec.Hard {
		if resourceName != corev1.ResourcePods && !r.isComputeResource(resourceName) {
			continue
		}
		if _, found := cachedNamespaceResourceUsage(crq.Status.Namespaces, namespace, resourceName); !found {
			return true
		}
	}

	return false
}

func namespaceMissingCachedStorageUsage(crq *quotav1alpha1.ClusterResourceQuota, namespace string) bool {
	for resourceName := range crq.Spec.Hard {
		if !isStorageResource(resourceName) {
			continue
		}
		if _, found := cachedNamespaceResourceUsage(crq.Status.Namespaces, namespace, resourceName); !found {
			return true
		}
	}

	return false
}

func hasServiceQuotaResources(hard quotav1alpha1.ResourceList) bool {
	for resourceName := range hard {
		if isServiceResource(resourceName) {
			return true
		}
	}
	return false
}

func namespaceMissingCachedServiceUsage(crq *quotav1alpha1.ClusterResourceQuota, namespace string) bool {
	for resourceName := range crq.Spec.Hard {
		if !isServiceResource(resourceName) {
			continue
		}
		if _, found := cachedNamespaceResourceUsage(crq.Status.Namespaces, namespace, resourceName); !found {
			return true
		}
	}

	return false
}

func (r *ClusterResourceQuotaReconciler) resolveNamespaceResourceUsage(
	ctx context.Context,
	nsName string,
	resourceName corev1.ResourceName,
	resourceSnapshots map[string]namespaceResourceSnapshot,
) (resource.Quantity, error) {
	snapshot, hasSnapshot := resourceSnapshots[nsName]

	switch resourceName {
	case corev1.ResourceRequestsCPU,
		corev1.ResourceRequestsMemory,
		corev1.ResourceLimitsCPU,
		corev1.ResourceLimitsMemory,
		corev1.ResourcePods:
		if hasSnapshot {
			return calculateComputeUsageFromPods(snapshot.Pods, resourceName), nil
		}
		return r.calculateComputeResources(ctx, nsName, resourceName)
	case corev1.ResourceRequestsStorage:
		if hasSnapshot {
			return calculateStorageUsageFromPVCs(snapshot.PVCs, resourceName), nil
		}
		return r.calculateStorageResources(ctx, nsName, resourceName)
	case usage.ResourcePersistentVolumeClaims:
		if hasSnapshot {
			return calculatePVCCountUsageFromPVCs(snapshot.PVCs), nil
		}
		return r.calculateStorageResources(ctx, nsName, resourceName)
	case usage.ResourceServices, usage.ResourceServicesLoadBalancers, usage.ResourceServicesNodePorts:
		if hasSnapshot {
			return calculateServiceUsageFromServices(snapshot.Services, resourceName), nil
		}
		return r.calculateServiceResources(ctx, nsName, resourceName)
	default:
		// Handle extended resources (hugepages, GPUs, etc.) via compute calculator.
		if r.isComputeResource(resourceName) {
			if hasSnapshot {
				return calculateComputeUsageFromPods(snapshot.Pods, resourceName), nil
			}
			return r.calculateComputeResources(ctx, nsName, resourceName)
		}
		return r.calculateObjectCount(ctx, nsName, resourceName)
	}
}

func shouldPrefetchNamespaceResources(hard quotav1alpha1.ResourceList) bool {
	for resourceName := range hard {
		resourceStr := string(resourceName)
		switch resourceName {
		case corev1.ResourceRequestsCPU,
			corev1.ResourceRequestsMemory,
			corev1.ResourceLimitsCPU,
			corev1.ResourceLimitsMemory,
			corev1.ResourcePods,
			corev1.ResourceRequestsStorage,
			usage.ResourcePersistentVolumeClaims,
			usage.ResourceServices,
			usage.ResourceServicesLoadBalancers,
			usage.ResourceServicesNodePorts:
			return true
		}

		if strings.HasSuffix(resourceStr, ".storageclass.storage.k8s.io/requests.storage") ||
			strings.HasSuffix(resourceStr, ".storageclass.storage.k8s.io/persistentvolumeclaims") {
			return true
		}
	}

	return false
}

func calculateComputeUsageFromPods(pods []corev1.Pod, resourceName corev1.ResourceName) resource.Quantity {
	return pod.CalculateUsageFromPods(pods, resourceName)
}

func calculateServiceUsageFromServices(svcs []corev1.Service, resourceName corev1.ResourceName) resource.Quantity {
	return services.CalculateUsageFromServices(svcs, resourceName)
}

func calculateStorageUsageFromPVCs(pvcs []corev1.PersistentVolumeClaim, resourceName corev1.ResourceName) resource.Quantity {
	return storage.CalculateStorageUsageFromPVCs(pvcs, resourceName)
}

func calculatePVCCountUsageFromPVCs(pvcs []corev1.PersistentVolumeClaim) resource.Quantity {
	return storage.CalculatePVCCountUsageFromPVCs(pvcs)
}

func calculateStorageClassUsageFromPVCs(pvcs []corev1.PersistentVolumeClaim, storageClass string) resource.Quantity {
	return storage.CalculateStorageClassUsageFromPVCs(pvcs, storageClass)
}

func calculateStorageClassCountFromPVCs(pvcs []corev1.PersistentVolumeClaim, storageClass string) int64 {
	return storage.CalculateStorageClassCountFromPVCs(pvcs, storageClass)
}

func pvcMatchesStorageClass(pvc *corev1.PersistentVolumeClaim, storageClass string) bool {
	return storage.PVCMatchesStorageClass(pvc, storageClass)
}

type namespaceResourceSnapshot struct {
	Pods     []corev1.Pod
	Services []corev1.Service
	PVCs     []corev1.PersistentVolumeClaim
}

func (r *ClusterResourceQuotaReconciler) prefetchNamespaceResources(
	ctx context.Context,
	namespaces []string,
) (map[string]namespaceResourceSnapshot, error) {
	return r.prefetchNamespaceResourcesWithPlan(ctx, namespaces, namespacePrefetchPlan{})
}

func (r *ClusterResourceQuotaReconciler) prefetchNamespaceResourcesWithPlan(
	ctx context.Context,
	namespaces []string,
	plan namespacePrefetchPlan,
) (map[string]namespaceResourceSnapshot, error) {
	if r.KubeClient == nil {
		return nil, fmt.Errorf("kube client is nil")
	}

	snapshots := make(map[string]namespaceResourceSnapshot, len(namespaces))
	for _, nsName := range namespaces {
		if nsName == "" {
			continue
		}

		var err error

		pods := &corev1.PodList{}
		if shouldFetchForNamespace(plan.pods, nsName) {
			pods, err = r.KubeClient.CoreV1().Pods(nsName).List(ctx, metav1.ListOptions{})
			if err != nil {
				return nil, fmt.Errorf("failed to prefetch pods in namespace %s: %w", nsName, err)
			}
		}

		svcs := &corev1.ServiceList{}
		if shouldFetchForNamespace(plan.services, nsName) {
			svcs, err = r.KubeClient.CoreV1().Services(nsName).List(ctx, metav1.ListOptions{})
			if err != nil {
				return nil, fmt.Errorf("failed to prefetch services in namespace %s: %w", nsName, err)
			}
		}

		pvcs := &corev1.PersistentVolumeClaimList{}
		if shouldFetchForNamespace(plan.pvcs, nsName) {
			pvcs, err = r.KubeClient.CoreV1().PersistentVolumeClaims(nsName).List(ctx, metav1.ListOptions{})
			if err != nil {
				return nil, fmt.Errorf("failed to prefetch pvcs in namespace %s: %w", nsName, err)
			}
		}

		snapshots[nsName] = namespaceResourceSnapshot{
			Pods:     pods.Items,
			Services: svcs.Items,
			PVCs:     pvcs.Items,
		}
	}

	return snapshots, nil
}

func shouldFetchForNamespace(fetchMap map[string]bool, namespace string) bool {
	if len(fetchMap) == 0 {
		return true
	}
	return fetchMap[namespace]
}

func (r *ClusterResourceQuotaReconciler) aggregationStepForResource(resourceName corev1.ResourceName) string {
	switch resourceName {
	case corev1.ResourceRequestsCPU,
		corev1.ResourceRequestsMemory,
		corev1.ResourceLimitsCPU,
		corev1.ResourceLimitsMemory,
		corev1.ResourcePods:
		return "compute"
	case corev1.ResourceRequestsStorage:
		return "storage"
	case usage.ResourceServices, usage.ResourceServicesLoadBalancers, usage.ResourceServicesNodePorts:
		return "services"
	default:
		if r.isComputeResource(resourceName) {
			return "compute_extended"
		}
		return "object_count"
	}
}
