package namespaceselection

import (
	"context"
	"testing"

	"github.com/stretchr/testify/assert"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
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
					"environment": "prod",
					"team":        "frontend",
				},
			},
		},
		{
			ObjectMeta: metav1.ObjectMeta{
				Name: "review-123",
				Labels: map[string]string{
					"environment": "review",
					"team":        "frontend",
				},
			},
		},
	}
}

func TestLabelSelectorMatchingOnly(t *testing.T) {
	// Create a fake client with our test namespaces
	namespaces := setupFakeNamespaces()
	fakeClient := fake.NewClientBuilder().WithObjects(
		&namespaces[0], &namespaces[1], &namespaces[2], &namespaces[3],
	).Build()

	// Create a selector for the "environment=test" label
	selector := &metav1.LabelSelector{
		MatchLabels: map[string]string{
			"environment": "test",
		},
	}

	// Create our namespace selector
	namespaceSelector, err := NewLabelBasedNamespaceSelector(fakeClient, selector)
	assert.NoError(t, err, "Failed to create namespace selector")

	// Get the selected namespaces
	selectedNamespaces, err := namespaceSelector.GetSelectedNamespaces(context.Background())
	assert.NoError(t, err, "Failed to get selected namespaces")
	assert.ElementsMatch(t, []string{"test-ns1", "test-ns2"}, selectedNamespaces)
}

func TestDetermineNamespaceChanges(t *testing.T) {
	// Create a fake client with our test namespaces
	namespaces := setupFakeNamespaces()
	fakeClient := fake.NewClientBuilder().WithObjects(
		&namespaces[0], &namespaces[1], &namespaces[2], &namespaces[3],
	).Build()

	// Create a selector for the "environment=test" label
	selector := &metav1.LabelSelector{
		MatchLabels: map[string]string{
			"environment": "test",
		},
	}

	// Create our namespace selector
	namespaceSelector, err := NewLabelBasedNamespaceSelector(fakeClient, selector)
	assert.NoError(t, err, "Failed to create namespace selector")

	// Previous state included test-ns1 and prod-ns1
	previousNamespaces := []string{"test-ns1", "prod-ns1"}

	// Current state should be test-ns1 and test-ns2 based on the label selector
	added, removed, err := namespaceSelector.DetermineNamespaceChanges(context.Background(), previousNamespaces)
	assert.NoError(t, err, "Failed to determine namespace changes")

	// test-ns2 should be added, prod-ns1 should be removed
	assert.ElementsMatch(t, []string{"test-ns2"}, added)
	assert.ElementsMatch(t, []string{"prod-ns1"}, removed)
}

func TestGetNamespacesFromAnnotation(t *testing.T) {
	testCases := []struct {
		name        string
		annotations map[string]string
		expected    []string
	}{
		{
			name:        "nil annotations",
			annotations: nil,
			expected:    nil,
		},
		{
			name:        "empty annotations",
			annotations: map[string]string{},
			expected:    nil,
		},
		{
			name: "with namespaces",
			annotations: map[string]string{
				"quota.powerapp.cloud/namespaces": "ns1,ns2,ns3",
			},
			expected: []string{"ns1", "ns2", "ns3"},
		},
		{
			name: "with spaces",
			annotations: map[string]string{
				"quota.powerapp.cloud/namespaces": "ns1, ns2, ns3",
			},
			expected: []string{"ns1", "ns2", "ns3"},
		},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			result := GetNamespacesFromAnnotation(tc.annotations)
			assert.Equal(t, tc.expected, result)
		})
	}
}
