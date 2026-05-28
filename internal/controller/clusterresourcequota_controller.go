package controller

import (
	"context"
	"fmt"
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
	"go.uber.org/zap"
	corev1 "k8s.io/api/core/v1"
	apiequality "k8s.io/apimachinery/pkg/api/equality"
	"k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/api/resource"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/labels"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/client-go/kubernetes"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
)

// ClusterResourceQuotaReconciler reconciles a ClusterResourceQuota object
type ClusterResourceQuotaReconciler struct {
	client.Client
	Scheme                   *runtime.Scheme
	KubeClient               kubernetes.Interface
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
	usageStateStore           *usageStateStore
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
	if r.usageStateStore != nil {
		r.usageStateStore.ensureQuotaNamespaces(crq.Name, selectedNamespaces)
	}

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
		r.logger.Info("Unsupported object count resource for calculateObjectCount",
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
