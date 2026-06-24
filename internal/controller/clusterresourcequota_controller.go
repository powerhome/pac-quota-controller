package controller

import (
	"context"
	"fmt"
	"slices"
	"sort"
	"strings"
	"sync"
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
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/types"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/builder"
	"sigs.k8s.io/controller-runtime/pkg/client"
	ctrlcontroller "sigs.k8s.io/controller-runtime/pkg/controller"
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
	ObjectCountCalculator    *objectcount.ObjectCountCalculator
	EventRecorder            *events.EventRecorder
	Config                   *config.Config
	logger                   *zap.Logger
	ExcludeNamespaceLabelKey string
	ExcludedNamespaces       []string

	// mu guards previousNamespacesByQuota and lastQuotaExceededAt across
	// concurrent Reconcile calls (MaxConcurrentReconciles: 5).
	mu                        sync.RWMutex
	previousNamespacesByQuota map[string][]string
	lastQuotaExceededAt       map[string]time.Time
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
	r.logger.Info("Reconciling ClusterResourceQuota", zap.String("crq_name", req.Name))
	metrics.QuotaReconcileTotal.WithLabelValues(req.Name, "started").Inc()
	startTime := time.Now()
	defer func() {
		duration := time.Since(startTime)
		r.logger.Info("Finished reconciliation",
			zap.String("crq_name", req.Name),
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
		r.logger.Error("Failed to get ClusterResourceQuota", zap.Error(err), zap.String("crq_name", req.Name))
		metrics.QuotaReconcileErrors.WithLabelValues(req.Name).Inc()
		metrics.QuotaReconcileTotal.WithLabelValues(req.Name, "failed").Inc()
		return ctrl.Result{}, err
	}

	// Get the list of selected namespaces, filtering out excluded ones.
	var selectedNamespaces []string
	if crq.Spec.NamespaceSelector != nil {
		selector, err := metav1.LabelSelectorAsSelector(crq.Spec.NamespaceSelector)
		if err != nil {
			r.logger.Error("Failed to create selector from CRQ spec", zap.Error(err), zap.String("crq_name", crq.Name))
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
			r.logger.Error("Failed to list namespaces", zap.Error(err), zap.String("crq_name", crq.Name))
			r.EventRecorder.CalculationFailed(crq, err)
			metrics.QuotaReconcileErrors.WithLabelValues(crq.Name).Inc()
			metrics.QuotaReconcileTotal.WithLabelValues(crq.Name, "failed").Inc()
			return ctrl.Result{}, err
		}

		for _, ns := range namespaceList.Items {
			if r.isNamespaceExcluded(&ns) {
				continue
			}
			selectedNamespaces = append(selectedNamespaces, ns.Name)
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
		r.logger.Error("Failed to calculate resource usage", zap.Error(err), zap.String("crq_name", crq.Name))
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
			hard := crq.Spec.Hard[resourceName]
			metrics.CRQUsage.WithLabelValues(crq.Name, ns, string(resourceName)).Set(percentOfHard(used, hard))
		}
	}
	for resourceName, total := range totalUsage {
		hard := crq.Spec.Hard[resourceName]
		metrics.CRQTotalUsage.WithLabelValues(crq.Name, string(resourceName)).Set(percentOfHard(total, hard))
	}

	// Update the status of the ClusterResourceQuota
	if err := r.updateStatus(ctx, crq, totalUsage, usageByNamespace); err != nil {
		if errors.IsNotFound(err) {
			r.logger.Info("CRQ not found during status update, likely deleted. Skipping status update.", zap.String("crq_name", crq.Name))
			return ctrl.Result{}, nil
		}
		r.logger.Error("Failed to update ClusterResourceQuota status", zap.Error(err), zap.String("crq_name", crq.Name))
		metrics.QuotaReconcileErrors.WithLabelValues(crq.Name).Inc()
		metrics.QuotaReconcileTotal.WithLabelValues(crq.Name, "status_update_failed").Inc()
		return ctrl.Result{}, err
	}

	metrics.QuotaReconcileTotal.WithLabelValues(crq.Name, "success").Inc()
	return ctrl.Result{}, nil
}

// percentOfHard returns used/hard as a 0..1 float, or 0 when hard is unset.
func percentOfHard(used, hard resource.Quantity) float64 {
	if hard.Value() <= 0 {
		return 0
	}
	return used.AsApproximateFloat64() / hard.AsApproximateFloat64()
}

// calculateAndAggregateUsage walks each namespace once, lists only the resource
// kinds the CRQ tracks, and computes per-resource usage off the in-memory slices.
func (r *ClusterResourceQuotaReconciler) calculateAndAggregateUsage(
	ctx context.Context,
	crq *quotav1alpha1.ClusterResourceQuota,
	namespaces []string,
) (quotav1alpha1.ResourceList, []quotav1alpha1.ResourceQuotaStatusByNamespace, error) {
	r.logger.Debug("Calculating resource usage", zap.String("crq_name", crq.Name))
	timer := prometheus.NewTimer(metrics.QuotaAggregationDuration.WithLabelValues(crq.Name))
	defer timer.ObserveDuration()

	totalUsage := make(quotav1alpha1.ResourceList, len(crq.Spec.Hard))
	usageByNamespace := make([]quotav1alpha1.ResourceQuotaStatusByNamespace, len(namespaces))
	kinds := r.classifyKindsNeeded(crq.Spec.Hard)

	for i, nsName := range namespaces {
		usageByNamespace[i] = quotav1alpha1.ResourceQuotaStatusByNamespace{
			Namespace: nsName,
			Status:    quotav1alpha1.ResourceQuotaStatus{Used: make(quotav1alpha1.ResourceList)},
		}

		pods, svcs, pvcs, err := r.listNamespaceResources(ctx, nsName, kinds)
		if err != nil {
			return nil, nil, err
		}

		var pvcsByClass map[string][]corev1.PersistentVolumeClaim
		if kinds.storageClasses {
			pvcsByClass = bucketPVCsByStorageClass(pvcs)
		}

		for resourceName := range crq.Spec.Hard {
			stepStart := time.Now()
			used, err := r.computeNamespaceResourceUsage(
				ctx, nsName, resourceName, pods, svcs, pvcs, pvcsByClass,
			)
			metrics.QuotaAggregationStepDuration.
				WithLabelValues(crq.Name, r.aggregationStepForResource(resourceName)).
				Observe(time.Since(stepStart).Seconds())
			if err != nil {
				return nil, nil, err
			}

			usageByNamespace[i].Status.Used[resourceName] = used
			q := totalUsage[resourceName]
			q.Add(used)
			totalUsage[resourceName] = q
		}
	}

	r.logger.Debug("Usage calculation finished.")
	return totalUsage, usageByNamespace, nil
}

// namespaceKinds enumerates the kinds of namespaced resources a CRQ requires
// listing. storageClasses is true when any *.storageclass.storage.k8s.io/* key
// is present, so the controller knows to bucket PVCs by class once per namespace.
type namespaceKinds struct {
	pods           bool
	services       bool
	pvcs           bool
	storageClasses bool
}

func (r *ClusterResourceQuotaReconciler) classifyKindsNeeded(hard quotav1alpha1.ResourceList) namespaceKinds {
	var k namespaceKinds
	for resourceName := range hard {
		resourceStr := string(resourceName)
		switch resourceName {
		case corev1.ResourceRequestsCPU,
			corev1.ResourceRequestsMemory,
			corev1.ResourceLimitsCPU,
			corev1.ResourceLimitsMemory,
			corev1.ResourcePods:
			k.pods = true
		case usage.ResourceServices,
			usage.ResourceServicesLoadBalancers,
			usage.ResourceServicesNodePorts:
			k.services = true
		case corev1.ResourceRequestsStorage, usage.ResourcePersistentVolumeClaims:
			k.pvcs = true
		default:
			if r.isComputeResource(resourceName) {
				k.pods = true
			} else if strings.HasSuffix(resourceStr, ".storageclass.storage.k8s.io/requests.storage") ||
				strings.HasSuffix(resourceStr, ".storageclass.storage.k8s.io/persistentvolumeclaims") {
				k.pvcs = true
				k.storageClasses = true
			}
		}
	}
	return k
}

func (r *ClusterResourceQuotaReconciler) listNamespaceResources(
	ctx context.Context,
	nsName string,
	kinds namespaceKinds,
) ([]corev1.Pod, []corev1.Service, []corev1.PersistentVolumeClaim, error) {
	var pods []corev1.Pod
	var svcs []corev1.Service
	var pvcs []corev1.PersistentVolumeClaim

	if kinds.pods {
		list := &corev1.PodList{}
		if err := r.List(ctx, list, client.InNamespace(nsName)); err != nil {
			return nil, nil, nil, fmt.Errorf("failed to list pods in namespace %s: %w", nsName, err)
		}
		pods = list.Items
	}
	if kinds.services {
		list := &corev1.ServiceList{}
		if err := r.List(ctx, list, client.InNamespace(nsName)); err != nil {
			return nil, nil, nil, fmt.Errorf("failed to list services in namespace %s: %w", nsName, err)
		}
		svcs = list.Items
	}
	if kinds.pvcs {
		list := &corev1.PersistentVolumeClaimList{}
		if err := r.List(ctx, list, client.InNamespace(nsName)); err != nil {
			return nil, nil, nil, fmt.Errorf("failed to list pvcs in namespace %s: %w", nsName, err)
		}
		pvcs = list.Items
	}
	return pods, svcs, pvcs, nil
}

// bucketPVCsByStorageClass groups PVCs once per namespace so each storage-class
// resource lookup is O(1) instead of a full PVC scan.
func bucketPVCsByStorageClass(pvcs []corev1.PersistentVolumeClaim) map[string][]corev1.PersistentVolumeClaim {
	if len(pvcs) == 0 {
		return nil
	}
	buckets := make(map[string][]corev1.PersistentVolumeClaim, 4)
	for i := range pvcs {
		class := storage.PVCStorageClass(&pvcs[i])
		if class == "" {
			continue
		}
		buckets[class] = append(buckets[class], pvcs[i])
	}
	return buckets
}

func (r *ClusterResourceQuotaReconciler) computeNamespaceResourceUsage(
	ctx context.Context,
	nsName string,
	resourceName corev1.ResourceName,
	pods []corev1.Pod,
	svcs []corev1.Service,
	pvcs []corev1.PersistentVolumeClaim,
	pvcsByClass map[string][]corev1.PersistentVolumeClaim,
) (resource.Quantity, error) {
	switch resourceName {
	case corev1.ResourceRequestsCPU,
		corev1.ResourceRequestsMemory,
		corev1.ResourceLimitsCPU,
		corev1.ResourceLimitsMemory,
		corev1.ResourcePods:
		return pod.CalculateUsageFromPods(pods, resourceName), nil
	case corev1.ResourceRequestsStorage:
		return storage.CalculateStorageUsageFromPVCs(pvcs, resourceName), nil
	case usage.ResourcePersistentVolumeClaims:
		return storage.CalculatePVCCountUsageFromPVCs(pvcs), nil
	case usage.ResourceServices,
		usage.ResourceServicesLoadBalancers,
		usage.ResourceServicesNodePorts:
		return services.CalculateUsageFromServices(svcs, resourceName), nil
	}

	resourceStr := string(resourceName)
	if class, ok := strings.CutSuffix(resourceStr, ".storageclass.storage.k8s.io/requests.storage"); ok {
		return storage.CalculateStorageUsageFromPVCs(pvcsByClass[class], corev1.ResourceRequestsStorage), nil
	}
	if class, ok := strings.CutSuffix(resourceStr, ".storageclass.storage.k8s.io/persistentvolumeclaims"); ok {
		return *resource.NewQuantity(int64(len(pvcsByClass[class])), resource.DecimalSI), nil
	}

	if r.isComputeResource(resourceName) {
		return pod.CalculateUsageFromPods(pods, resourceName), nil
	}
	return r.calculateObjectCount(ctx, nsName, resourceName)
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
		r.logger.Debug("Found ClusterResourceQuota for namespace", zap.String("crq_name", crq.Name), zap.String("namespace", ns.Name))
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

// SetupWithManager sets up the controller with the Manager. It is a thin
// orchestrator over three helpers so each concern (DI, background workers,
// watch wiring) reads independently.
func (r *ClusterResourceQuotaReconciler) SetupWithManager(ctx context.Context, cfg *config.Config, mgr ctrl.Manager) error {
	r.ensureDependencies(mgr)
	r.startBackgroundWorkers(ctx, mgr)
	r.logger.Info("Setting up ClusterResourceQuota controller")
	return r.installWatches(mgr)
}

// ensureDependencies lazily initialises all reconciler-owned collaborators.
// Tests can pre-populate any field; production paths fall back to defaults.
func (r *ClusterResourceQuotaReconciler) ensureDependencies(mgr ctrl.Manager) {
	if r.logger == nil {
		r.logger = zap.L().Named("clusterresourcequota-controller")
	}
	if r.crqClient == nil {
		r.crqClient = quota.NewCRQClient(r.Client, r.logger)
	}
	if r.ObjectCountCalculator == nil {
		r.ObjectCountCalculator = objectcount.NewObjectCountCalculator(r.Client, r.logger)
	}
	if r.EventRecorder == nil {
		r.EventRecorder = events.NewEventRecorder(
			mgr.GetEventRecorder("pac-quota-controller"),
			r.logger,
		)
	}
	if r.previousNamespacesByQuota == nil {
		r.previousNamespacesByQuota = make(map[string][]string)
	}
	if r.lastQuotaExceededAt == nil {
		r.lastQuotaExceededAt = make(map[string]time.Time)
	}
}

// startBackgroundWorkers fires the long-lived goroutines that outlive a
// single Reconcile. Currently just the event-cleanup manager (exits on ctx).
func (r *ClusterResourceQuotaReconciler) startBackgroundWorkers(ctx context.Context, mgr ctrl.Manager) {
	cleanupConfig := r.resolveCleanupConfig()
	cleanupManager := events.NewEventCleanupManager(mgr.GetClient(), cleanupConfig, r.logger)
	go cleanupManager.Start(ctx)
}

func (r *ClusterResourceQuotaReconciler) resolveCleanupConfig() events.CleanupConfig {
	if r.Config != nil && r.Config.EventsEnable {
		cleanupConfig, err := events.LoadEventCleanupConfig(
			r.Config.EventsConfigPath,
			r.Config.EventsTTL,
			r.Config.EventsMaxEventsPerCRQ,
			r.Config.EventsCleanupInterval,
		)
		if err != nil {
			r.logger.Warn("Failed to load event cleanup config, using defaults", zap.Error(err))
			return events.DefaultCleanupConfig()
		}
		return cleanupConfig
	}
	return events.CleanupConfig{Enabled: false}
}

// installWatches wires the CRQ owner watch plus every cross-resource watch
// that should re-enqueue the matching CRQ.
func (r *ClusterResourceQuotaReconciler) installWatches(mgr ctrl.Manager) error {
	resourcePredicate := resourceUpdatePredicate{}
	watched := []struct {
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

	b := ctrl.NewControllerManagedBy(mgr).
		For(&quotav1alpha1.ClusterResourceQuota{}).
		WithOptions(ctrlcontroller.Options{MaxConcurrentReconciles: 5})
	for _, w := range watched {
		b = b.Watches(
			w.obj,
			handler.EnqueueRequestsFromMapFunc(r.findQuotasForObject),
			builder.WithPredicates(w.preds...),
		)
	}
	return b.Named("clusterresourcequota").Complete(r)
}
