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

	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/types"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/handler"
	logf "sigs.k8s.io/controller-runtime/pkg/log"
	"sigs.k8s.io/controller-runtime/pkg/reconcile"

	quotav1alpha1 "github.com/powerhome/pac-quota-controller/api/v1alpha1"
	"github.com/powerhome/pac-quota-controller/pkg/kubernetes"
)

var log = logf.Log.WithName("clusterresourcequota-controller")

// ClusterResourceQuotaReconciler reconciles a ClusterResourceQuota object
type ClusterResourceQuotaReconciler struct {
	client.Client
	Scheme    *runtime.Scheme
	crqClient *kubernetes.CRQClient
}

// +kubebuilder:rbac:groups=quota.powerapp.cloud,resources=clusterresourcequotas,verbs=get;list;watch;create;update;patch;delete
// +kubebuilder:rbac:groups=quota.powerapp.cloud,resources=clusterresourcequotas/status,verbs=get;update;patch
// +kubebuilder:rbac:groups=quota.powerapp.cloud,resources=clusterresourcequotas/finalizers,verbs=update
// +kubebuilder:rbac:groups=core,resources=namespaces,verbs=get;list;watch
// +kubebuilder:rbac:groups=core,resources=pods,verbs=get;list;watch
// +kubebuilder:rbac:groups=core,resources=services,verbs=get;list;watch
// +kubebuilder:rbac:groups=core,resources=persistentvolumeclaims,verbs=get;list;watch
// +kubebuilder:rbac:groups=core,resources=secrets,verbs=get;list;watch
// +kubebuilder:rbac:groups=core,resources=configmaps,verbs=get;list;watch

// Reconcile is part of the main kubernetes reconciliation loop which aims to
// move the current state of the cluster closer to the desired state.
// It implements the logic to select namespaces, create/update ResourceQuotas in those
// namespaces, and keep track of aggregate usage.
//
// For more details, check Reconcile and its Result here:
// - https://pkg.go.dev/sigs.k8s.io/controller-runtime@v0.21.0/pkg/reconcile
func (r *ClusterResourceQuotaReconciler) Reconcile(ctx context.Context, req ctrl.Request) (ctrl.Result, error) {
	log := log.WithValues("clusterresourcequota", req.NamespacedName)
	log.Info("Reconciling ClusterResourceQuota")

	// Fetch the ClusterResourceQuota instance
	crq := &quotav1alpha1.ClusterResourceQuota{}
	if err := r.Get(ctx, req.NamespacedName, crq); err != nil {
		if client.IgnoreNotFound(err) == nil {
			// Object not found, likely deleted, return without error
			log.Info("ClusterResourceQuota resource not found. Ignoring since object must have been deleted")
			return ctrl.Result{}, nil
		}
		// Error reading the object - requeue the request
		log.Error(err, "Failed to get ClusterResourceQuota")
		return ctrl.Result{}, err
	}

	// Use CRQClient for namespace selection
	if r.crqClient == nil {
		r.crqClient = kubernetes.NewCRQClient(r.Client)
	}
	selectedNamespaces, err := kubernetes.GetSelectedNamespaces(ctx, r.Client, crq)
	if err != nil {
		log.Error(err, "Failed to get selected namespaces")
		return ctrl.Result{}, err
	}

	log.Info("Found namespaces matching selection criteria", "count", len(selectedNamespaces), "namespaces", selectedNamespaces)

	// Use CRQClient for extracting previous namespaces from status
	previousNamespaces := r.crqClient.GetNamespacesFromStatus(crq)

	// Determine what changed (which namespaces were added or removed)
	addedNamespaces, removedNamespaces := kubernetes.DetermineNamespaceChanges(previousNamespaces, selectedNamespaces)

	if len(addedNamespaces) > 0 {
		log.Info("Namespaces added to selection", "namespaces", addedNamespaces)
	}

	if len(removedNamespaces) > 0 {
		log.Info("Namespaces removed from selection", "namespaces", removedNamespaces)
	}

	// Store current namespaces in status for future comparisons
	if err := r.updateNamespaceStatus(ctx, crq, selectedNamespaces); err != nil {
		log.Error(err, "Failed to update namespace status")
		return ctrl.Result{}, err
	}

	// In the Reconcile function, after reconciling CRQs, update the ownership cache
	// (This is a simplified example; actual logic should be placed after CRQ status is updated)
	if err := r.updateNamespaceOwnershipCache(ctx); err != nil {
		log.Error(err, "Failed to update namespace ownership cache")
		return ctrl.Result{}, err
	}

	// TODO: Further implementation to apply quotas to each namespace
	// and aggregate usage will be implemented in follow-up PRs

	log.Info("Finished reconciliation")
	return ctrl.Result{}, nil
}

// findQuotasForNamespace maps Namespace objects to ClusterResourceQuota requests
// that should be reconciled based on namespace selection criteria
func (r *ClusterResourceQuotaReconciler) findQuotasForNamespace(ctx context.Context, obj client.Object) []reconcile.Request {
	ns, ok := obj.(*corev1.Namespace)
	if !ok {
		return nil
	}

	logger := log
	logger = logger.WithValues("namespace", ns.Name)

	logger.Info("Processing namespace event")

	// List all ClusterResourceQuotas
	quotaList := &quotav1alpha1.ClusterResourceQuotaList{}
	if err := r.List(ctx, quotaList); err != nil {
		logger.Error(err, "Failed to list ClusterResourceQuotas")
		return nil
	}

	// For each ClusterResourceQuota, check if the namespace matches any of its selection criteria
	var requests []reconcile.Request
	for i := range quotaList.Items {
		quota := &quotaList.Items[i]
		shouldEnqueue, err := r.crqClient.NamespaceMatchesCRQ(ns, quota)
		if err != nil {
			logger.Error(err, "Failed to check namespace match", "quota", quota.Name)
			continue
		}
		if shouldEnqueue {
			logger.Info("Enqueueing ClusterResourceQuota for reconciliation due to namespace change",
				"quota", quota.Name)
			requests = append(requests, reconcile.Request{
				NamespacedName: types.NamespacedName{
					Name: quota.Name,
				},
			})
		}
	}

	return requests
}

// updateNamespaceAnnotation stores the list of namespaces in an annotation on the ClusterResourceQuota
func (r *ClusterResourceQuotaReconciler) updateNamespaceStatus(ctx context.Context, crq *quotav1alpha1.ClusterResourceQuota, namespaces []string) error {
	// Create a copy to avoid updating the object in the cache
	crqCopy := crq.DeepCopy()

	// Initialize annotations if needed
	crqCopy.Status.Namespaces = make([]quotav1alpha1.ResourceQuotaStatusByNamespace, len(namespaces))
	for i, ns := range namespaces {
		crqCopy.Status.Namespaces[i] = quotav1alpha1.ResourceQuotaStatusByNamespace{
			Namespace: ns,
			// Resource Usage will be populated later
		}
	}

	return r.Status().Update(ctx, crqCopy)
}

// SetupWithManager sets up the controller with the Manager.
func (r *ClusterResourceQuotaReconciler) SetupWithManager(mgr ctrl.Manager) error {
	// Watch for changes to ClusterResourceQuota objects
	// Also watch for changes to Namespaces, as namespace label changes may affect quota application
	return ctrl.NewControllerManagedBy(mgr).
		For(&quotav1alpha1.ClusterResourceQuota{}).
		Watches(
			&corev1.Namespace{},
			handler.EnqueueRequestsFromMapFunc(r.findQuotasForNamespace),
		).
		Named("clusterresourcequota").
		Complete(r)
}
