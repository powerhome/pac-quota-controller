package selector

import (
	"context"
	"testing"

	"github.com/stretchr/testify/assert"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/client/fake"
)

func setupFakeNamespaces() []corev1.Namespace {
	return []corev1.Namespace{
		{
			ObjectMeta: metav1.ObjectMeta{
				Name: "test-ns1",
				Labels: map[string]string{
					"environment": "test",
					"team":        "frontend",
				},
			},
		},
		{
			ObjectMeta: metav1.ObjectMeta{
				Name: "test-ns2",
				Labels: map[string]string{
					"environment": "test",
					"team":        "backend",
				},
			},
		},
		{
			ObjectMeta: metav1.ObjectMeta{
				Name: "prod-ns1",
				Labels: map[string]string{
					"environment": "production",
					"team":        "frontend",
				},
			},
		},
		{
			ObjectMeta: metav1.ObjectMeta{
				Name: "prod-ns2",
				Labels: map[string]string{
					"environment": "production",
					"team":        "backend",
				},
			},
		},
		{
			ObjectMeta: metav1.ObjectMeta{
				Name: "dev-ns",
				Labels: map[string]string{
					"environment": "development",
				},
			},
		},
		{
			ObjectMeta: metav1.ObjectMeta{
				Name:   "terminating-ns",
				Labels: map[string]string{"environment": "test"},
			},
			Status: corev1.NamespaceStatus{
				Phase: corev1.NamespaceTerminating,
			},
		},
	}
}

func TestLabelBasedNamespaceSelector_GetSelectedNamespaces(t *testing.T) {
	tests := []struct {
		name          string
		labelSelector *metav1.LabelSelector
		expected      []string
	}{
		{
			name: "Select by label selector",
			labelSelector: &metav1.LabelSelector{
				MatchLabels: map[string]string{
					"environment": "test",
				},
			},
			expected: []string{"test-ns1", "test-ns2"},
		},
		{
			name: "Should skip terminating namespaces",
			labelSelector: &metav1.LabelSelector{
				MatchLabels: map[string]string{
					"environment": "test",
				},
			},
			expected: []string{"test-ns1", "test-ns2"},
		},
		{
			name: "Non-existent namespace (wrong label)",
			labelSelector: &metav1.LabelSelector{
				MatchLabels: map[string]string{
					"non-existent-label": "test",
				},
			},
			expected: []string{},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			// Setup
			fakeClient := fake.NewClientBuilder().WithObjects(objectsFromNamespaces(setupFakeNamespaces())...).Build()
			selector := NewLabelBasedNamespaceSelector(fakeClient, tt.labelSelector)

			// Execute
			actual, err := selector.GetSelectedNamespaces(context.Background())

			// Assert
			assert.NoError(t, err)
			assert.ElementsMatch(t, tt.expected, actual)
		})
	}
}

// TestLabelBasedNamespaceSelector_GetSelectedNamespaces_Errors tests error handling in GetSelectedNamespaces
func TestLabelBasedNamespaceSelector_GetSelectedNamespaces_Errors(t *testing.T) {
	tests := []struct {
		name          string
		labelSelector *metav1.LabelSelector
		expectError   bool
		errorContains string
	}{
		{
			name: "Invalid label selector",
			labelSelector: &metav1.LabelSelector{
				MatchExpressions: []metav1.LabelSelectorRequirement{
					{
						Key:      "environment",
						Operator: "InvalidOperator", // This will cause an error
						Values:   []string{"test"},
					},
				},
			},
			expectError:   true,
			errorContains: "failed to get namespaces by label selector",
		},
		{
			name:          "Nil label selector",
			labelSelector: nil,
			expectError:   false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			// Setup
			fakeClient := fake.NewClientBuilder().WithObjects(objectsFromNamespaces(setupFakeNamespaces())...).Build()
			selector := NewLabelBasedNamespaceSelector(fakeClient, tt.labelSelector)

			// Execute
			namespaces, err := selector.GetSelectedNamespaces(context.Background())

			// Assert
			if tt.expectError {
				assert.Error(t, err)
				if tt.errorContains != "" {
					assert.Contains(t, err.Error(), tt.errorContains)
				}
			} else {
				assert.NoError(t, err)
				if tt.labelSelector == nil {
					assert.Empty(t, namespaces)
				}
			}
		})
	}
}

