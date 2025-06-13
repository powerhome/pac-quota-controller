package kubernetes_test

import (
	"context"
	"fmt"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	quotav1alpha1 "github.com/powerhome/pac-quota-controller/api/v1alpha1"
	"github.com/powerhome/pac-quota-controller/pkg/kubernetes"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/client-go/kubernetes/scheme"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/client/fake"
)

var _ = Describe("Namespace Utils", func() {
	var (
		ctx       context.Context
		k8sClient client.Client
		sch       *runtime.Scheme
	)

	BeforeEach(func() {
		ctx = context.Background()
		sch = scheme.Scheme
		Expect(quotav1alpha1.AddToScheme(sch)).To(Succeed())
		Expect(corev1.AddToScheme(sch)).To(Succeed())
	})

	Describe("ValidateNamespaceOwnershipWithAPI", func() {
		var (
			crq   *quotav1alpha1.ClusterResourceQuota
			nsOne *corev1.Namespace
			nsTwo *corev1.Namespace
		)

		BeforeEach(func() {
			nsOne = &corev1.Namespace{
				ObjectMeta: metav1.ObjectMeta{
					Name:   "ns-one",
					Labels: map[string]string{"labelkey1": "labelvalue1"},
				},
			}
			nsTwo = &corev1.Namespace{
				ObjectMeta: metav1.ObjectMeta{
					Name:   "ns-two",
					Labels: map[string]string{"labelkey2": "labelvalue2"},
				},
			}

			crq = &quotav1alpha1.ClusterResourceQuota{
				ObjectMeta: metav1.ObjectMeta{Name: "test-crq"},
				Spec: quotav1alpha1.ClusterResourceQuotaSpec{
					NamespaceSelector: &metav1.LabelSelector{
						MatchLabels: map[string]string{"labelkey1": "labelvalue1"},
					},
				},
			}
		})

		Context("when CRQ has no namespace selector", func() {
			It("should return no warnings and no error", func() {
				crq.Spec.NamespaceSelector = nil
				k8sClient = fake.NewClientBuilder().WithScheme(sch).Build()
				warnings, err := kubernetes.ValidateNamespaceOwnershipWithAPI(ctx, k8sClient, crq)
				Expect(err).NotTo(HaveOccurred())
				Expect(warnings).To(BeEmpty())
			})
		})

		Context("when namespace selector selects no namespaces", func() {
			It("should return no warnings and no error", func() {
				crq.Spec.NamespaceSelector = &metav1.LabelSelector{
					MatchLabels: map[string]string{"labelkey1": "nonexistentvalue"},
				}
				k8sClient = fake.NewClientBuilder().WithScheme(sch).WithObjects(nsOne).Build()
				warnings, err := kubernetes.ValidateNamespaceOwnershipWithAPI(ctx, k8sClient, crq)
				Expect(err).NotTo(HaveOccurred())
				Expect(warnings).To(BeEmpty())
			})
		})

		Context("when namespaces are selected and no other CRQs exist", func() {
			It("should return no warnings and no error", func() {
				k8sClient = fake.NewClientBuilder().WithScheme(sch).WithObjects(nsOne).Build()
				warnings, err := kubernetes.ValidateNamespaceOwnershipWithAPI(ctx, k8sClient, crq)
				Expect(err).NotTo(HaveOccurred())
				Expect(warnings).To(BeEmpty())
			})
		})

		Context("when namespaces are selected and other CRQs exist with no conflicts", func() {
			It("should return no warnings and no error", func() {
				otherCRQ := &quotav1alpha1.ClusterResourceQuota{
					ObjectMeta: metav1.ObjectMeta{Name: "other-crq"},
					Spec: quotav1alpha1.ClusterResourceQuotaSpec{
						NamespaceSelector: &metav1.LabelSelector{MatchLabels: map[string]string{"labelkey2": "labelvalue2"}},
					},
					Status: quotav1alpha1.ClusterResourceQuotaStatus{
						Namespaces: []quotav1alpha1.ResourceQuotaStatusByNamespace{
							{
								Namespace: "ns-two",
								Status:    quotav1alpha1.ResourceQuotaStatus{},
							},
						},
					},
				}
				k8sClient = fake.NewClientBuilder().WithScheme(sch).WithObjects(nsOne, nsTwo, otherCRQ).Build()
				warnings, err := kubernetes.ValidateNamespaceOwnershipWithAPI(ctx, k8sClient, crq)
				Expect(err).NotTo(HaveOccurred())
				Expect(warnings).To(BeEmpty())
			})
		})

		Context("when a selected namespace is already owned by another CRQ", func() {
			It("should return an error", func() {
				otherCRQ := &quotav1alpha1.ClusterResourceQuota{
					ObjectMeta: metav1.ObjectMeta{Name: "other-crq"},
					Spec: quotav1alpha1.ClusterResourceQuotaSpec{
						NamespaceSelector: &metav1.LabelSelector{
							MatchLabels: map[string]string{"labelkey1": "labelvalue1"},
						},
					},
					Status: quotav1alpha1.ClusterResourceQuotaStatus{
						Namespaces: []quotav1alpha1.ResourceQuotaStatusByNamespace{
							{
								Namespace: "ns-one", // otherCRQ claims ns-one
								Status:    quotav1alpha1.ResourceQuotaStatus{},
							},
						},
					},
				}
				k8sClient = fake.NewClientBuilder().WithScheme(sch).WithObjects(nsOne, otherCRQ).Build()
				warnings, err := kubernetes.ValidateNamespaceOwnershipWithAPI(ctx, k8sClient, crq) // crq wants ns-one
				Expect(err).To(HaveOccurred())
				Expect(err.Error()).To(ContainSubstring(
					"namespace 'ns-one' is already owned by another ClusterResourceQuota 'other-crq'",
				),
				)
				Expect(warnings).To(BeEmpty())
			})
		})

		Context("when multiple selected namespaces are already owned by another CRQ", func() {
			It("should return an error listing all conflicting namespaces", func() {
				nsOneExtra := &corev1.Namespace{
					ObjectMeta: metav1.ObjectMeta{
						Name:   "ns-one-extra",
						Labels: map[string]string{"labelkey1": "labelvalue1", "env": "test"}, // Also selected by crq
					},
				}
				crq.Spec.NamespaceSelector = &metav1.LabelSelector{
					MatchLabels: map[string]string{"labelkey1": "labelvalue1"}, // Selects ns-one and ns-one-extra
				}
				otherCRQ := &quotav1alpha1.ClusterResourceQuota{
					ObjectMeta: metav1.ObjectMeta{Name: "other-crq"},
					// Spec.NamespaceSelector is not strictly needed here as Status is the source of truth for ownership
					Status: quotav1alpha1.ClusterResourceQuotaStatus{
						Namespaces: []quotav1alpha1.ResourceQuotaStatusByNamespace{
							{
								Namespace: "ns-one", // otherCRQ claims ns-one
								Status:    quotav1alpha1.ResourceQuotaStatus{},
							},
							{
								Namespace: "ns-one-extra", // otherCRQ claims ns-one-extra
								Status:    quotav1alpha1.ResourceQuotaStatus{},
							},
						},
					},
				}
				k8sClient = fake.NewClientBuilder().WithScheme(sch).WithObjects(nsOne, nsOneExtra, otherCRQ).Build()
				warnings, err := kubernetes.ValidateNamespaceOwnershipWithAPI(ctx, k8sClient, crq)
				Expect(err).To(HaveOccurred())
				Expect(err.Error()).To(ContainSubstring(
					"namespace 'ns-one' is already owned by another ClusterResourceQuota 'other-crq'",
				),
				)
				Expect(err.Error()).To(ContainSubstring(
					"namespace 'ns-one-extra' is already owned by another ClusterResourceQuota 'other-crq'",
				),
				)
				Expect(warnings).To(BeEmpty())
			})
		})
	})

	Describe("GetSelectedNamespaces", func() {
		var (
			crq *quotav1alpha1.ClusterResourceQuota
			nsA *corev1.Namespace
			nsB *corev1.Namespace
		)

		BeforeEach(func() {
			nsA = &corev1.Namespace{ // Corresponds to original "another-blue" due to naming for sort order
				ObjectMeta: metav1.ObjectMeta{
					Name:   "ns-a", // Alphabetically first
					Labels: map[string]string{"app": "my-app", "env": "prod"},
				},
			}
			nsB = &corev1.Namespace{ // Corresponds to original "blue"
				ObjectMeta: metav1.ObjectMeta{
					Name:   "ns-b", // Alphabetically second
					Labels: map[string]string{"app": "my-app", "env": "staging"},
				},
			}
			// nsC is for other tests, not selected by default app:my-app selector
			nsC := &corev1.Namespace{
				ObjectMeta: metav1.ObjectMeta{
					Name:   "ns-c",
					Labels: map[string]string{"app": "other-app", "env": "dev"},
				},
			}
			// Initialize k8sClient here as it's used in multiple contexts
			k8sClient = fake.NewClientBuilder().WithScheme(sch).WithObjects(nsA, nsB, nsC).Build()

			crq = &quotav1alpha1.ClusterResourceQuota{
				ObjectMeta: metav1.ObjectMeta{Name: "test-crq"},
			}
		})

		Context("when CRQ has no namespace selector", func() {
			It("should return nil and no error", func() {
				crq.Spec.NamespaceSelector = nil
				// k8sClient already initialized with no specific objects needed for this case beyond scheme
				selected, err := kubernetes.GetSelectedNamespaces(ctx, k8sClient, crq)
				Expect(err).NotTo(HaveOccurred())
				Expect(selected).To(BeNil())
			})
		})

		Context("when namespace selector matches specific namespaces", func() {
			It("should return the names of the matched namespaces, sorted", func() {
				// nsA (ns-a) and nsB (ns-b) both have "app: my-app"
				crq.Spec.NamespaceSelector = &metav1.LabelSelector{MatchLabels: map[string]string{"app": "my-app"}}
				// k8sClient already initialized with nsA, nsB, nsC

				selected, err := kubernetes.GetSelectedNamespaces(ctx, k8sClient, crq)
				Expect(err).NotTo(HaveOccurred())
				Expect(selected).To(ConsistOf("ns-a", "ns-b"))
				Expect(selected).To(Equal([]string{"ns-a", "ns-b"})) // Sorted alphabetically
			})
		})

		Context("when namespace selector uses MatchExpressions", func() {
			It("should return the names of the matched namespaces, sorted", func() {
				// nsA (app:my-app, env:prod)
				// nsB (app:my-app, env:staging)
				// nsD (app:my-app, env:dev) - new namespace for this test
				nsD := &corev1.Namespace{
					ObjectMeta: metav1.ObjectMeta{
						Name:   "ns-d",
						Labels: map[string]string{"app": "my-app", "env": "dev"},
					},
				}
				k8sClient = fake.NewClientBuilder().WithScheme(sch).WithObjects(nsA, nsB, nsD).Build()

				crq.Spec.NamespaceSelector = &metav1.LabelSelector{
					MatchExpressions: []metav1.LabelSelectorRequirement{
						{Key: "app", Operator: metav1.LabelSelectorOpIn, Values: []string{"my-app"}},
						{Key: "env", Operator: metav1.LabelSelectorOpNotIn, Values: []string{"prod"}},
					},
				} // Should match ns-b (env:staging) and ns-d (env:dev), but not ns-a (env:prod)
				selected, err := kubernetes.GetSelectedNamespaces(ctx, k8sClient, crq)
				Expect(err).NotTo(HaveOccurred())
				Expect(selected).To(ConsistOf("ns-b", "ns-d"))
				Expect(selected).To(Equal([]string{"ns-b", "ns-d"})) // Sorted
			})
		})

		Context("when namespace selector matches no namespaces", func() {
			It("should return an empty slice and no error", func() {
				crq.Spec.NamespaceSelector = &metav1.LabelSelector{MatchLabels: map[string]string{"app": "nonexistent-app"}}
				// k8sClient already initialized with nsA, nsB, nsC
				selected, err := kubernetes.GetSelectedNamespaces(ctx, k8sClient, crq)
				Expect(err).NotTo(HaveOccurred())
				Expect(selected).To(BeEmpty())
			})
		})

		Context("when an invalid namespace selector is provided", func() {
			It("should return an error", func() {
				crq.Spec.NamespaceSelector = &metav1.LabelSelector{
					MatchExpressions: []metav1.LabelSelectorRequirement{
						{Key: "app", Operator: "InvalidOperator", Values: []string{"my-app"}},
					},
				}
				// k8sClient already initialized
				_, err := kubernetes.GetSelectedNamespaces(ctx, k8sClient, crq)
				Expect(err).To(HaveOccurred())
				Expect(err.Error()).To(ContainSubstring("failed to create namespace selector"))
			})
		})
	})

	Describe("DetermineNamespaceChanges", func() {
		type testCase struct {
			description     string
			previous        []string
			current         []string
			expectedAdded   []string
			expectedRemoved []string
		}

		testCases := []testCase{
			{
				description:     "no changes",
				previous:        []string{"ns1", "ns2"},
				current:         []string{"ns1", "ns2"},
				expectedAdded:   []string{},
				expectedRemoved: []string{},
			},
			{
				description:     "namespaces added",
				previous:        []string{"ns1"},
				current:         []string{"ns1", "ns2", "ns3"},
				expectedAdded:   []string{"ns2", "ns3"},
				expectedRemoved: []string{},
			},
			{
				description:     "namespaces removed",
				previous:        []string{"ns1", "ns2", "ns3"},
				current:         []string{"ns1"},
				expectedAdded:   []string{},
				expectedRemoved: []string{"ns2", "ns3"},
			},
			{
				description:     "namespaces added and removed",
				previous:        []string{"ns1", "ns2"},
				current:         []string{"ns2", "ns3"},
				expectedAdded:   []string{"ns3"},
				expectedRemoved: []string{"ns1"},
			},
			{
				description:     "empty previous, non-empty current",
				previous:        []string{},
				current:         []string{"ns1", "ns2"},
				expectedAdded:   []string{"ns1", "ns2"},
				expectedRemoved: []string{},
			},
			{
				description:     "non-empty previous, empty current",
				previous:        []string{"ns1", "ns2"},
				current:         []string{},
				expectedAdded:   []string{},
				expectedRemoved: []string{"ns1", "ns2"},
			},
			{
				description:     "both empty",
				previous:        []string{},
				current:         []string{},
				expectedAdded:   []string{},
				expectedRemoved: []string{},
			},
			{
				description:     "unsorted input, sorted output",
				previous:        []string{"c", "a"},
				current:         []string{"b", "a"},
				expectedAdded:   []string{"b"},
				expectedRemoved: []string{"c"},
			},
		}

		for _, tc := range testCases {
			It(fmt.Sprintf("should correctly determine changes when %s", tc.description), func() {
				added, removed := kubernetes.DetermineNamespaceChanges(tc.previous, tc.current)
				if len(tc.expectedAdded) == 0 {
					Expect(added).To(BeEmpty())
				} else {
					Expect(added).To(Equal(tc.expectedAdded))
				}
				if len(tc.expectedRemoved) == 0 {
					Expect(removed).To(BeEmpty())
				} else {
					Expect(removed).To(Equal(tc.expectedRemoved))
				}
			})
		}
	})
})

// MockNamespaceSelector is a utility for testing GetSelectedNamespaces
// when direct mocking of the selector logic is needed.
// However, for these tests, using the fake client and actual LabelSelector logic is preferred.
type MockNamespaceSelector struct {
	NamespacesToReturn []string
	ErrorToReturn      error
	Client             client.Client
	Selector           *metav1.LabelSelector
}

func (m *MockNamespaceSelector) GetSelectedNamespaces(ctx context.Context) ([]string, error) {
	if m.ErrorToReturn != nil {
		return nil, m.ErrorToReturn
	}
	if m.Selector != nil && m.Client != nil { // Fallback to actual logic if needed for some tests
		labelSelector, err := metav1.LabelSelectorAsSelector(m.Selector)
		if err != nil {
			return nil, err
		}
		nsList := &corev1.NamespaceList{}
		if err := m.Client.List(ctx, nsList, client.MatchingLabelsSelector{Selector: labelSelector}); err != nil {
			return nil, err
		}
		var selected []string
		for _, ns := range nsList.Items {
			selected = append(selected, ns.Name)
		}
		// The actual implementation sorts, so we should too if mimicking it.
		// sort.Strings(selected)
		return selected, nil
	}
	return m.NamespacesToReturn, nil
}
