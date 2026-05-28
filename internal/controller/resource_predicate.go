package controller

import (
	"github.com/powerhome/pac-quota-controller/pkg/kubernetes/pod"
	corev1 "k8s.io/api/core/v1"
	"sigs.k8s.io/controller-runtime/pkg/event"
	"sigs.k8s.io/controller-runtime/pkg/predicate"
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