func TestLabelBasedNamespaceSelector_DetermineNamespaceChanges(t *testing.T) {
	tests := []struct {
		name               string
		labelSelector      *metav1.LabelSelector
		previousNamespaces []string
		expectedAdded      []string
		expectedRemoved    []string
	}{
		{
			name: "Added namespaces",
			labelSelector: &metav1.LabelSelector{
				MatchLabels: map[string]string{
					"environment": "test",
				},
			},
			previousNamespaces: []string{"test-ns1"},
			expectedAdded:      []string{"test-ns2"},
			expectedRemoved:    []string{},
		},
		{
			name: "Removed namespaces",
			labelSelector: &metav1.LabelSelector{
				MatchLabels: map[string]string{
					"environment": "test",
				},
			},
			previousNamespaces: []string{"test-ns1", "test-ns2", "other-ns"},
			expectedAdded:      []string{},
			expectedRemoved:    []string{"other-ns"},
		},
		{
			name: "Both added and removed",
			labelSelector: &metav1.LabelSelector{
				MatchLabels: map[string]string{
					"environment": "production",
				},
			},
			previousNamespaces: []string{"prod-ns1", "old-ns"},
			expectedAdded:      []string{"prod-ns2"},
			expectedRemoved:    []string{"old-ns"},
		},
		{
			name:               "No changes",
			labelSelector:      nil,
			previousNamespaces: []string{},
			expectedAdded:      []string{},
			expectedRemoved:    []string{},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			// Setup
			fakeClient := fake.NewClientBuilder().WithObjects(objectsFromNamespaces(setupFakeNamespaces())...).Build()
			selector := NewLabelBasedNamespaceSelector(fakeClient, tt.labelSelector)

			// Execute
			added, removed, err := selector.DetermineNamespaceChanges(context.Background(), tt.previousNamespaces)

			// Assert
			assert.NoError(t, err)
			assert.ElementsMatch(t, tt.expectedAdded, added)
			assert.ElementsMatch(t, tt.expectedRemoved, removed)
		})
	}
}

// TestLabelBasedNamespaceSelector_DetermineNamespaceChanges_Errors tests error handling in DetermineNamespaceChanges
func TestLabelBasedNamespaceSelector_DetermineNamespaceChanges_Errors(t *testing.T) {
	tests := []struct {
		name               string
		labelSelector      *metav1.LabelSelector
		previousNamespaces []string
		expectError        bool
		errorContains      string
	}{
		{
			name: "Error getting current namespaces",
			labelSelector: &metav1.LabelSelector{
				MatchExpressions: []metav1.LabelSelectorRequirement{
					{
						Key:      "environment",
						Operator: "InvalidOperator", // This will cause an error
						Values:   []string{"test"},
					},
				},
			},
			previousNamespaces: []string{"test-ns1"},
			expectError:        true,
			errorContains:      "failed to get current selected namespaces",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			// Setup
			fakeClient := fake.NewClientBuilder().WithObjects(objectsFromNamespaces(setupFakeNamespaces())...).Build()
			selector := NewLabelBasedNamespaceSelector(fakeClient, tt.labelSelector)

			// Execute
			added, removed, err := selector.DetermineNamespaceChanges(context.Background(), tt.previousNamespaces)

			// Assert
			if tt.expectError {
				assert.Error(t, err)
				assert.Nil(t, added)
				assert.Nil(t, removed)
				if tt.errorContains != "" {
					assert.Contains(t, err.Error(), tt.errorContains)
				}
			} else {
				assert.NoError(t, err)
			}
		})
	}
}

// Convert []corev1.Namespace to []client.Object
func objectsFromNamespaces(namespaces []corev1.Namespace) []client.Object {
	result := make([]client.Object, len(namespaces))
	for i := range namespaces {
		result[i] = &namespaces[i]
	}
	return result
}

