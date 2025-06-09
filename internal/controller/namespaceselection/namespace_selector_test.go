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

func TestLabelSelectorNoMatch(t *testing.T) {
	// Create a fake client with our test namespaces
	namespaces := setupFakeNamespaces()
	fakeClient := fake.NewClientBuilder().WithObjects(
		&namespaces[0], &namespaces[1], &namespaces[2], &namespaces[3],
	).Build()

	// Selector that matches nothing
	selector := &metav1.LabelSelector{
		MatchLabels: map[string]string{
			"environment": "doesnotexist",
		},
	}

	namespaceSelector, err := NewLabelBasedNamespaceSelector(fakeClient, selector)
	assert.NoError(t, err, "Failed to create namespace selector")

	selectedNamespaces, err := namespaceSelector.GetSelectedNamespaces(context.Background())
	assert.NoError(t, err, "Failed to get selected namespaces")
	assert.Empty(t, selectedNamespaces, "Expected no namespaces to match")
}

func TestLabelSelectorMultipleLabels(t *testing.T) {
	// Create a fake client with our test namespaces
	namespaces := setupFakeNamespaces()
	fakeClient := fake.NewClientBuilder().WithObjects(
		&namespaces[0], &namespaces[1], &namespaces[2], &namespaces[3],
	).Build()

	// Selector that matches environment=test and team=frontend
	selector := &metav1.LabelSelector{
		MatchLabels: map[string]string{
			"environment": "test",
			"team":        "frontend",
		},
	}

	namespaceSelector, err := NewLabelBasedNamespaceSelector(fakeClient, selector)
	assert.NoError(t, err, "Failed to create namespace selector")

	selectedNamespaces, err := namespaceSelector.GetSelectedNamespaces(context.Background())
	assert.NoError(t, err, "Failed to get selected namespaces")
	assert.ElementsMatch(t, []string{"test-ns1"}, selectedNamespaces)
}

func TestDetermineNamespaceChanges_EmptyPrevious(t *testing.T) {
	// Create a fake client with our test namespaces
	namespaces := setupFakeNamespaces()
	fakeClient := fake.NewClientBuilder().WithObjects(
		&namespaces[0], &namespaces[1], &namespaces[2], &namespaces[3],
	).Build()

	selector := &metav1.LabelSelector{
		MatchLabels: map[string]string{
			"environment": "test",
		},
	}

	namespaceSelector, err := NewLabelBasedNamespaceSelector(fakeClient, selector)
	assert.NoError(t, err, "Failed to create namespace selector")

	// Previous state is empty
	previousNamespaces := []string{}
	added, removed, err := namespaceSelector.DetermineNamespaceChanges(context.Background(), previousNamespaces)
	assert.NoError(t, err, "Failed to determine namespace changes")
	assert.ElementsMatch(t, []string{"test-ns1", "test-ns2"}, added)
	assert.Empty(t, removed)
}

func TestDetermineNamespaceChanges_AllRemoved(t *testing.T) {
	// Create a fake client with our test namespaces
	namespaces := setupFakeNamespaces()
	fakeClient := fake.NewClientBuilder().WithObjects(
		&namespaces[0], &namespaces[1], &namespaces[2], &namespaces[3],
	).Build()

	selector := &metav1.LabelSelector{
		MatchLabels: map[string]string{
			"environment": "doesnotexist",
		},
	}

	namespaceSelector, err := NewLabelBasedNamespaceSelector(fakeClient, selector)
	assert.NoError(t, err, "Failed to create namespace selector")

	// Previous state had all namespaces
	previousNamespaces := []string{"test-ns1", "test-ns2", "prod-ns1", "review-123"}
	added, removed, err := namespaceSelector.DetermineNamespaceChanges(context.Background(), previousNamespaces)
	assert.NoError(t, err, "Failed to determine namespace changes")
	assert.Empty(t, added)
	assert.ElementsMatch(t, previousNamespaces, removed)
}

func TestLabelSelectorInvalidSelector(t *testing.T) {
	fakeClient := fake.NewClientBuilder().Build()
	// Invalid selector (bad label key)
	selector := &metav1.LabelSelector{
		MatchLabels: map[string]string{
			"bad key!": "value",
		},
	}
	_, err := NewLabelBasedNamespaceSelector(fakeClient, selector)
	assert.Error(t, err, "Expected error for invalid label selector")
}
