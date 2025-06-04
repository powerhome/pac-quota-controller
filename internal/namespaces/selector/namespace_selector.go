package selector

import (
	"context"
	"fmt"

	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"
	"sigs.k8s.io/controller-runtime/pkg/client"
	logf "sigs.k8s.io/controller-runtime/pkg/log"

	"github.com/powerhome/pac-quota-controller/pkg/utils"
)

var log = logf.Log.WithName("namespace_selector")

// NamespaceSelector is an interface for selecting namespaces based on criteria
type NamespaceSelector interface {
	// GetSelectedNamespaces returns the list of namespaces that match the selector
	GetSelectedNamespaces(ctx context.Context) ([]string, error)
	// DetermineNamespaceChanges checks what namespaces have been added or removed since last reconciliation
	DetermineNamespaceChanges(ctx context.Context, previousNamespaces []string) (added []string, removed []string, err error)
}

// LabelBasedNamespaceSelector selects namespaces based on label selectors
type LabelBasedNamespaceSelector struct {
	client        client.Client
	labelSelector *metav1.LabelSelector
}

// NewLabelBasedNamespaceSelector creates a new namespace selector based on labels
func NewLabelBasedNamespaceSelector(
	client client.Client,
	labelSelector *metav1.LabelSelector,
) *LabelBasedNamespaceSelector {
	return &LabelBasedNamespaceSelector{
		client:        client,
		labelSelector: labelSelector,
	}
}

// GetSelectedNamespaces returns the namespaces that match the selector
func (s *LabelBasedNamespaceSelector) GetSelectedNamespaces(ctx context.Context) ([]string, error) {
	var selectedNamespaces []string
	var err error

	// Add namespaces that match the label selector
	if s.labelSelector != nil {
		var labelNamespaces []string
		labelNamespaces, err = s.getNamespacesByLabelSelector(ctx)
		if err != nil {
			return nil, fmt.Errorf("failed to get namespaces by label selector: %w", err)
		}
		selectedNamespaces = append(selectedNamespaces, labelNamespaces...)
	}

	// Remove duplicates and sort
	return utils.DeduplicateAndSort(selectedNamespaces), nil
}

// DetermineNamespaceChanges finds which namespaces have been added or removed since last reconciliation
func (s *LabelBasedNamespaceSelector) DetermineNamespaceChanges(
	ctx context.Context,
	previousNamespaces []string,
) ([]string, []string, error) {
	currentNamespaces, err := s.GetSelectedNamespaces(ctx)
	if err != nil {
		return nil, nil, fmt.Errorf("failed to get current selected namespaces: %w", err)
	}

	// Convert slices to maps for easier comparison
	currentMap := make(map[string]struct{})
	previousMap := make(map[string]struct{})

	for _, ns := range currentNamespaces {
		currentMap[ns] = struct{}{}
	}

	for _, ns := range previousNamespaces {
		previousMap[ns] = struct{}{}
	}

	// Find added namespaces (in current but not in previous)
	var added []string
	for ns := range currentMap {
		if _, exists := previousMap[ns]; !exists {
			added = append(added, ns)
		}
	}

	// Find removed namespaces (in previous but not in current)
	var removed []string
	for ns := range previousMap {
		if _, exists := currentMap[ns]; !exists {
			removed = append(removed, ns)
		}
	}

	// Sort for consistent results
	added = utils.DeduplicateAndSort(added)
	removed = utils.DeduplicateAndSort(removed)

	return added, removed, nil
}

// getNamespacesByLabelSelector returns namespaces that match the label selector
func (s *LabelBasedNamespaceSelector) getNamespacesByLabelSelector(ctx context.Context) ([]string, error) {
	selector, err := metav1.LabelSelectorAsSelector(s.labelSelector)
	if err != nil {
		return nil, fmt.Errorf("invalid label selector: %w", err)
	}

	var namespaceList corev1.NamespaceList
	if err := s.client.List(ctx, &namespaceList, &client.ListOptions{
		LabelSelector: selector,
	}); err != nil {
		return nil, fmt.Errorf("failed to list namespaces: %w", err)
	}

	var result []string
	for _, ns := range namespaceList.Items {
		if ns.Status.Phase == corev1.NamespaceTerminating {
			log.Info("Skipping namespace in terminating state", "namespace", ns.Name)
			continue
		}
		result = append(result, ns.Name)
	}

	return result, nil
}

// namespaceExists checks if a namespace exists
func (s *LabelBasedNamespaceSelector) namespaceExists(ctx context.Context, name string) error {
	var namespace corev1.Namespace
	if err := s.client.Get(ctx, types.NamespacedName{Name: name}, &namespace); err != nil {
		if errors.IsNotFound(err) {
			return fmt.Errorf("namespace %s does not exist", name)
		}
		return fmt.Errorf("error checking if namespace exists: %w", err)
	}

	if namespace.Status.Phase == corev1.NamespaceTerminating {
		return fmt.Errorf("namespace %s is in terminating state", name)
	}

	return nil
}
