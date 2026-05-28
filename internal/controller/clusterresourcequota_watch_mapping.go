package controller

import (
	"context"
	"slices"

	appsv1 "k8s.io/api/apps/v1"
	autoscalingv1 "k8s.io/api/autoscaling/v1"
	batchv1 "k8s.io/api/batch/v1"
	corev1 "k8s.io/api/core/v1"
	networkingv1 "k8s.io/api/networking/v1"
	"k8s.io/apimachinery/pkg/types"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/reconcile"

	"go.uber.org/zap"
)

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
		if r.usageStateStore != nil {
			r.markNamespaceDirtyForObject(crq.Name, ns.Name, obj)
		}
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

func (r *ClusterResourceQuotaReconciler) markNamespaceDirtyForObject(quotaName, namespace string, obj client.Object) {
	if r.usageStateStore == nil {
		return
	}

	switch obj.(type) {
	case *corev1.Pod:
		r.usageStateStore.markNamespaceComputeDirty(quotaName, namespace)
	case *corev1.Service:
		r.usageStateStore.markNamespaceServicesDirty(quotaName, namespace)
	case *corev1.PersistentVolumeClaim:
		r.usageStateStore.markNamespaceStorageDirty(quotaName, namespace)
	case *corev1.ConfigMap,
		*corev1.Secret,
		*corev1.ReplicationController,
		*appsv1.Deployment,
		*appsv1.StatefulSet,
		*appsv1.DaemonSet,
		*batchv1.Job,
		*batchv1.CronJob,
		*autoscalingv1.HorizontalPodAutoscaler,
		*networkingv1.Ingress:
		r.usageStateStore.markNamespaceObjectCountDirty(quotaName, namespace)
	}
}
