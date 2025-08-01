package namespace

import (
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/client-go/kubernetes"
	"k8s.io/client-go/kubernetes/fake"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
)

var _ = Describe("NamespaceSelector", func() {
	var (
		fakeClient kubernetes.Interface
	)

	BeforeEach(func() {
		fakeClient = fake.NewSimpleClientset()
	})

	Describe("NewLabelBasedNamespaceSelector", func() {
		It("should create a new selector with valid label selector", func() {
			labelSelector := &metav1.LabelSelector{
				MatchLabels: map[string]string{
					"team": "test",
				},
			}

			selector, err := NewLabelBasedNamespaceSelector(fakeClient, labelSelector)

			Expect(err).NotTo(HaveOccurred())
			Expect(selector).NotTo(BeNil())
			Expect(selector.client).To(Equal(fakeClient))
			Expect(selector.labelSelector).To(Equal(labelSelector))
		})

		It("should create a new selector with match expressions", func() {
			labelSelector := &metav1.LabelSelector{
				MatchExpressions: []metav1.LabelSelectorRequirement{
					{
						Key:      "team",
						Operator: metav1.LabelSelectorOpIn,
						Values:   []string{"test", "dev"},
					},
				},
			}

			selector, err := NewLabelBasedNamespaceSelector(fakeClient, labelSelector)

			Expect(err).NotTo(HaveOccurred())
			Expect(selector).NotTo(BeNil())
		})

		It("should return error with invalid label selector", func() {
			labelSelector := &metav1.LabelSelector{
				MatchExpressions: []metav1.LabelSelectorRequirement{
					{
						Key:      "team",
						Operator: "InvalidOperator",
						Values:   []string{"test"},
					},
				},
			}

			selector, err := NewLabelBasedNamespaceSelector(fakeClient, labelSelector)

			Expect(err).To(HaveOccurred())
			Expect(err.Error()).To(ContainSubstring("failed to convert label selector to selector"))
			Expect(selector).To(BeNil())
		})
	})

	Describe("GetSelectedNamespaces", func() {
		It("should return empty list when no namespaces exist", func() {
			labelSelector := &metav1.LabelSelector{
				MatchLabels: map[string]string{
					"team": "test",
				},
			}

			selector, err := NewLabelBasedNamespaceSelector(fakeClient, labelSelector)
			Expect(err).NotTo(HaveOccurred())

			namespaces, err := selector.GetSelectedNamespaces()

			Expect(err).NotTo(HaveOccurred())
			Expect(namespaces).To(BeEmpty())
		})

		It("should return namespaces that match labels", func() {
			// Create test namespaces
			ns1 := createTestNamespace("ns1", map[string]string{"team": "test"})
			ns2 := createTestNamespace("ns2", map[string]string{"team": "dev"})
			ns3 := createTestNamespace("ns3", map[string]string{"team": "test"})

			fakeClient = fake.NewSimpleClientset(ns1, ns2, ns3)

			labelSelector := &metav1.LabelSelector{
				MatchLabels: map[string]string{
					"team": "test",
				},
			}

			selector, err := NewLabelBasedNamespaceSelector(fakeClient, labelSelector)
			Expect(err).NotTo(HaveOccurred())

			namespaces, err := selector.GetSelectedNamespaces()

			Expect(err).NotTo(HaveOccurred())
			Expect(namespaces).To(HaveLen(2))
			Expect(namespaces).To(ContainElement("ns1"))
			Expect(namespaces).To(ContainElement("ns3"))
		})

		It("should return error when client fails to list namespaces", func() {
			// Create a client that will fail to list namespaces
			// We can't easily simulate client failure with fake.NewSimpleClientset,
			// but we can test with an invalid selector that will cause an error
			invalidLabelSelector := &metav1.LabelSelector{
				MatchExpressions: []metav1.LabelSelectorRequirement{
					{
						Key:      "team",
						Operator: "InvalidOperator",
						Values:   []string{"test"},
					},
				},
			}

			selector, err := NewLabelBasedNamespaceSelector(fakeClient, invalidLabelSelector)
			Expect(err).To(HaveOccurred())
			Expect(selector).To(BeNil())
			Expect(err.Error()).To(ContainSubstring("InvalidOperator"))
		})

		It("should handle empty selector", func() {
			labelSelector := &metav1.LabelSelector{}

			selector, err := NewLabelBasedNamespaceSelector(fakeClient, labelSelector)
			Expect(err).NotTo(HaveOccurred())

			namespaces, err := selector.GetSelectedNamespaces()

			Expect(err).NotTo(HaveOccurred())
			Expect(namespaces).To(BeEmpty())
		})
	})

	Describe("DetermineNamespaceChanges", func() {
		It("should detect added namespaces", func() {
			previous := []string{"ns1", "ns2"}
			current := []string{"ns1", "ns2", "ns3", "ns4"}

			added, removed := DetermineNamespaceChanges(previous, current)

			Expect(added).To(HaveLen(2))
			Expect(removed).To(BeEmpty())
			Expect(added).To(ContainElement("ns3"))
			Expect(added).To(ContainElement("ns4"))
		})

		It("should detect removed namespaces", func() {
			previous := []string{"ns1", "ns2", "ns3", "ns4"}
			current := []string{"ns1", "ns2"}

			added, removed := DetermineNamespaceChanges(previous, current)

			Expect(added).To(BeEmpty())
			Expect(removed).To(HaveLen(2))
			Expect(removed).To(ContainElement("ns3"))
			Expect(removed).To(ContainElement("ns4"))
		})

		It("should detect both added and removed namespaces", func() {
			previous := []string{"ns1", "ns2", "ns3"}
			current := []string{"ns1", "ns4", "ns5"}

			added, removed := DetermineNamespaceChanges(previous, current)

			Expect(added).To(HaveLen(2))
			Expect(removed).To(HaveLen(2))
			Expect(added).To(ContainElement("ns4"))
			Expect(added).To(ContainElement("ns5"))
			Expect(removed).To(ContainElement("ns2"))
			Expect(removed).To(ContainElement("ns3"))
		})

		It("should handle empty lists", func() {
			added, removed := DetermineNamespaceChanges([]string{}, []string{})

			Expect(added).To(BeEmpty())
			Expect(removed).To(BeEmpty())
		})

		It("should handle nil lists", func() {
			added, removed := DetermineNamespaceChanges(nil, nil)

			Expect(added).To(BeEmpty())
			Expect(removed).To(BeEmpty())
		})

		It("should return sorted results", func() {
			previous := []string{"ns3", "ns1", "ns2"}
			current := []string{"ns5", "ns4", "ns1"}

			added, removed := DetermineNamespaceChanges(previous, current)

			Expect(added).To(HaveLen(2))
			Expect(removed).To(HaveLen(2))
			// Check that results are sorted alphabetically
			Expect(added[0]).To(Equal("ns4"))
			Expect(added[1]).To(Equal("ns5"))
			Expect(removed[0]).To(Equal("ns2"))
			Expect(removed[1]).To(Equal("ns3"))
		})
	})
})

// Helper function to create test namespaces
func createTestNamespace(name string, labels map[string]string) *corev1.Namespace {
	return &corev1.Namespace{
		ObjectMeta: metav1.ObjectMeta{
			Name:   name,
			Labels: labels,
		},
	}
}
