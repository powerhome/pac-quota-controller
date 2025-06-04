package namespaceselection

import (
	"context"
	"fmt"
	"sort"
	"strings"

	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/labels"
	"sigs.k8s.io/controller-runtime/pkg/client"
)

// NamespaceSelector is an interface for selecting namespaces based on criteria
type NamespaceSelector interface {
	// GetSelectedNamespaces returns the list of namespaces that match the selector
	GetSelectedNamespaces(ctx context.Context) ([]string, error)
	// DetermineNamespaceChanges checks what namespaces have been added or removed since last reconciliation
	DetermineNamespaceChanges(ctx context.Context, previousNamespaces []string) (added []string, removed []string, err error)
}

// LabelBasedNamespaceSelector selects namespaces based on label selectors
type LabelBasedNamespaceSelector struct {
	client         client.Client
	labelSelector  *metav1.LabelSelector
	cachedSelector labels.Selector
}

// NewLabelBasedNamespaceSelector creates a new namespace selector based on label selector
func NewLabelBasedNamespaceSelector(k8sClient client.Client, labelSelector *metav1.LabelSelector) (*LabelBasedNamespaceSelector, error) {
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
func (s *LabelBasedNamespaceSelector) GetSelectedNamespaces(ctx context.Context) ([]string, error) {
	// Get all namespaces
	namespaceList := &corev1.NamespaceList{}
	if err := s.client.List(ctx, namespaceList); err != nil {
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
func (s *LabelBasedNamespaceSelector) DetermineNamespaceChanges(ctx context.Context, previousNamespaces []string) (added []string, removed []string, err error) {
	currentNamespaces, err := s.GetSelectedNamespaces(ctx)
	if err != nil {
		return nil, nil, fmt.Errorf("failed to get currently selected namespaces: %w", err)
	}

	// Create maps for faster lookup
	prevMap := make(map[string]struct{}, len(previousNamespaces))
	currMap := make(map[string]struct{}, len(currentNamespaces))

	for _, ns := range previousNamespaces {
		prevMap[ns] = struct{}{}
	}
	for _, ns := range currentNamespaces {
		currMap[ns] = struct{}{}
	}

	// Find added namespaces
	for _, ns := range currentNamespaces {
		if _, exists := prevMap[ns]; !exists {
			added = append(added, ns)
		}
	}

	// Find removed namespaces
	for _, ns := range previousNamespaces {
		if _, exists := currMap[ns]; !exists {
			removed = append(removed, ns)
		}
	}

	return added, removed, nil
}

// GetNamespacesFromAnnotation extracts namespace list from the ClusterResourceQuota annotation
func GetNamespacesFromAnnotation(annotations map[string]string) []string {
	if annotations == nil {
		return nil
	}

	nsString, exists := annotations["quota.powerapp.cloud/namespaces"]
	if !exists || nsString == "" {
		return nil
	}

	namespaces := strings.Split(nsString, ",")
	for i, ns := range namespaces {
		namespaces[i] = strings.TrimSpace(ns)
	}

	return namespaces
}
