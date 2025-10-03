package controller

import (
	"sort"

	quotav1alpha1 "github.com/powerhome/pac-quota-controller/api/v1alpha1"
)

// handleNamespaceChanges detects and records namespace additions/removals
func (r *ClusterResourceQuotaReconciler) handleNamespaceChanges(crq *quotav1alpha1.ClusterResourceQuota, currentNamespaces []string) {
	previousNamespaces := r.previousNamespacesByQuota[crq.Name]

	// Convert to sets for easy comparison
	prevSet := make(map[string]bool)
	for _, ns := range previousNamespaces {
		prevSet[ns] = true
	}

	currSet := make(map[string]bool)
	for _, ns := range currentNamespaces {
		currSet[ns] = true
		if !prevSet[ns] {
			r.EventRecorder.NamespaceAdded(crq, ns)
		}
	}

	for _, ns := range previousNamespaces {
		if !currSet[ns] {
			r.EventRecorder.NamespaceRemoved(crq, ns)
		}
	}

	// Update the tracking
	r.previousNamespacesByQuota[crq.Name] = make([]string, len(currentNamespaces))
	copy(r.previousNamespacesByQuota[crq.Name], currentNamespaces)
	sort.Strings(r.previousNamespacesByQuota[crq.Name])
}

// checkQuotaThresholds checks for quota warnings and violations
func (r *ClusterResourceQuotaReconciler) checkQuotaThresholds(crq *quotav1alpha1.ClusterResourceQuota, usage quotav1alpha1.ResourceList) {
	for resourceName, limit := range crq.Spec.Hard {
		used := usage[resourceName]

		usedQuantity := used.Value()
		limitQuantity := limit.Value()

		if limitQuantity > 0 {
			// Record violation event
			if usedQuantity > limitQuantity {
				r.EventRecorder.QuotaExceeded(crq, string(resourceName), usedQuantity, limitQuantity)
			}
		}
	}
}
