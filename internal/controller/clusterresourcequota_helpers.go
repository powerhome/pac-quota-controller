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

		// Use resource.Quantity comparison instead of .Value() to preserve fractional values
		// Only check if limit is greater than zero to avoid division by zero scenarios
		if !limit.IsZero() && used.Cmp(limit) > 0 {
			// Record violation event with human-readable format using resource.Quantity
			// This preserves the original unit format (e.g., "1500m" for CPU, "200Mi" for memory)
			r.EventRecorder.QuotaExceeded(crq, string(resourceName), used, limit)
		}
	}
}
