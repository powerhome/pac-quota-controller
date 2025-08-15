package namespace

import (
	"context"
	"fmt"
	"sort"
	"strings"

	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/labels"
	"k8s.io/client-go/kubernetes"

	quotav1alpha1 "github.com/powerhome/pac-quota-controller/api/v1alpha1"
	"github.com/powerhome/pac-quota-controller/pkg/kubernetes/quota"
)

// NamespaceValidator handles validation logic for namespaces and CRQs
type NamespaceValidator struct {
	kubernetesClient kubernetes.Interface
	crqClient        *quota.CRQClient
}

// NewNamespaceValidator creates a new NamespaceValidator
func NewNamespaceValidator(kubernetesClient kubernetes.Interface, crqClient *quota.CRQClient) *NamespaceValidator {
	return &NamespaceValidator{
		kubernetesClient: kubernetesClient,
		crqClient:        crqClient,
	}
}

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
func (s *LabelBasedNamespaceSelector) GetSelectedNamespaces(ctx context.Context) ([]string, error) {
	// Get all namespaces
	namespaceList, err := s.client.CoreV1().Namespaces().List(ctx, metav1.ListOptions{})
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
	ctx context.Context,
	previousNamespaces []string,
) (added []string, removed []string, err error) {
	currentNamespaces, err := s.GetSelectedNamespaces(ctx)
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

// ValidateCRQNamespaceConflicts validates that a CRQ doesn't conflict with existing CRQs
func (v *NamespaceValidator) ValidateCRQNamespaceConflicts(
	ctx context.Context,
	crq *quotav1alpha1.ClusterResourceQuota,
) error {
	if crq.Spec.NamespaceSelector == nil {
		return nil // If no selector, nothing to check
	}

	// Use the namespace selector utility to get intended namespaces for this CRQ
	selector, err := NewLabelBasedNamespaceSelector(v.kubernetesClient, crq.Spec.NamespaceSelector)
	if err != nil {
		return fmt.Errorf("failed to create namespace selector: %w", err)
	}
	intendedNamespaces, err := selector.GetSelectedNamespaces(ctx)
	if err != nil {
		return fmt.Errorf("failed to select namespaces: %w", err)
	}
	if len(intendedNamespaces) == 0 {
		return nil // No intended namespaces, nothing to check
	}

	// Check if any of the intended namespaces are already owned by other CRQs
	conflictingCRQs, err := v.findConflictingCRQsForNamespaces(ctx, intendedNamespaces, crq.Name)
	if err != nil {
		return fmt.Errorf("failed to check for conflicting CRQs: %w", err)
	}

	if len(conflictingCRQs) > 0 {
		var conflictMessages []string
		for namespaceName, crqNames := range conflictingCRQs {
			conflictMessages = append(conflictMessages,
				fmt.Sprintf("namespace '%s' is already selected by ClusterResourceQuota(s): %v", namespaceName, crqNames))
		}
		return fmt.Errorf("namespace ownership conflict: ClusterResourceQuota '%s' cannot be created/updated "+
			"because it would conflict with existing ClusterResourceQuotas: %s",
			crq.Name, strings.Join(conflictMessages, "; "))
	}

	return nil
}

// ValidateNamespaceAgainstCRQs validates that a namespace doesn't conflict with existing CRQs
func (v *NamespaceValidator) ValidateNamespaceAgainstCRQs(ctx context.Context, namespace *corev1.Namespace) error {
	// Get all existing CRQs
	allCRQs, err := v.listAllCRQs(ctx)
	if err != nil {
		return fmt.Errorf("failed to list CRQs: %w", err)
	}

	// Check which CRQs would select this namespace
	var matchingCRQs []string
	for _, crq := range allCRQs {
		matches, err := v.namespaceMatchesCRQ(namespace, &crq)
		if err != nil {
			return fmt.Errorf("failed to check if namespace %s matches CRQ %s: %w",
				namespace.Name, crq.Name, err)
		}

		if matches {
			matchingCRQs = append(matchingCRQs, crq.Name)
		}
	}

	// If more than one CRQ would select this namespace, it's a conflict
	if len(matchingCRQs) > 1 {
		return fmt.Errorf("multiple ClusterResourceQuotas select namespace \"%s\": %v",
			namespace.Name, matchingCRQs)
	}

	return nil
}

// findConflictingCRQsForNamespaces checks if any of the given namespaces are already selected by other CRQs
func (v *NamespaceValidator) findConflictingCRQsForNamespaces(
	ctx context.Context, namespaces []string, excludeCRQName string,
) (map[string][]string, error) {
	conflicts := make(map[string][]string)

	// Get all existing CRQs
	allCRQs, err := v.listAllCRQs(ctx)
	if err != nil {
		return nil, fmt.Errorf("failed to list CRQs: %w", err)
	}

	// For each namespace, check if it would be selected by any existing CRQs (excluding the current one)
	for _, namespaceName := range namespaces {
		// Get the namespace object to check its labels
		ns, err := v.kubernetesClient.CoreV1().Namespaces().Get(ctx, namespaceName, metav1.GetOptions{})
		if err != nil {
			// If namespace doesn't exist, it's not a conflict (it will be created)
			continue
		}

		// Check each CRQ to see if it would select this namespace
		var conflictingCRQNames []string
		for _, existingCRQ := range allCRQs {
			// Skip the CRQ we're currently validating
			if existingCRQ.Name == excludeCRQName {
				continue
			}

			// Check if this CRQ would select this namespace
			matches, err := v.namespaceMatchesCRQ(ns, &existingCRQ)
			if err != nil {
				return nil, fmt.Errorf("failed to check if namespace %s matches CRQ %s: %w",
					namespaceName, existingCRQ.Name, err)
			}

			if matches {
				conflictingCRQNames = append(conflictingCRQNames, existingCRQ.Name)
			}
		}

		if len(conflictingCRQNames) > 0 {
			conflicts[namespaceName] = conflictingCRQNames
		}
	}

	return conflicts, nil
}

// listAllCRQs returns all ClusterResourceQuotas in the cluster using the CRQ client
func (v *NamespaceValidator) listAllCRQs(ctx context.Context) ([]quotav1alpha1.ClusterResourceQuota, error) {
	if v.crqClient == nil {
		return []quotav1alpha1.ClusterResourceQuota{}, nil
	}
	return v.crqClient.ListAllCRQs(ctx)
}

// namespaceMatchesCRQ returns true if the namespace matches the CRQ's selector.
func (v *NamespaceValidator) namespaceMatchesCRQ(
	ns *corev1.Namespace, crq *quotav1alpha1.ClusterResourceQuota,
) (bool, error) {
	if v.crqClient == nil {
		return false, nil
	}
	return v.crqClient.NamespaceMatchesCRQ(ns, crq)
}

func GetSelectedNamespaces(
	ctx context.Context,
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
	return selector.GetSelectedNamespaces(ctx)
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

// ValidateNamespaceAgainstCRQs validates that a namespace doesn't conflict with existing CRQs
// This is used by the namespace webhook to ensure no namespace gets selected by multiple CRQs
func ValidateNamespaceAgainstCRQs(
	ctx context.Context,
	c kubernetes.Interface,
	crqClient *quota.CRQClient,
	namespace *corev1.Namespace,
) error {
	if crqClient == nil {
		return nil // Skip validation if no CRQ client available
	}

	// Get all existing CRQs
	allCRQs, err := crqClient.ListAllCRQs(ctx)
	if err != nil {
		return fmt.Errorf("failed to list CRQs: %w", err)
	}

	// Check which CRQs would select this namespace
	var matchingCRQs []string
	for _, crq := range allCRQs {
		matches, err := crqClient.NamespaceMatchesCRQ(namespace, &crq)
		if err != nil {
			return fmt.Errorf("failed to check if namespace %s matches CRQ %s: %w",
				namespace.Name, crq.Name, err)
		}

		if matches {
			matchingCRQs = append(matchingCRQs, crq.Name)
		}
	}

	// If more than one CRQ would select this namespace, it's a conflict
	if len(matchingCRQs) > 1 {
		return fmt.Errorf("multiple ClusterResourceQuotas select namespace \"%s\": %v",
			namespace.Name, matchingCRQs)
	}

	return nil
}
