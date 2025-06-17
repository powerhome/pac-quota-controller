package namespace

import (
	"context"
	"fmt"
	"sort"

	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/webhook/admission"

	quotav1alpha1 "github.com/powerhome/pac-quota-controller/api/v1alpha1"
)

// ValidateNamespaceOwnership checks to ensure no namespace is already owned by another CRQ.
func ValidateNamespaceOwnership(
	ctx context.Context,
	c client.Client,
	crq *quotav1alpha1.ClusterResourceQuota,
) (admission.Warnings, error) {
	if crq.Spec.NamespaceSelector == nil {
		return nil, nil // If no selector, nothing to check
	}

	// Use the namespace selector utility to get intended namespaces for this CRQ
	selector, err := NewLabelBasedNamespaceSelector(c, crq.Spec.NamespaceSelector)
	if err != nil {
		return nil, fmt.Errorf("failed to create namespace selector: %w", err)
	}
	intendedNamespaces, err := selector.GetSelectedNamespaces(ctx)
	if err != nil {
		return nil, fmt.Errorf("failed to select namespaces: %w", err)
	}
	if len(intendedNamespaces) == 0 {
		return nil, nil // No intended namespaces, nothing to check
	}
	myNamespaces := make(map[string]struct{}, len(intendedNamespaces))
	for _, ns := range intendedNamespaces {
		myNamespaces[ns] = struct{}{}
	}

	// List all CRQs
	crqList := &quotav1alpha1.ClusterResourceQuotaList{}
	if err := c.List(ctx, crqList); err != nil {
		return nil, fmt.Errorf("failed to list ClusterResourceQuotas: %w", err)
	}

	// Check for conflicts with other CRQs' status.namespaces
	conflicts := []string{}
	for _, otherCRQ := range crqList.Items {
		if otherCRQ.Name == crq.Name {
			continue
		}
		for _, nsStatus := range otherCRQ.Status.Namespaces {
			if _, conflict := myNamespaces[nsStatus.Namespace]; conflict {
				conflicts = append(conflicts, fmt.Sprintf(
					"namespace '%s' is already owned by another ClusterResourceQuota",
					nsStatus.Namespace,
				))
			}
		}
	}
	if len(conflicts) > 0 {
		return nil, fmt.Errorf("namespace ownership conflict: %s", conflicts)
	}
	return nil, nil
}

func GetSelectedNamespaces(
	ctx context.Context,
	c client.Client,
	crq *quotav1alpha1.ClusterResourceQuota,
) ([]string, error) {
	if crq.Spec.NamespaceSelector == nil {
		return nil, nil // No selector means no namespaces to select
	}

	// Use the namespace selector utility to get intended namespaces for this CRQ
	selector, err := NewLabelBasedNamespaceSelector(c, crq.Spec.NamespaceSelector)
	if err != nil {
		return nil, fmt.Errorf("failed to create namespace selector: %w", err)
	}
	return selector.GetSelectedNamespaces(ctx)
}

// DetermineNamespaceChanges finds which namespaces have been added or removed
func DetermineNamespaceChanges(previous, current []string) (added, removed []string) {
	// Create maps for faster lookup
	prevMap := make(map[string]struct{}, len(previous))
	currMap := make(map[string]struct{}, len(current))

	for _, ns := range previous {
		prevMap[ns] = struct{}{}
	}
	for _, ns := range current {
		currMap[ns] = struct{}{}
	}

	// Find added namespaces
	for _, ns := range current {
		if _, exists := prevMap[ns]; !exists {
			added = append(added, ns)
		}
	}

	// Find removed namespaces
	for _, ns := range previous {
		if _, exists := currMap[ns]; !exists {
			removed = append(removed, ns)
		}
	}
	// Sort the results for consistency across reconciliations
	sort.Strings(added)
	sort.Strings(removed)

	return added, removed
}
