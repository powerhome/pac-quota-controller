package controller

import (
	"context"
	"fmt"
	"slices"
	"sort"
	"strings"
	"time"

	quotav1alpha1 "github.com/powerhome/pac-quota-controller/api/v1alpha1"
	"github.com/powerhome/pac-quota-controller/pkg/config"
	"github.com/powerhome/pac-quota-controller/pkg/events"
	"github.com/powerhome/pac-quota-controller/pkg/kubernetes/objectcount"
	"github.com/powerhome/pac-quota-controller/pkg/kubernetes/pod"
	"github.com/powerhome/pac-quota-controller/pkg/kubernetes/quota"
	"github.com/powerhome/pac-quota-controller/pkg/kubernetes/services"
	"github.com/powerhome/pac-quota-controller/pkg/kubernetes/storage"
	"github.com/powerhome/pac-quota-controller/pkg/kubernetes/usage"
	"github.com/powerhome/pac-quota-controller/pkg/metrics"
	"github.com/prometheus/client_golang/prometheus"
	"go.uber.org/zap"
	appsv1 "k8s.io/api/apps/v1"
	autoscalingv1 "k8s.io/api/autoscaling/v1"
	batchv1 "k8s.io/api/batch/v1"
	corev1 "k8s.io/api/core/v1"
	networkingv1 "k8s.io/api/networking/v1"
	apiequality "k8s.io/apimachinery/pkg/api/equality"
	"k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/api/resource"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/labels"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/types"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/builder"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/event"
	"sigs.k8s.io/controller-runtime/pkg/handler"
	"sigs.k8s.io/controller-runtime/pkg/predicate"
	"sigs.k8s.io/controller-runtime/pkg/reconcile"
)

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

	// Special handling for Pods: reconcile if the pod transitions to or from a terminal state
	// or if there's a significant status change (like an init container finishing).
	if podOld, ok := e.ObjectOld.(*corev1.Pod); ok {
		if podNew, ok := e.ObjectNew.(*corev1.Pod); ok {
			// Trigger on terminal state transition
			if pod.IsPodTerminal(podOld) != pod.IsPodTerminal(podNew) {
				return true
			}
			// Trigger if any container (init or app) has terminated since last update
			if containerTerminated(podOld.Status.InitContainerStatuses, podNew.Status.InitContainerStatuses) ||
				containerTerminated(podOld.Status.ContainerStatuses, podNew.Status.ContainerStatuses) {
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

// containerTerminated returns true if any container in the new statuses has transitioned to Terminated
// while it was not terminated in the old statuses, or if the set of containers has changed.
func containerTerminated(oldStatuses, newStatuses []corev1.ContainerStatus) bool {
	if len(oldStatuses) != len(newStatuses) {
		return true
	}

	oldTerminated := make(map[string]bool)
	oldExists := make(map[string]bool)
	for _, s := range oldStatuses {
		oldExists[s.Name] = true
		if s.State.Terminated != nil {
			oldTerminated[s.Name] = true
		}
	}

	for _, s := range newStatuses {
		if !oldExists[s.Name] {
			return true
		}
		if s.State.Terminated != nil && !oldTerminated[s.Name] {
			return true
		}
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
	ServiceCalculator        *services.ServiceResourceCalculator
	ObjectCountCalculator    *objectcount.ObjectCountCalculator
	EventRecorder            *events.EventRecorder
	Config                   *config.Config
	logger                   *zap.Logger
	ExcludeNamespaceLabelKey string
	ExcludedNamespaces       []string
	// previousNamespacesByQuota tracks namespaces from previous reconciliation for change detection
	previousNamespacesByQuota map[string][]string
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
	r.logger.Info("Reconciling ClusterResourceQuota", zap.String("crq", req.Name))
	metrics.QuotaReconcileTotal.WithLabelValues(req.Name, "started").Inc()
	startTime := time.Now()
	defer func() {
		duration := time.Since(startTime)
		r.logger.Info("Finished reconciliation",
			zap.String("crq", req.Name),
			zap.Duration("duration", duration),
		)
	}()

	// Fetch the ClusterResourceQuota instance
	crq := &quotav1alpha1.ClusterResourceQuota{}
	if err := r.Get(ctx, req.NamespacedName, crq); err != nil {
		if errors.IsNotFound(err) {
			// Object not found, likely deleted, return without error
			r.logger.Info("ClusterResourceQuota resource not found. Ignoring since object must have been deleted")
			return ctrl.Result{}, nil
		}
		// Error reading the object - requeue the request
		r.logger.Error("Failed to get ClusterResourceQuota", zap.Error(err), zap.String("crq", req.Name))
		metrics.QuotaReconcileErrors.WithLabelValues(req.Name).Inc()
		metrics.QuotaReconcileTotal.WithLabelValues(req.Name, "failed").Inc()
		return ctrl.Result{}, err
	}

	// Get the list of selected namespaces, filtering out excluded ones.
	var selectedNamespaces []string
	if crq.Spec.NamespaceSelector != nil {
		selector, err := metav1.LabelSelectorAsSelector(crq.Spec.NamespaceSelector)
		if err != nil {
			r.logger.Error("Failed to create selector from CRQ spec", zap.Error(err), zap.String("crq", crq.Name))
			r.EventRecorder.InvalidSelector(crq, err)
			metrics.QuotaReconcileErrors.WithLabelValues(crq.Name).Inc()
			metrics.QuotaReconcileTotal.WithLabelValues(crq.Name, "invalid_selector").Inc()
			return ctrl.Result{}, fmt.Errorf("failed to create selector from CRQ spec: %w", err)
		}

		namespaceList := &corev1.NamespaceList{}
		listOpts := &client.ListOptions{
			LabelSelector: selector,
		}

		if err := r.List(ctx, namespaceList, listOpts); err != nil {
			r.logger.Error("Failed to list namespaces", zap.Error(err), zap.String("crq", crq.Name))
			r.EventRecorder.CalculationFailed(crq, err)
			metrics.QuotaReconcileErrors.WithLabelValues(crq.Name).Inc()
			metrics.QuotaReconcileTotal.WithLabelValues(crq.Name, "failed").Inc()
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

	// Check for namespace changes and emit events
	r.handleNamespaceChanges(crq, selectedNamespaces)

	r.logger.Debug("Found namespaces matching selection criteria",
		zap.Int("count", len(selectedNamespaces)),
		zap.Strings("namespaces", selectedNamespaces),
	)

	// Calculate aggregated resource usage across all selected namespaces
	totalUsage, usageByNamespace, err := r.calculateAndAggregateUsage(ctx, crq, selectedNamespaces)
	if err != nil {
		r.logger.Error("Failed to calculate resource usage", zap.Error(err), zap.String("crq", crq.Name))
		metrics.QuotaReconcileErrors.WithLabelValues(crq.Name).Inc()
		metrics.QuotaReconcileTotal.WithLabelValues(crq.Name, "failed").Inc()
		return ctrl.Result{}, err
	}

	// Check for quota warnings and violations
	r.checkQuotaThresholds(crq, totalUsage)

	// Expose custom metrics: per-namespace and total usage as percent (0-1 float)
	for _, nsUsage := range usageByNamespace {
		ns := nsUsage.Namespace
		for resourceName, used := range nsUsage.Status.Used {
			hard, hasHard := crq.Spec.Hard[resourceName]
			var percent float64
			if hasHard && hard.Value() > 0 {
				percent = used.AsApproximateFloat64() / hard.AsApproximateFloat64()
			} else {
				percent = 0.0
			}
			metrics.CRQUsage.WithLabelValues(crq.Name, ns, string(resourceName)).Set(percent)
		}
	}
	// Pick the first namespace (alphabetically) for routing and join all for context
	var routingNamespace string
	if len(selectedNamespaces) > 0 {
		routingNamespace = selectedNamespaces[0]
	}
	allNamespaces := strings.Join(selectedNamespaces, ",")
	for resourceName, total := range totalUsage {
		hard, hasHard := crq.Spec.Hard[resourceName]
		var percent float64
		if hasHard && hard.Value() > 0 {
			percent = total.AsApproximateFloat64() / hard.AsApproximateFloat64()
		} else {
			percent = 0.0
		}
		metrics.CRQTotalUsage.WithLabelValues(
			crq.Name, string(resourceName), routingNamespace, allNamespaces,
		).Set(percent)
	}

	// Update the status of the ClusterResourceQuota
	if err := r.updateStatus(ctx, crq, totalUsage, usageByNamespace); err != nil {
		if errors.IsNotFound(err) {
			r.logger.Info("CRQ not found during status update, likely deleted. Skipping status update.", zap.String("name", crq.Name))
			return ctrl.Result{}, nil
		}
		r.logger.Error("Failed to update ClusterResourceQuota status", zap.Error(err), zap.String("crq", crq.Name))
		metrics.QuotaReconcileErrors.WithLabelValues(crq.Name).Inc()
		metrics.QuotaReconcileTotal.WithLabelValues(crq.Name, "status_update_failed").Inc()
		return ctrl.Result{}, err
	}

	metrics.QuotaReconcileTotal.WithLabelValues(crq.Name, "success").Inc()
	return ctrl.Result{}, nil
}

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

	resourceSnapshots := map[string]namespaceResourceSnapshot{}
	if shouldPrefetchNamespaceResources(crq.Spec.Hard) {
		var err error
		resourceSnapshots, err = r.prefetchNamespaceResources(ctx, namespaces)
		if err != nil {
			r.logger.Warn(
				"Failed to prefetch namespace resources, falling back to calculators",
				zap.Error(err),
				zap.String("crq", crq.Name),
			)
			resourceSnapshots = nil
		}
	}

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
				stepStart := time.Now()
				if snapshot, ok := resourceSnapshots[nsName]; ok {
					currentUsage = storage.CalculateStorageClassUsageFromPVCs(snapshot.PVCs, storageClass)
				} else if r.StorageCalculator != nil {
					storageUsage, err := r.StorageCalculator.CalculateStorageClassUsage(ctx, nsName, storageClass)
					if err != nil {
						metrics.QuotaAggregationStepDuration.WithLabelValues(crq.Name, "storage_class_usage").Observe(time.Since(stepStart).Seconds())
						return nil, nil, fmt.Errorf("failed to calculate storage class usage for %s in %s: %w",
							storageClass, nsName, err)
					}
					currentUsage = storageUsage
				} else {
					r.logger.Error("StorageCalculator is nil",
						zap.String("namespace", nsName), zap.Stringer("resource", resourceName))
					currentUsage = resource.MustParse("0")
				}
				metrics.QuotaAggregationStepDuration.WithLabelValues(crq.Name, "storage_class_usage").Observe(time.Since(stepStart).Seconds())
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
				stepStart := time.Now()
				if snapshot, ok := resourceSnapshots[nsName]; ok {
					currentCount = storage.CalculateStorageClassCountFromPVCs(snapshot.PVCs, storageClass)
				} else if r.StorageCalculator != nil {
					count, err := r.StorageCalculator.CalculateStorageClassCount(ctx, nsName, storageClass)
					if err != nil {
						metrics.QuotaAggregationStepDuration.WithLabelValues(crq.Name, "storage_class_count").Observe(time.Since(stepStart).Seconds())
						return nil, nil, fmt.Errorf("failed to calculate storage class PVC count for %s in %s: %w",
							storageClass, nsName, err)
					}
					currentCount = count
				} else {
					r.logger.Error("StorageCalculator is nil",
						zap.String("namespace", nsName), zap.Stringer("resource", resourceName))
					currentCount = 0
				}
				metrics.QuotaAggregationStepDuration.WithLabelValues(crq.Name, "storage_class_count").Observe(time.Since(stepStart).Seconds())
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
			// If nsName is empty, skip usage calculation for this entry
			if nsName == "" {
				r.logger.Info("Skipping usage calculation for empty namespace name")
				continue
			}
			stepName := r.aggregationStepForResource(resourceName)
			stepStart := time.Now()

			currentUsage, calcErr := r.resolveNamespaceResourceUsage(ctx, nsName, resourceName, resourceSnapshots)

			if calcErr != nil {
				metrics.QuotaAggregationStepDuration.WithLabelValues(crq.Name, stepName).Observe(time.Since(stepStart).Seconds())
				return nil, nil, calcErr
			}

			metrics.QuotaAggregationStepDuration.WithLabelValues(crq.Name, stepName).Observe(time.Since(stepStart).Seconds())

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

	r.logger.Debug("Usage calculation finished.")
	return totalUsage, usageByNamespace, nil
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
			return pod.CalculateUsageFromPods(snapshot.Pods, resourceName), nil
		}
		return r.calculateComputeResources(ctx, nsName, resourceName)
	case corev1.ResourceRequestsStorage:
		if hasSnapshot {
			return storage.CalculateStorageUsageFromPVCs(snapshot.PVCs, resourceName), nil
		}
		return r.calculateStorageResources(ctx, nsName, resourceName)
	case usage.ResourcePersistentVolumeClaims:
		if hasSnapshot {
			return storage.CalculatePVCCountUsageFromPVCs(snapshot.PVCs), nil
		}
		return r.calculateStorageResources(ctx, nsName, resourceName)
	case usage.ResourceServices, usage.ResourceServicesLoadBalancers, usage.ResourceServicesNodePorts:
		if hasSnapshot {
			return services.CalculateUsageFromServices(snapshot.Services, resourceName), nil
		}
		return r.calculateServiceResources(ctx, nsName, resourceName)
	default:
		// Handle extended resources (hugepages, GPUs, etc.) via compute calculator.
		if r.isComputeResource(resourceName) {
			if hasSnapshot {
				return pod.CalculateUsageFromPods(snapshot.Pods, resourceName), nil
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

type namespaceResourceSnapshot struct {
	Pods     []corev1.Pod
	Services []corev1.Service
	PVCs     []corev1.PersistentVolumeClaim
}

func (r *ClusterResourceQuotaReconciler) prefetchNamespaceResources(
	ctx context.Context,
	namespaces []string,
) (map[string]namespaceResourceSnapshot, error) {
	snapshots := make(map[string]namespaceResourceSnapshot, len(namespaces))
	for _, nsName := range namespaces {
		if nsName == "" {
			continue
		}

		pods := &corev1.PodList{}
		if err := r.List(ctx, pods, client.InNamespace(nsName)); err != nil {
			return nil, fmt.Errorf("failed to prefetch pods in namespace %s: %w", nsName, err)
		}

		svcs := &corev1.ServiceList{}
		if err := r.List(ctx, svcs, client.InNamespace(nsName)); err != nil {
			return nil, fmt.Errorf("failed to prefetch services in namespace %s: %w", nsName, err)
		}

		pvcs := &corev1.PersistentVolumeClaimList{}
		if err := r.List(ctx, pvcs, client.InNamespace(nsName)); err != nil {
			return nil, fmt.Errorf("failed to prefetch pvcs in namespace %s: %w", nsName, err)
		}

		snapshots[nsName] = namespaceResourceSnapshot{
			Pods:     pods.Items,
			Services: svcs.Items,
			PVCs:     pvcs.Items,
		}
	}

	return snapshots, nil
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

// calculateObjectCount calculates the usage for object count quotas.
func (r *ClusterResourceQuotaReconciler) calculateObjectCount(
	ctx context.Context, ns string, resourceName corev1.ResourceName,
) (resource.Quantity, error) {
	// Use the correct calculator for each resource type
	switch resourceName {
	case usage.ResourceConfigMaps, usage.ResourceSecrets, usage.ResourceReplicationControllers,
		usage.ResourceDeployments, usage.ResourceStatefulSets, usage.ResourceDaemonSets,
		usage.ResourceJobs, usage.ResourceCronJobs, usage.ResourceHorizontalPodAutoscalers, usage.ResourceIngresses:
		objectCount, err := r.ObjectCountCalculator.CalculateUsage(ctx, ns, resourceName)
		if err != nil {
			r.logger.Error("Failed to calculate object count usage",
				zap.Error(err), zap.Stringer("resource", resourceName), zap.String("namespace", ns))
			return resource.Quantity{}, err
		}
		return objectCount, nil
	default:
		// CRQ tracks a resource we have no calculator for (typo or unsupported kind).
		// Return zero to keep the rest of the reconcile working, but emit a Warn +
		// metric so operators can detect the silent admit.
		metrics.QuotaUnsupportedResource.WithLabelValues(string(resourceName)).Inc()
		r.logger.Warn("Unsupported resource in CRQ; reporting zero usage",
			zap.Stringer("resource", resourceName),
			zap.String("namespace", ns),
		)
		return resource.MustParse("0"), nil
	}
}

// calculateComputeResources calculates the usage for compute resource quotas (CPU/Memory).
func (r *ClusterResourceQuotaReconciler) calculateComputeResources(
	ctx context.Context, ns string, resourceName corev1.ResourceName,
) (resource.Quantity, error) {
	computeUsage, err := r.ComputeCalculator.CalculateUsage(ctx, ns, resourceName)
	if err != nil {
		r.logger.Error("Failed to calculate compute resources",
			zap.Error(err), zap.Stringer("resource", resourceName), zap.String("namespace", ns))
		return resource.Quantity{}, err
	}
	return computeUsage, nil
}

// calculateStorageResources calculates the usage for storage resource quotas.
func (r *ClusterResourceQuotaReconciler) calculateStorageResources(
	ctx context.Context, ns string, resourceName corev1.ResourceName,
) (resource.Quantity, error) {
	if r.StorageCalculator == nil {
		r.logger.Error("StorageCalculator is nil",
			zap.String("namespace", ns), zap.Stringer("resource", resourceName))
		return resource.MustParse("0"), nil
	}

	storageUsage, err := r.StorageCalculator.CalculateUsage(ctx, ns, resourceName)
	if err != nil {
		r.logger.Error("Failed to calculate storage resources",
			zap.Error(err), zap.Stringer("resource", resourceName), zap.String("namespace", ns))
		return resource.Quantity{}, err
	}
	return storageUsage, nil
}

// calculateServiceResources calculates the usage for service resource quotas.
func (r *ClusterResourceQuotaReconciler) calculateServiceResources(
	ctx context.Context, ns string, resourceName corev1.ResourceName,
) (resource.Quantity, error) {
	if r.ServiceCalculator == nil {
		r.logger.Error("ServiceCalculator is nil",
			zap.String("namespace", ns), zap.Stringer("resource", resourceName))
		return resource.MustParse("0"), nil
	}

	serviceUsage, err := r.ServiceCalculator.CalculateUsage(ctx, ns, resourceName)
	if err != nil {
		r.logger.Error("Failed to calculate service resources",
			zap.Error(err), zap.Stringer("resource", resourceName), zap.String("namespace", ns))
		return resource.Quantity{}, err
	}
	return serviceUsage, nil
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

	if apiequality.Semantic.DeepEqual(crq.Status, crqCopy.Status) {
		return nil
	}

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
			r.logger.Error("Failed to get namespace for object to check for exclusion", zap.Error(err), zap.String("object", client.ObjectKeyFromObject(obj).String()))
			return nil
		}
	}

	if r.isNamespaceExcluded(ns) {
		return nil // Ignore events from excluded namespaces
	}

	// Find which CRQ selects this namespace.
	crq, err := r.crqClient.GetCRQByNamespace(ctx, ns)
	if err != nil {
		r.logger.Error("Failed to get ClusterResourceQuota for namespace", zap.Error(err))
		return nil
	}
	if crq != nil {
		r.logger.Debug("Found ClusterResourceQuota for namespace", zap.String("crq", crq.Name), zap.String("namespace", ns.Name))
	} else {
		r.logger.Debug("No ClusterResourceQuota found for namespace", zap.String("namespace", ns.Name))
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

	// Extended resources start with request.
	if strings.HasPrefix(resourceStr, "requests.") {
		return true
	}

	// If we can't categorize it, assume it's not a compute resource
	return false
}

// runViolationCleanupLoop periodically clears expired entries from the violation cache.
// Exits when ctx is cancelled (manager shutdown).
func (r *ClusterResourceQuotaReconciler) runViolationCleanupLoop(ctx context.Context, interval time.Duration) {
	ticker := time.NewTicker(interval)
	defer ticker.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			r.EventRecorder.CleanupExpiredViolations()
		}
	}
}

// SetupWithManager sets up the controller with the Manager.
func (r *ClusterResourceQuotaReconciler) SetupWithManager(ctx context.Context, cfg *config.Config, mgr ctrl.Manager) error {
	// Initialize logger
	if r.logger == nil {
		r.logger = zap.L().Named("clusterresourcequota-controller")
	}

	if r.crqClient == nil {
		r.crqClient = quota.NewCRQClient(r.Client, r.logger)
	}

	if r.StorageCalculator == nil {
		r.StorageCalculator = storage.NewStorageResourceCalculator(r.Client, r.logger)
	}
	if r.ComputeCalculator == nil {
		r.ComputeCalculator = pod.NewPodResourceCalculator(r.Client, r.logger)
	}
	if r.ServiceCalculator == nil {
		r.ServiceCalculator = services.NewServiceResourceCalculator(r.Client, r.logger)
	}
	if r.ObjectCountCalculator == nil {
		r.ObjectCountCalculator = objectcount.NewObjectCountCalculator(r.Client, r.logger)
	}

	// Initialize EventRecorder
	if r.EventRecorder == nil {
		r.EventRecorder = events.NewEventRecorder(
			mgr.GetEventRecorder("pac-quota-controller"),
			cfg.OwnNamespace,
			r.logger,
		)
	}

	// Initialize previous namespaces tracking
	if r.previousNamespacesByQuota == nil {
		r.previousNamespacesByQuota = make(map[string][]string)
	}

	// Load event cleanup configuration from multiple sources
	var cleanupConfig events.CleanupConfig

	if r.Config != nil && r.Config.EventsEnable {
		var err error
		cleanupConfig, err = events.LoadEventCleanupConfig(
			r.Config.EventsConfigPath,
			r.Config.EventsTTL,
			r.Config.EventsMaxEventsPerCRQ,
			r.Config.EventsCleanupInterval,
		)
		if err != nil {
			r.logger.Warn("Failed to load event cleanup config, using defaults", zap.Error(err))
			cleanupConfig = events.DefaultCleanupConfig()
		}
	} else {
		// Events disabled or no config provided, use defaults but disable cleanup
		cleanupConfig = events.DefaultCleanupConfig()
		if r.Config != nil && !r.Config.EventsEnable {
			cleanupConfig.Enabled = false
		}
	}

	cleanupManager := events.NewEventCleanupManager(mgr.GetClient(), cleanupConfig, r.logger)

	// Start cleanup in background
	go func() {
		cleanupManager.Start(ctx)
	}()

	// Start periodic violation cache cleanup; honours ctx so it exits on manager shutdown.
	go r.runViolationCleanupLoop(ctx, 15*time.Minute)

	r.logger.Info("Setting up ClusterResourceQuota controller")

	// Predicate to filter out updates to status subresource
	// This prevents reconcile loops caused by status updates
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
		{&corev1.Service{}, nil},
		// Generic object count resources
		{&corev1.ConfigMap{}, nil},
		{&corev1.Secret{}, nil},
		{&corev1.ReplicationController{}, nil},
		{&appsv1.Deployment{}, nil},
		{&appsv1.StatefulSet{}, nil},
		{&appsv1.DaemonSet{}, nil},
		{&batchv1.Job{}, nil},
		{&batchv1.CronJob{}, nil},
		{&autoscalingv1.HorizontalPodAutoscaler{}, nil},
		{&networkingv1.Ingress{}, nil},
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