// TestLabelBasedNamespaceSelector_namespaceExists tests the namespaceExists function
func TestLabelBasedNamespaceSelector_namespaceExists(t *testing.T) {
	tests := []struct {
		name           string
		namespaceName  string
		setupNamespace *corev1.Namespace
		expectError    bool
		errorContains  string
	}{
		{
			name:          "Namespace exists",
			namespaceName: "existing-ns",
			setupNamespace: &corev1.Namespace{
				ObjectMeta: metav1.ObjectMeta{
					Name: "existing-ns",
				},
			},
			expectError: false,
		},
		{
			name:          "Namespace does not exist",
			namespaceName: "non-existing-ns",
			setupNamespace: &corev1.Namespace{
				ObjectMeta: metav1.ObjectMeta{
					Name: "other-ns",
				},
			},
			expectError:   true,
			errorContains: "does not exist",
		},
		{
			name:          "Namespace is terminating",
			namespaceName: "terminating-ns",
			setupNamespace: &corev1.Namespace{
				ObjectMeta: metav1.ObjectMeta{
					Name: "terminating-ns",
				},
				Status: corev1.NamespaceStatus{
					Phase: corev1.NamespaceTerminating,
				},
			},
			expectError:   true,
			errorContains: "is in terminating state",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			// Setup
			fakeClient := fake.NewClientBuilder().WithObjects(tt.setupNamespace).Build()
			selector := NewLabelBasedNamespaceSelector(fakeClient, nil)

			// Execute
			err := selector.namespaceExists(context.Background(), tt.namespaceName)

			// Assert
			if tt.expectError {
				assert.Error(t, err)
				assert.Contains(t, err.Error(), tt.errorContains)
			} else {
				assert.NoError(t, err)
			}
		})
	}
}

// TestLabelBasedNamespaceSelector_getNamespacesByLabelSelector tests the getNamespacesByLabelSelector function
func TestLabelBasedNamespaceSelector_getNamespacesByLabelSelector(t *testing.T) {
	tests := []struct {
		name           string
		labelSelector  *metav1.LabelSelector
		namespaces     []corev1.Namespace
		expectedResult []string
		expectError    bool
		errorContains  string
	}{
		{
			name: "Valid label selector",
			labelSelector: &metav1.LabelSelector{
				MatchLabels: map[string]string{
					"environment": "test",
				},
			},
			namespaces:     setupFakeNamespaces(),
			expectedResult: []string{"test-ns1", "test-ns2"},
			expectError:    false,
		},
		{
			name: "Invalid label selector",
			labelSelector: &metav1.LabelSelector{
				MatchExpressions: []metav1.LabelSelectorRequirement{
					{
						Key:      "environment",
						Operator: "InvalidOperator", // This will cause an error
						Values:   []string{"test"},
					},
				},
			},
			namespaces:    setupFakeNamespaces(),
			expectError:   true,
			errorContains: "invalid label selector",
		},
		{
			name: "Skip terminating namespaces",
			labelSelector: &metav1.LabelSelector{
				MatchLabels: map[string]string{
					"environment": "test",
				},
			},
			namespaces:     setupFakeNamespaces(),
			expectedResult: []string{"test-ns1", "test-ns2"},
			expectError:    false,
		},
		{
			name: "No matching namespaces",
			labelSelector: &metav1.LabelSelector{
				MatchLabels: map[string]string{
					"environment": "non-existent",
				},
			},
			namespaces:     setupFakeNamespaces(),
			expectedResult: []string{},
			expectError:    false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			// Setup
			fakeClient := fake.NewClientBuilder().WithObjects(objectsFromNamespaces(tt.namespaces)...).Build()
			selector := NewLabelBasedNamespaceSelector(fakeClient, tt.labelSelector)

			// Execute
			result, err := selector.getNamespacesByLabelSelector(context.Background())

			// Assert
			if tt.expectError {
				assert.Error(t, err)
				if tt.errorContains != "" {
					assert.Contains(t, err.Error(), tt.errorContains)
				}
			} else {
				assert.NoError(t, err)
				assert.ElementsMatch(t, tt.expectedResult, result)
			}
		})
	}
}
