package namespace

import (
	"context"
	"testing"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"

	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/client/fake"
)

func TestNamespaceSelector(t *testing.T) {
	RegisterFailHandler(Fail)
	RunSpecs(t, "Namespace Selector Package Suite")
}

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

var _ = Describe("LabelBasedNamespaceSelector", func() {
	var (
		ctx        context.Context
		fakeClient client.Client
		namespaces []corev1.Namespace
		scheme     *runtime.Scheme
	)

	BeforeEach(func() {
		ctx = context.Background()
		scheme = runtime.NewScheme()
		Expect(corev1.AddToScheme(scheme)).To(Succeed())

		namespaces = setupFakeNamespaces()
		fakeClient = fake.NewClientBuilder().WithScheme(scheme).WithObjects(
			&namespaces[0], &namespaces[1], &namespaces[2], &namespaces[3],
		).Build()
	})

	It("should match namespaces by environment label", func() {
		selector := &metav1.LabelSelector{
			MatchLabels: map[string]string{
				"environment": "test",
			},
		}

		namespaceSelector, err := NewLabelBasedNamespaceSelector(fakeClient, selector)
		Expect(err).NotTo(HaveOccurred())

		selectedNamespaces, err := namespaceSelector.GetSelectedNamespaces(ctx)
		Expect(err).NotTo(HaveOccurred())
		Expect(selectedNamespaces).To(ConsistOf("test-ns1", "test-ns2"))
	})

	It("should determine namespace changes correctly", func() {
		selector := &metav1.LabelSelector{
			MatchLabels: map[string]string{
				"environment": "test",
			},
		}

		namespaceSelector, err := NewLabelBasedNamespaceSelector(fakeClient, selector)
		Expect(err).NotTo(HaveOccurred())

		previousNamespaces := []string{"test-ns1", "prod-ns1"}
		added, removed, err := namespaceSelector.DetermineNamespaceChanges(ctx, previousNamespaces)
		Expect(err).NotTo(HaveOccurred())
		Expect(added).To(ConsistOf("test-ns2"))
		Expect(removed).To(ConsistOf("prod-ns1"))
	})

	It("should handle no matching namespaces", func() {
		selector := &metav1.LabelSelector{
			MatchLabels: map[string]string{
				"environment": "doesnotexist",
			},
		}

		namespaceSelector, err := NewLabelBasedNamespaceSelector(fakeClient, selector)
		Expect(err).NotTo(HaveOccurred())

		selectedNamespaces, err := namespaceSelector.GetSelectedNamespaces(ctx)
		Expect(err).NotTo(HaveOccurred())
		Expect(selectedNamespaces).To(BeEmpty())
	})

	It("should match multiple labels", func() {
		selector := &metav1.LabelSelector{
			MatchLabels: map[string]string{
				"environment": "test",
				"team":        "frontend",
			},
		}

		namespaceSelector, err := NewLabelBasedNamespaceSelector(fakeClient, selector)
		Expect(err).NotTo(HaveOccurred())

		selectedNamespaces, err := namespaceSelector.GetSelectedNamespaces(ctx)
		Expect(err).NotTo(HaveOccurred())
		Expect(selectedNamespaces).To(ConsistOf("test-ns1"))
	})

	It("should handle empty previous namespaces", func() {
		selector := &metav1.LabelSelector{
			MatchLabels: map[string]string{
				"environment": "test",
			},
		}

		namespaceSelector, err := NewLabelBasedNamespaceSelector(fakeClient, selector)
		Expect(err).NotTo(HaveOccurred())

		previousNamespaces := []string{}
		added, removed, err := namespaceSelector.DetermineNamespaceChanges(ctx, previousNamespaces)
		Expect(err).NotTo(HaveOccurred())
		Expect(added).To(ConsistOf("test-ns1", "test-ns2"))
		Expect(removed).To(BeEmpty())
	})

	It("should handle all namespaces removed", func() {
		selector := &metav1.LabelSelector{
			MatchLabels: map[string]string{
				"environment": "doesnotexist",
			},
		}

		namespaceSelector, err := NewLabelBasedNamespaceSelector(fakeClient, selector)
		Expect(err).NotTo(HaveOccurred())

		previousNamespaces := []string{"test-ns1", "test-ns2", "prod-ns1", "review-123"}
		added, removed, err := namespaceSelector.DetermineNamespaceChanges(ctx, previousNamespaces)
		Expect(err).NotTo(HaveOccurred())
		Expect(added).To(BeEmpty())
		Expect(removed).To(ConsistOf("test-ns1", "test-ns2", "prod-ns1", "review-123"))
	})

	It("should handle invalid selector", func() {
		selector := &metav1.LabelSelector{
			MatchLabels: map[string]string{
				"bad key!": "value",
			},
		}
		_, err := NewLabelBasedNamespaceSelector(fakeClient, selector)
		Expect(err).To(HaveOccurred())
	})

	It("should match using expressions", func() {
		selector := &metav1.LabelSelector{
			MatchExpressions: []metav1.LabelSelectorRequirement{{
				Key:      "team",
				Operator: metav1.LabelSelectorOpIn,
				Values:   []string{"frontend"},
			}},
		}

		namespaceSelector, err := NewLabelBasedNamespaceSelector(fakeClient, selector)
		Expect(err).NotTo(HaveOccurred())

		selectedNamespaces, err := namespaceSelector.GetSelectedNamespaces(ctx)
		Expect(err).NotTo(HaveOccurred())
		Expect(selectedNamespaces).To(ConsistOf("test-ns1", "prod-ns1", "review-123"))
	})

	It("should exclude namespaces without labels", func() {
		noLabelNS := corev1.Namespace{ObjectMeta: metav1.ObjectMeta{Name: "nolabel-ns"}}
		fakeClientWithExtra := fake.NewClientBuilder().WithScheme(scheme).WithObjects(
			&namespaces[0], &namespaces[1], &namespaces[2], &namespaces[3], &noLabelNS,
		).Build()

		selector := &metav1.LabelSelector{MatchLabels: map[string]string{"environment": "test"}}
		namespaceSelector, err := NewLabelBasedNamespaceSelector(fakeClientWithExtra, selector)
		Expect(err).NotTo(HaveOccurred())

		selectedNamespaces, err := namespaceSelector.GetSelectedNamespaces(ctx)
		Expect(err).NotTo(HaveOccurred())
		Expect(selectedNamespaces).NotTo(ContainElement("nolabel-ns"))
	})
})
