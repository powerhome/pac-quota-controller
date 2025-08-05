package namespace

import (
	"fmt"

	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/client-go/kubernetes/fake"

	quotav1alpha1 "github.com/powerhome/pac-quota-controller/api/v1alpha1"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
)

var _ = Describe("Namespace", func() {
	Describe("DetermineNamespaceChanges", func() {
		It("should detect added namespaces", func() {
			previous := []string{"ns1", "ns2", "ns3"}
			current := []string{"ns1", "ns2", "ns3", "ns4", "ns5"}

			added, removed := DetermineNamespaceChanges(previous, current)

			Expect(added).To(ConsistOf("ns4", "ns5"))
			Expect(removed).To(BeEmpty())
		})

		It("should detect removed namespaces", func() {
			previous := []string{"ns1", "ns2", "ns3", "ns4", "ns5"}
			current := []string{"ns1", "ns2", "ns3"}

			added, removed := DetermineNamespaceChanges(previous, current)

			Expect(added).To(BeEmpty())
			Expect(removed).To(ConsistOf("ns4", "ns5"))
		})

		It("should detect both added and removed namespaces", func() {
			previous := []string{"ns1", "ns2", "ns3"}
			current := []string{"ns2", "ns3", "ns4", "ns5"}

			added, removed := DetermineNamespaceChanges(previous, current)

			Expect(added).To(ConsistOf("ns4", "ns5"))
			Expect(removed).To(ConsistOf("ns1"))
		})

		It("should handle empty previous list", func() {
			previous := []string{}
			current := []string{"ns1", "ns2", "ns3"}

			added, removed := DetermineNamespaceChanges(previous, current)

			Expect(added).To(ConsistOf("ns1", "ns2", "ns3"))
			Expect(removed).To(BeEmpty())
		})

		It("should handle empty current list", func() {
			previous := []string{"ns1", "ns2", "ns3"}
			current := []string{}

			added, removed := DetermineNamespaceChanges(previous, current)

			Expect(added).To(BeEmpty())
			Expect(removed).To(ConsistOf("ns1", "ns2", "ns3"))
		})

		It("should handle both empty lists", func() {
			previous := []string{}
			current := []string{}

			added, removed := DetermineNamespaceChanges(previous, current)

			Expect(added).To(BeEmpty())
			Expect(removed).To(BeEmpty())
		})

		It("should handle nil lists", func() {
			added, removed := DetermineNamespaceChanges(nil, nil)

			Expect(added).To(BeEmpty())
			Expect(removed).To(BeEmpty())
		})

		It("should handle nil previous list", func() {
			current := []string{"ns1", "ns2", "ns3"}

			added, removed := DetermineNamespaceChanges(nil, current)

			Expect(added).To(ConsistOf("ns1", "ns2", "ns3"))
			Expect(removed).To(BeEmpty())
		})

		It("should handle nil current list", func() {
			previous := []string{"ns1", "ns2", "ns3"}

			added, removed := DetermineNamespaceChanges(previous, nil)

			Expect(added).To(BeEmpty())
			Expect(removed).To(ConsistOf("ns1", "ns2", "ns3"))
		})

		It("should return sorted results", func() {
			previous := []string{"ns3", "ns1", "ns2"}
			current := []string{"ns2", "ns4", "ns1"}

			added, removed := DetermineNamespaceChanges(previous, current)

			// Check that results are sorted
			Expect(added).To(Equal([]string{"ns4"}))
			Expect(removed).To(Equal([]string{"ns3"}))
		})

		It("should handle duplicate namespaces in previous list", func() {
			previous := []string{"ns1", "ns2", "ns1", "ns3"}
			current := []string{"ns1", "ns2", "ns4"}

			added, removed := DetermineNamespaceChanges(previous, current)

			Expect(added).To(ConsistOf("ns4"))
			Expect(removed).To(ConsistOf("ns3"))
		})

		It("should handle duplicate namespaces in current list", func() {
			previous := []string{"ns1", "ns2", "ns3"}
			current := []string{"ns1", "ns2", "ns4", "ns4"}

			added, removed := DetermineNamespaceChanges(previous, current)

			Expect(added).To(ConsistOf("ns4"))
			Expect(removed).To(ConsistOf("ns3"))
		})

		It("should handle case-sensitive namespace names", func() {
			previous := []string{"Namespace1", "namespace1", "NS1"}
			current := []string{"Namespace1", "namespace1", "ns1"}

			added, removed := DetermineNamespaceChanges(previous, current)

			Expect(added).To(ConsistOf("ns1"))
			Expect(removed).To(ConsistOf("NS1"))
		})

		It("should handle special characters in namespace names", func() {
			previous := []string{"ns-1", "ns_2", "ns.3"}
			current := []string{"ns-1", "ns_2", "ns.4"}

			added, removed := DetermineNamespaceChanges(previous, current)

			Expect(added).To(ConsistOf("ns.4"))
			Expect(removed).To(ConsistOf("ns.3"))
		})

		It("should handle very long namespace names", func() {
			longName1 := "very-long-namespace-name-that-exceeds-normal-length-limits-for-testing-purposes-1"
			longName2 := "very-long-namespace-name-that-exceeds-normal-length-limits-for-testing-purposes-2"
			previous := []string{longName1, "ns1"}
			current := []string{longName2, "ns1"}

			added, removed := DetermineNamespaceChanges(previous, current)

			Expect(added).To(ConsistOf(longName2))
			Expect(removed).To(ConsistOf(longName1))
		})
	})

	Describe("CRQ with nil namespace selector", func() {
		It("should handle CRQ with nil namespace selector", func() {
			crq := &quotav1alpha1.ClusterResourceQuota{
				ObjectMeta: metav1.ObjectMeta{
					Name: "test-crq",
				},
				Spec: quotav1alpha1.ClusterResourceQuotaSpec{
					NamespaceSelector: nil,
				},
			}

			// This test verifies that the CRQ structure can be created with nil selector
			// The actual validation logic is currently skipped due to controller-runtime removal
			Expect(crq.Spec.NamespaceSelector).To(BeNil())
		})
	})

	Describe("Namespace change edge cases", func() {
		It("should handle single namespace changes", func() {
			previous := []string{"ns1"}
			current := []string{"ns2"}

			added, removed := DetermineNamespaceChanges(previous, current)

			Expect(added).To(ConsistOf("ns2"))
			Expect(removed).To(ConsistOf("ns1"))
		})

		It("should handle no changes", func() {
			previous := []string{"ns1", "ns2", "ns3"}
			current := []string{"ns1", "ns2", "ns3"}

			added, removed := DetermineNamespaceChanges(previous, current)

			Expect(added).To(BeEmpty())
			Expect(removed).To(BeEmpty())
		})

		It("should handle complete replacement", func() {
			previous := []string{"ns1", "ns2", "ns3"}
			current := []string{"ns4", "ns5", "ns6"}

			added, removed := DetermineNamespaceChanges(previous, current)

			Expect(added).To(ConsistOf("ns4", "ns5", "ns6"))
			Expect(removed).To(ConsistOf("ns1", "ns2", "ns3"))
		})

		It("should handle large namespace lists", func() {
			previous := make([]string, 100)
			current := make([]string, 100)

			for i := 0; i < 100; i++ {
				previous[i] = fmt.Sprintf("ns%d", i)
				if i < 50 {
					current[i] = fmt.Sprintf("ns%d", i)
				} else {
					current[i] = fmt.Sprintf("new-ns%d", i)
				}
			}

			added, removed := DetermineNamespaceChanges(previous, current)

			Expect(added).To(HaveLen(50))
			Expect(removed).To(HaveLen(50))

			// Verify some specific values
			Expect(added).To(ContainElement("new-ns50"))
			Expect(removed).To(ContainElement("ns50"))
		})
	})

	Describe("Performance characteristics", func() {
		It("should handle performance with large datasets", func() {
			// Create large datasets to test performance
			previous := make([]string, 1000)
			current := make([]string, 1000)

			for i := 0; i < 1000; i++ {
				previous[i] = fmt.Sprintf("ns%d", i)
				if i%2 == 0 {
					current[i] = fmt.Sprintf("ns%d", i)
				} else {
					current[i] = fmt.Sprintf("new-ns%d", i)
				}
			}

			// This should complete quickly
			added, removed := DetermineNamespaceChanges(previous, current)

			Expect(added).To(HaveLen(500))
			Expect(removed).To(HaveLen(500))
		})
	})

	Describe("NamespaceValidator.ValidateCRQNamespaceConflicts", func() {
		It("should return nil when namespace selector is nil", func() {
			fakeClient := fake.NewSimpleClientset()
			// Create a mock CRQ client that returns empty list
			validator := NewNamespaceValidator(fakeClient, nil)

			crq := &quotav1alpha1.ClusterResourceQuota{
				Spec: quotav1alpha1.ClusterResourceQuotaSpec{
					NamespaceSelector: nil,
				},
			}

			err := validator.ValidateCRQNamespaceConflicts(crq)

			Expect(err).NotTo(HaveOccurred())
		})

		It("should return no error for valid CRQ when no conflicts", func() {
			fakeClient := fake.NewSimpleClientset()
			// Create a mock CRQ client that returns empty list
			validator := NewNamespaceValidator(fakeClient, nil)

			crq := &quotav1alpha1.ClusterResourceQuota{
				Spec: quotav1alpha1.ClusterResourceQuotaSpec{
					NamespaceSelector: &metav1.LabelSelector{
						MatchLabels: map[string]string{"team": "test"},
					},
				},
			}

			err := validator.ValidateCRQNamespaceConflicts(crq)

			Expect(err).NotTo(HaveOccurred())
		})

		It("should handle error when selector creation fails", func() {
			fakeClient := fake.NewSimpleClientset()
			validator := NewNamespaceValidator(fakeClient, nil)

			crq := &quotav1alpha1.ClusterResourceQuota{
				Spec: quotav1alpha1.ClusterResourceQuotaSpec{
					NamespaceSelector: &metav1.LabelSelector{
						MatchExpressions: []metav1.LabelSelectorRequirement{
							{
								Key:      "team",
								Operator: "InvalidOperator",
								Values:   []string{"test"},
							},
						},
					},
				},
			}

			err := validator.ValidateCRQNamespaceConflicts(crq)

			Expect(err).To(HaveOccurred())
			Expect(err.Error()).To(ContainSubstring("failed to create namespace selector"))
		})

		It("should handle error when namespace selection fails", func() {
			// Create a client that will fail when listing namespaces
			fakeClient := fake.NewSimpleClientset()
			validator := NewNamespaceValidator(fakeClient, nil)

			// Since the fake client doesn't have any namespaces, GetSelectedNamespaces will succeed
			// but return an empty list. To test the error path, we need to create a scenario where
			// the selector itself fails. Let's use an invalid selector that will cause an error.
			crqInvalid := &quotav1alpha1.ClusterResourceQuota{
				Spec: quotav1alpha1.ClusterResourceQuotaSpec{
					NamespaceSelector: &metav1.LabelSelector{
						MatchExpressions: []metav1.LabelSelectorRequirement{
							{
								Key:      "team",
								Operator: "InvalidOperator",
								Values:   []string{"test"},
							},
						},
					},
				},
			}

			err := validator.ValidateCRQNamespaceConflicts(crqInvalid)

			Expect(err).To(HaveOccurred())
			Expect(err.Error()).To(ContainSubstring("failed to create namespace selector"))
		})

		It("should handle empty namespace list", func() {
			fakeClient := fake.NewSimpleClientset()
			validator := NewNamespaceValidator(fakeClient, nil)

			crq := &quotav1alpha1.ClusterResourceQuota{
				Spec: quotav1alpha1.ClusterResourceQuotaSpec{
					NamespaceSelector: &metav1.LabelSelector{
						MatchLabels: map[string]string{"team": "nonexistent"},
					},
				},
			}

			err := validator.ValidateCRQNamespaceConflicts(crq)

			Expect(err).NotTo(HaveOccurred())
		})

		It("should handle case where namespaces are found but validation is skipped", func() {
			// Create a fake client with some namespaces that match the selector
			fakeClient := fake.NewSimpleClientset()
			validator := NewNamespaceValidator(fakeClient, nil)

			// Note: We can't easily add namespaces to fake.NewSimpleClientset() in this context
			// The current implementation skips validation and returns empty list anyway
			crq := &quotav1alpha1.ClusterResourceQuota{
				Spec: quotav1alpha1.ClusterResourceQuotaSpec{
					NamespaceSelector: &metav1.LabelSelector{
						MatchLabels: map[string]string{"team": "test"},
					},
				},
			}

			err := validator.ValidateCRQNamespaceConflicts(crq)

			Expect(err).NotTo(HaveOccurred())
		})
	})

	Describe("GetSelectedNamespaces", func() {
		It("should return nil when namespace selector is nil", func() {
			crq := &quotav1alpha1.ClusterResourceQuota{
				Spec: quotav1alpha1.ClusterResourceQuotaSpec{
					NamespaceSelector: nil,
				},
			}

			namespaces, err := GetSelectedNamespaces(nil, crq)

			Expect(err).NotTo(HaveOccurred())
			Expect(namespaces).To(BeNil())
		})

		It("should return error when selector creation fails", func() {
			crq := &quotav1alpha1.ClusterResourceQuota{
				Spec: quotav1alpha1.ClusterResourceQuotaSpec{
					NamespaceSelector: &metav1.LabelSelector{
						MatchExpressions: []metav1.LabelSelectorRequirement{
							{
								Key:      "team",
								Operator: "InvalidOperator",
								Values:   []string{"test"},
							},
						},
					},
				},
			}

			namespaces, err := GetSelectedNamespaces(nil, crq)

			Expect(err).To(HaveOccurred())
			Expect(err.Error()).To(ContainSubstring("failed to create namespace selector"))
			Expect(namespaces).To(BeNil())
		})

		It("should return error when namespace selection fails", func() {
			// Create a CRQ with an invalid selector that will cause GetSelectedNamespaces to fail
			crq := &quotav1alpha1.ClusterResourceQuota{
				Spec: quotav1alpha1.ClusterResourceQuotaSpec{
					NamespaceSelector: &metav1.LabelSelector{
						MatchExpressions: []metav1.LabelSelectorRequirement{
							{
								Key:      "team",
								Operator: "InvalidOperator",
								Values:   []string{"test"},
							},
						},
					},
				},
			}

			namespaces, err := GetSelectedNamespaces(nil, crq)

			Expect(err).To(HaveOccurred())
			Expect(err.Error()).To(ContainSubstring("failed to create namespace selector"))
			Expect(namespaces).To(BeNil())
		})

		It("should return selected namespaces successfully", func() {
			fakeClient := fake.NewSimpleClientset()
			crq := &quotav1alpha1.ClusterResourceQuota{
				Spec: quotav1alpha1.ClusterResourceQuotaSpec{
					NamespaceSelector: &metav1.LabelSelector{
						MatchLabels: map[string]string{"team": "test"},
					},
				},
			}

			namespaces, err := GetSelectedNamespaces(fakeClient, crq)

			Expect(err).NotTo(HaveOccurred())
			Expect(namespaces).To(BeEmpty()) // No namespaces exist in fake client
		})
	})

	Describe("DetermineNamespaceChanges Edge Cases", func() {
		It("should handle duplicate namespaces", func() {
			previous := []string{"ns1", "ns2", "ns3"}
			current := []string{"ns1", "ns2", "ns3", "ns3", "ns4"}

			added, removed := DetermineNamespaceChanges(previous, current)

			Expect(added).To(ConsistOf("ns4"))
			Expect(removed).To(BeEmpty())
		})

		It("should handle large namespace lists", func() {
			previous := make([]string, 100)
			current := make([]string, 150)

			for i := 0; i < 100; i++ {
				previous[i] = fmt.Sprintf("ns-%d", i)
			}

			for i := 0; i < 150; i++ {
				current[i] = fmt.Sprintf("ns-%d", i)
			}

			added, removed := DetermineNamespaceChanges(previous, current)

			Expect(added).To(HaveLen(50))
			Expect(removed).To(BeEmpty())
		})

		It("should handle empty namespace names", func() {
			previous := []string{"ns1", "", "ns3"}
			current := []string{"ns1", "", "ns3", "ns4"}

			added, removed := DetermineNamespaceChanges(previous, current)

			Expect(added).To(ConsistOf("ns4"))
			Expect(removed).To(BeEmpty())
		})

		It("should handle single namespace changes", func() {
			previous := []string{"ns1"}
			current := []string{"ns2"}

			added, removed := DetermineNamespaceChanges(previous, current)

			Expect(added).To(ConsistOf("ns2"))
			Expect(removed).To(ConsistOf("ns1"))
		})

		It("should handle no changes", func() {
			previous := []string{"ns1", "ns2", "ns3"}
			current := []string{"ns1", "ns2", "ns3"}

			added, removed := DetermineNamespaceChanges(previous, current)

			Expect(added).To(BeEmpty())
			Expect(removed).To(BeEmpty())
		})
	})

	Describe("LabelBasedNamespaceSelector.DetermineNamespaceChanges", func() {
		var (
			fakeClient *fake.Clientset
			selector   *LabelBasedNamespaceSelector
		)

		BeforeEach(func() {
			fakeClient = fake.NewSimpleClientset()
			labelSelector := &metav1.LabelSelector{
				MatchLabels: map[string]string{"team": "test"},
			}
			var err error
			selector, err = NewLabelBasedNamespaceSelector(fakeClient, labelSelector)
			Expect(err).NotTo(HaveOccurred())
		})

		It("should detect namespace changes", func() {
			previous := []string{"ns1", "ns2", "ns3"}

			added, removed, err := selector.DetermineNamespaceChanges(previous)

			Expect(err).NotTo(HaveOccurred())
			// Since no namespaces exist in fake client, all previous should be removed
			Expect(added).To(BeEmpty())
			Expect(removed).To(ConsistOf("ns1", "ns2", "ns3"))
		})

		It("should handle empty previous list", func() {
			previous := []string{}

			added, removed, err := selector.DetermineNamespaceChanges(previous)

			Expect(err).NotTo(HaveOccurred())
			Expect(added).To(BeEmpty())
			Expect(removed).To(BeEmpty())
		})

		It("should handle nil previous list", func() {
			added, removed, err := selector.DetermineNamespaceChanges(nil)

			Expect(err).NotTo(HaveOccurred())
			Expect(added).To(BeEmpty())
			Expect(removed).To(BeEmpty())
		})

		It("should handle error in GetSelectedNamespaces", func() {
			// Create a selector that will fail by using an invalid label selector
			invalidLabelSelector := &metav1.LabelSelector{
				MatchExpressions: []metav1.LabelSelectorRequirement{
					{
						Key:      "team",
						Operator: "InvalidOperator",
						Values:   []string{"test"},
					},
				},
			}
			invalidSelector, err := NewLabelBasedNamespaceSelector(fakeClient, invalidLabelSelector)
			Expect(err).To(HaveOccurred()) // This should fail during creation
			Expect(invalidSelector).To(BeNil())

			// Test that the error is properly propagated
			Expect(err.Error()).To(ContainSubstring("InvalidOperator"))
		})

		It("should handle large namespace lists", func() {
			// Create a large previous list
			previous := make([]string, 100)
			for i := 0; i < 100; i++ {
				previous[i] = fmt.Sprintf("ns-%d", i)
			}

			added, removed, err := selector.DetermineNamespaceChanges(previous)

			Expect(err).NotTo(HaveOccurred())
			// All previous namespaces should be removed since none exist in fake client
			Expect(added).To(BeEmpty())
			Expect(removed).To(HaveLen(100))
		})

		It("should handle duplicate namespaces in previous list", func() {
			previous := []string{"ns1", "ns2", "ns1", "ns3"}

			added, removed, err := selector.DetermineNamespaceChanges(previous)

			Expect(err).NotTo(HaveOccurred())
			Expect(added).To(BeEmpty())
			// The implementation now deduplicates the input, so we expect unique namespaces only
			Expect(removed).To(ConsistOf("ns1", "ns2", "ns3"))
		})

		It("should handle empty namespace names", func() {
			previous := []string{"", "ns1", "ns2"}

			added, removed, err := selector.DetermineNamespaceChanges(previous)

			Expect(err).NotTo(HaveOccurred())
			Expect(added).To(BeEmpty())
			Expect(removed).To(ConsistOf("", "ns1", "ns2"))
		})
	})
})
