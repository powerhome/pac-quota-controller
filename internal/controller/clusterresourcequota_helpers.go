package controller

import (
	"sort"
	"time"

	quotav1alpha1 "github.com/powerhome/pac-quota-controller/api/v1alpha1"
)

// quotaExceededCooldown is the minimum interval between QuotaExceeded events
// for the same CRQ+resource pair. Prevents etcd write storms when a CRQ is
// persistently over quota and reconciles fire on every pod change.
const quotaExceededCooldown = 5 * time.Minute

// handleNamespaceChanges detects and records namespace additions/removals.
// Event emission happens outside the lock to avoid blocking reconciles.
func (r *ClusterResourceQuotaReconciler) handleNamespaceChanges(crq *quotav1alpha1.ClusterResourceQuota, currentNamespaces []string) {
	r.mu.Lock()
	previousNamespaces := r.previousNamespacesByQuota[crq.Name]

	prevSet := make(map[string]bool, len(previousNamespaces))
	for _, ns := range previousNamespaces {
		prevSet[ns] = true
	}

	currSet := make(map[string]bool, len(currentNamespaces))
	for _, ns := range currentNamespaces {
		currSet[ns] = true
	}

	var added, removed []string
	for _, ns := range currentNamespaces {
		if !prevSet[ns] {
			added = append(added, ns)
		}
	}
	for _, ns := range previousNamespaces {
		if !currSet[ns] {
			removed = append(removed, ns)
		}
	}

	updated := make([]string, len(currentNamespaces))
	copy(updated, currentNamespaces)
	sort.Strings(updated)
	r.previousNamespacesByQuota[crq.Name] = updated
	r.mu.Unlock()

	for _, ns := range added {
		r.EventRecorder.NamespaceAdded(crq, ns)
	}
	for _, ns := range removed {
		r.EventRecorder.NamespaceRemoved(crq, ns)
	}
}

// checkQuotaThresholds emits a QuotaExceeded event for each over-limit resource,
// rate-limited to at most one event per CRQ+resource per quotaExceededCooldown.
func (r *ClusterResourceQuotaReconciler) checkQuotaThresholds(crq *quotav1alpha1.ClusterResourceQuota, usage quotav1alpha1.ResourceList) {
	now := time.Now()
	for resourceName, limit := range crq.Spec.Hard {
		used := usage[resourceName]
		if limit.IsZero() || used.Cmp(limit) <= 0 {
			continue
		}

		key := crq.Name + "/" + string(resourceName)
		r.mu.Lock()
		if r.lastQuotaExceededAt == nil {
			r.lastQuotaExceededAt = make(map[string]time.Time)
		}
		last := r.lastQuotaExceededAt[key]
		if now.Sub(last) < quotaExceededCooldown {
			r.mu.Unlock()
			continue
		}
		r.lastQuotaExceededAt[key] = now
		r.mu.Unlock()

		r.EventRecorder.QuotaExceeded(crq, string(resourceName), used, limit)
	}
}
