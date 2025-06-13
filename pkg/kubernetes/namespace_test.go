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
			crq     *quotav1alpha1.ClusterResourceQuota
			nsBlue  *corev1.Namespace
			nsGreen *corev1.Namespace
		)

		BeforeEach(func() {
			nsBlue = &corev1.Namespace{ObjectMeta: metav1.ObjectMeta{Name: "blue", Labels: map[string]string{"color": "blue"}}}
			nsGreen = &corev1.Namespace{ObjectMeta: metav1.ObjectMeta{Name: "green", Labels: map[string]string{"color": "green"}}}

			crq = &quotav1alpha1.ClusterResourceQuota{
				ObjectMeta: metav1.ObjectMeta{Name: "test-crq"},
				Spec: quotav1alpha1.ClusterResourceQuotaSpec{
					NamespaceSelector: &metav1.LabelSelector{
						MatchLabels: map[string]string{"color": "blue"},
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
				crq.Spec.NamespaceSelector = &metav1.LabelSelector{MatchLabels: map[string]string{"color": "nonexistent"}}
				k8sClient = fake.NewClientBuilder().WithScheme(sch).WithObjects(nsBlue).Build() // nsBlue exists but won't be selected
				warnings, err := kubernetes.ValidateNamespaceOwnershipWithAPI(ctx, k8sClient, crq)
				Expect(err).NotTo(HaveOccurred())
				Expect(warnings).To(BeEmpty())
			})
		})

		Context("when namespaces are selected and no other CRQs exist", func() {
			It("should return no warnings and no error", func() {
				k8sClient = fake.NewClientBuilder().WithScheme(sch).WithObjects(nsBlue).Build()
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
						NamespaceSelector: &metav1.LabelSelector{MatchLabels: map[string]string{"color": "green"}},
					},
					Status: quotav1alpha1.ClusterResourceQuotaStatus{
						Namespaces: []quotav1alpha1.ResourceQuotaStatusByNamespace{
							{
								Namespace: "green",
								Status:    quotav1alpha1.ResourceQuotaStatus{},
							},
						},
					},
				}
				k8sClient = fake.NewClientBuilder().WithScheme(sch).WithObjects(nsBlue, nsGreen, otherCRQ).Build()
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
						NamespaceSelector: &metav1.LabelSelector{MatchLabels: map[string]string{"color": "blue"}}, // This would also select 'blue'
					},
					Status: quotav1alpha1.ClusterResourceQuotaStatus{
						Namespaces: []quotav1alpha1.ResourceQuotaStatusByNamespace{
							{
								Namespace: "blue",
								Status:    quotav1alpha1.ResourceQuotaStatus{},
							},
						},
					},
				}
				k8sClient = fake.NewClientBuilder().WithScheme(sch).WithObjects(nsBlue, otherCRQ).Build()
				warnings, err := kubernetes.ValidateNamespaceOwnershipWithAPI(ctx, k8sClient, crq)
				Expect(err).To(HaveOccurred())
				Expect(err.Error()).To(ContainSubstring("namespace 'blue' is already owned by another ClusterResourceQuota"))
				Expect(warnings).To(BeEmpty())
			})
		})

		Context("when multiple selected namespaces are already owned by another CRQ", func() {
			It("should return an error listing all conflicting namespaces", func() {
				nsBlueGreen := &corev1.Namespace{ObjectMeta: metav1.ObjectMeta{Name: "bluegreen", Labels: map[string]string{"color": "blue", "env": "test"}}}
				crq.Spec.NamespaceSelector = &metav1.LabelSelector{
					MatchLabels: map[string]string{"color": "blue"}, // Selects blue and bluegreen
				}
				otherCRQ := &quotav1alpha1.ClusterResourceQuota{
					ObjectMeta: metav1.ObjectMeta{Name: "other-crq"},
					Status: quotav1alpha1.ClusterResourceQuotaStatus{
						Namespaces: []quotav1alpha1.ResourceQuotaStatusByNamespace{
							{
								Namespace: "blue",
								Status:    quotav1alpha1.ResourceQuotaStatus{},
							},
							{
								Namespace: "bluegreen",
								Status:    quotav1alpha1.ResourceQuotaStatus{},
							},
						},
					},
				}
				k8sClient = fake.NewClientBuilder().WithScheme(sch).WithObjects(nsBlue, nsBlueGreen, otherCRQ).Build()
				warnings, err := kubernetes.ValidateNamespaceOwnershipWithAPI(ctx, k8sClient, crq)
				Expect(err).To(HaveOccurred())
				Expect(err.Error()).To(ContainSubstring("namespace 'blue' is already owned by another ClusterResourceQuota"))
				Expect(err.Error()).To(ContainSubstring("namespace 'bluegreen' is already owned by another ClusterResourceQuota"))
				Expect(warnings).To(BeEmpty())
			})
		})
	})

	Describe("GetSelectedNamespaces", func() {
		var (
			crq    *quotav1alpha1.ClusterResourceQuota
			nsBlue *corev1.Namespace
			nsRed  *corev1.Namespace
		)

		BeforeEach(func() {
			nsBlue = &corev1.Namespace{ObjectMeta: metav1.ObjectMeta{Name: "blue", Labels: map[string]string{"color": "blue", "env": "prod"}}}
			nsRed = &corev1.Namespace{ObjectMeta: metav1.ObjectMeta{Name: "red", Labels: map[string]string{"color": "red", "env": "dev"}}}
			crq = &quotav1alpha1.ClusterResourceQuota{
				ObjectMeta: metav1.ObjectMeta{Name: "test-crq"},
			}
		})

		Context("when CRQ has no namespace selector", func() {
			It("should return nil and no error", func() {
				crq.Spec.NamespaceSelector = nil
				k8sClient = fake.NewClientBuilder().WithScheme(sch).Build()
				selected, err := kubernetes.GetSelectedNamespaces(ctx, k8sClient, crq)
				Expect(err).NotTo(HaveOccurred())
				Expect(selected).To(BeNil())
			})
		})

		Context("when namespace selector matches specific namespaces", func() {
			It("should return the names of the matched namespaces, sorted", func() {
				nsAnotherBlue := &corev1.Namespace{ObjectMeta: metav1.ObjectMeta{Name: "another-blue", Labels: map[string]string{"color": "blue", "env": "staging"}}}
				crq.Spec.NamespaceSelector = &metav1.LabelSelector{MatchLabels: map[string]string{"color": "blue"}}
				k8sClient = fake.NewClientBuilder().WithScheme(sch).WithObjects(nsBlue, nsRed, nsAnotherBlue).Build()

				selected, err := kubernetes.GetSelectedNamespaces(ctx, k8sClient, crq)
				Expect(err).NotTo(HaveOccurred())
				Expect(selected).To(ConsistOf("blue", "another-blue"))       // Order doesn't matter for ConsistOf, but the function sorts it.
				Expect(selected).To(Equal([]string{"another-blue", "blue"})) // Check sorted order
			})
		})

		Context("when namespace selector uses MatchExpressions", func() {
			It("should return the names of the matched namespaces, sorted", func() {
				nsBlueDev := &corev1.Namespace{ObjectMeta: metav1.ObjectMeta{Name: "blue-dev", Labels: map[string]string{"color": "blue", "env": "dev"}}}
				crq.Spec.NamespaceSelector = &metav1.LabelSelector{
					MatchExpressions: []metav1.LabelSelectorRequirement{
						{Key: "color", Operator: metav1.LabelSelectorOpIn, Values: []string{"blue"}},
						{Key: "env", Operator: metav1.LabelSelectorOpNotIn, Values: []string{"prod"}},
					},
				} // Should match blue-dev (color:blue, env:dev) but not nsBlue (color:blue, env:prod)
				k8sClient = fake.NewClientBuilder().WithScheme(sch).WithObjects(nsBlue, nsRed, nsBlueDev).Build()
				selected, err := kubernetes.GetSelectedNamespaces(ctx, k8sClient, crq)
				Expect(err).NotTo(HaveOccurred())
				Expect(selected).To(ConsistOf("blue-dev"))
				Expect(selected).To(Equal([]string{"blue-dev"}))
			})
		})

		Context("when namespace selector matches no namespaces", func() {
			It("should return an empty slice and no error", func() {
				crq.Spec.NamespaceSelector = &metav1.LabelSelector{MatchLabels: map[string]string{"color": "nonexistent"}}
				k8sClient = fake.NewClientBuilder().WithScheme(sch).WithObjects(nsBlue, nsRed).Build()
				selected, err := kubernetes.GetSelectedNamespaces(ctx, k8sClient, crq)
				Expect(err).NotTo(HaveOccurred())
				Expect(selected).To(BeEmpty())
			})
		})

		Context("when an invalid namespace selector is provided", func() {
			It("should return an error", func() {
				crq.Spec.NamespaceSelector = &metav1.LabelSelector{
					MatchExpressions: []metav1.LabelSelectorRequirement{
						{Key: "color", Operator: "InvalidOperator", Values: []string{"blue"}},
					},
				}
				k8sClient = fake.NewClientBuilder().WithScheme(sch).WithObjects(nsBlue).Build()
				_, err := kubernetes.GetSelectedNamespaces(ctx, k8sClient, crq)
				Expect(err).To(HaveOccurred())
				Expect(err.Error()).To(ContainSubstring("failed to create namespace selector"))
				// The underlying error from metav1.LabelSelectorAsSelector will be something like:
				// "operator: Invalid value: "InvalidOperator": not a valid selector operator"
				// We check for our wrapped error message.
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
			tc := tc // Capture range variable
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

// MockNamespaceSelector is a utility for testing GetSelectedNamespaces when direct mocking of the selector logic is needed.
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
