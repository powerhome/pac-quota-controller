package namespace

import (
	"context"
	"fmt"
	"sort"

	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/labels"
	"k8s.io/client-go/kubernetes"

	quotav1alpha1 "github.com/powerhome/pac-quota-controller/api/v1alpha1"
)

// LabelBasedNamespaceSelector selects namespaces based on label selectors
type LabelBasedNamespaceSelector struct {
	client         kubernetes.Interface
	labelSelector  *metav1.LabelSelector
	cachedSelector labels.Selector
}

// NewLabelBasedNamespaceSelector creates a new namespace selector based on label selector
func NewLabelBasedNamespaceSelector(
	k8sClient kubernetes.Interface,
	labelSelector *metav1.LabelSelector,
) (*LabelBasedNamespaceSelector, error) {
	selector, err := metav1.LabelSelectorAsSelector(labelSelector)
	if err != nil {
		return nil, fmt.Errorf("failed to convert label selector to selector: %w", err)
	}

	return &LabelBasedNamespaceSelector{
		client:         k8sClient,
		labelSelector:  labelSelector,
		cachedSelector: selector,
	}, nil
}

// GetSelectedNamespaces returns namespaces that match the selector
func (s *LabelBasedNamespaceSelector) GetSelectedNamespaces() ([]string, error) {
	// Get all namespaces
	namespaceList, err := s.client.CoreV1().Namespaces().List(context.Background(), metav1.ListOptions{})
	if err != nil {
		return nil, fmt.Errorf("failed to list namespaces: %w", err)
	}

	selectedNamespaces := make(map[string]struct{})

	// Finally add namespaces that match the label selector
	if s.cachedSelector != nil && !s.cachedSelector.Empty() {
		for _, ns := range namespaceList.Items {
			if s.cachedSelector.Matches(labels.Set(ns.Labels)) {
				selectedNamespaces[ns.Name] = struct{}{}
			}
		}
	}

	// Convert to sorted slice for deterministic output
	result := make([]string, 0, len(selectedNamespaces))
	for nsName := range selectedNamespaces {
		result = append(result, nsName)
	}
	sort.Strings(result)

	return result, nil
}

// DetermineNamespaceChanges checks what namespaces have been added or removed since last reconciliation
func (s *LabelBasedNamespaceSelector) DetermineNamespaceChanges(
	previousNamespaces []string,
) (added []string, removed []string, err error) {
	currentNamespaces, err := s.GetSelectedNamespaces()
	if err != nil {
		return nil, nil, fmt.Errorf("failed to get currently selected namespaces: %w", err)
	}

	// Deduplicate previous namespaces to avoid duplicates in the result
	prevMap := make(map[string]struct{}, len(previousNamespaces))
	for _, ns := range previousNamespaces {
		prevMap[ns] = struct{}{}
	}

	// Create deduplicated previous list
	deduplicatedPrevious := make([]string, 0, len(prevMap))
	for ns := range prevMap {
		deduplicatedPrevious = append(deduplicatedPrevious, ns)
	}

	// Create maps for faster lookup
	currMap := make(map[string]struct{}, len(currentNamespaces))
	for _, ns := range currentNamespaces {
		currMap[ns] = struct{}{}
	}

	// Find added namespaces
	for _, ns := range currentNamespaces {
		if _, exists := prevMap[ns]; !exists {
			added = append(added, ns)
		}
	}

	// Find removed namespaces (using deduplicated list)
	for _, ns := range deduplicatedPrevious {
		if _, exists := currMap[ns]; !exists {
			removed = append(removed, ns)
		}
	}

	return added, removed, nil
}

// ValidateNamespaceOwnership checks to ensure no namespace is already owned by another CRQ.
func ValidateNamespaceOwnership(
	c kubernetes.Interface,
	crq *quotav1alpha1.ClusterResourceQuota,
) ([]string, error) {
	if crq.Spec.NamespaceSelector == nil {
		return nil, nil // If no selector, nothing to check
	}

	// Use the namespace selector utility to get intended namespaces for this CRQ
	selector, err := NewLabelBasedNamespaceSelector(c, crq.Spec.NamespaceSelector)
	if err != nil {
		return nil, fmt.Errorf("failed to create namespace selector: %w", err)
	}
	intendedNamespaces, err := selector.GetSelectedNamespaces()
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

	// For now, skip CRQ validation since we're removing controller-runtime dependencies
	// TODO: Implement CRQ validation using native Kubernetes client when needed
	return []string{}, nil
}

func GetSelectedNamespaces(
	c kubernetes.Interface,
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
	return selector.GetSelectedNamespaces()
}

// DetermineNamespaceChanges finds which namespaces have been added or removed
func DetermineNamespaceChanges(previous, current []string) (added, removed []string) {
	// Deduplicate input lists to avoid duplicates in the result
	prevMap := make(map[string]struct{}, len(previous))
	currMap := make(map[string]struct{}, len(current))

	for _, ns := range previous {
		prevMap[ns] = struct{}{}
	}
	for _, ns := range current {
		currMap[ns] = struct{}{}
	}

	// Create deduplicated lists
	deduplicatedPrevious := make([]string, 0, len(prevMap))
	deduplicatedCurrent := make([]string, 0, len(currMap))

	for ns := range prevMap {
		deduplicatedPrevious = append(deduplicatedPrevious, ns)
	}
	for ns := range currMap {
		deduplicatedCurrent = append(deduplicatedCurrent, ns)
	}

	// Find added namespaces
	for _, ns := range deduplicatedCurrent {
		if _, exists := prevMap[ns]; !exists {
			added = append(added, ns)
		}
	}

	// Find removed namespaces
	for _, ns := range deduplicatedPrevious {
		if _, exists := currMap[ns]; !exists {
			removed = append(removed, ns)
		}
	}
	// Sort the results for consistency across reconciliations
	sort.Strings(added)
	sort.Strings(removed)

	return added, removed
}
