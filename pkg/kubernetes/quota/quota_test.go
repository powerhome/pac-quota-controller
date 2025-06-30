package quota

import (
	"context"
	"testing"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	quotav1alpha1 "github.com/powerhome/pac-quota-controller/api/v1alpha1"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/client/fake"
)

func TestQuota(t *testing.T) {
	RegisterFailHandler(Fail)
	RunSpecs(t, "Quota Package Suite")
}

var _ = Describe("CRQClient", func() {
	var (
		ctx       context.Context
		k8sClient client.Client
		crqClient *CRQClient
		sch       *runtime.Scheme
		crq1      *quotav1alpha1.ClusterResourceQuota
		crq2      *quotav1alpha1.ClusterResourceQuota
		nsDev     *corev1.Namespace
		nsProd    *corev1.Namespace
		nsTest    *corev1.Namespace
	)

	BeforeEach(func() {
		ctx = context.Background()
		sch = runtime.NewScheme()
		Expect(corev1.AddToScheme(sch)).To(Succeed())
		Expect(quotav1alpha1.AddToScheme(sch)).To(Succeed())

		nsDev = &corev1.Namespace{
			ObjectMeta: metav1.ObjectMeta{Name: "dev", Labels: map[string]string{"env": "development"}},
		}
		nsProd = &corev1.Namespace{
			ObjectMeta: metav1.ObjectMeta{Name: "prod", Labels: map[string]string{"env": "production"}},
		}
		nsTest = &corev1.Namespace{
			ObjectMeta: metav1.ObjectMeta{Name: "test", Labels: map[string]string{"env": "testing"}},
		}

		crq1 = &quotav1alpha1.ClusterResourceQuota{
			ObjectMeta: metav1.ObjectMeta{Name: "crq-dev"},
			Spec: quotav1alpha1.ClusterResourceQuotaSpec{
				NamespaceSelector: &metav1.LabelSelector{
					MatchLabels: map[string]string{"env": "development"},
				},
			},
			Status: quotav1alpha1.ClusterResourceQuotaStatus{
				Namespaces: []quotav1alpha1.ResourceQuotaStatusByNamespace{
					{
						Namespace: "dev1",
						Status:    quotav1alpha1.ResourceQuotaStatus{},
					},
					{
						Namespace: "dev2",
						Status:    quotav1alpha1.ResourceQuotaStatus{},
					},
				},
			},
		}
		crq2 = &quotav1alpha1.ClusterResourceQuota{
			ObjectMeta: metav1.ObjectMeta{Name: "crq-prod"},
			Spec: quotav1alpha1.ClusterResourceQuotaSpec{
				NamespaceSelector: &metav1.LabelSelector{
					MatchLabels: map[string]string{"env": "production"},
				},
			},
			Status: quotav1alpha1.ClusterResourceQuotaStatus{
				Namespaces: []quotav1alpha1.ResourceQuotaStatusByNamespace{
					{
						Namespace: "prod1",
						Status:    quotav1alpha1.ResourceQuotaStatus{},
					},
				},
			},
		}
	})

	JustBeforeEach(func() {
		crqClient = NewCRQClient(k8sClient)
	})

	Describe("ListAllCRQs", func() {
		Context("when CRQs exist", func() {
			BeforeEach(func() {
				k8sClient = fake.NewClientBuilder().WithScheme(sch).WithObjects(crq1, crq2).Build()
			})
			It("should return all CRQs", func() {
				crqs, err := crqClient.ListAllCRQs(ctx)
				Expect(err).NotTo(HaveOccurred())
				Expect(crqs).To(HaveLen(2))
				Expect(crqs).To(ConsistOf(*crq1, *crq2))
			})
		})
		Context("when no CRQs exist", func() {
			BeforeEach(func() {
				k8sClient = fake.NewClientBuilder().WithScheme(sch).Build()
			})
			It("should return an empty list", func() {
				crqs, err := crqClient.ListAllCRQs(ctx)
				Expect(err).NotTo(HaveOccurred())
				Expect(crqs).To(BeEmpty())
			})
		})
		// Error case for c.Client.List is hard to test with fake client without specific error injection.
	})

	Describe("NamespaceMatchesCRQ", func() {
		BeforeEach(func() {
			// k8sClient is not strictly needed for this method as it doesn't make API calls
			k8sClient = fake.NewClientBuilder().WithScheme(sch).Build()
		})

		Context("when CRQ has no namespace selector", func() {
			It("should return false", func() {
				crqNoSelector := crq1.DeepCopy()
				crqNoSelector.Spec.NamespaceSelector = nil
				matches, err := crqClient.NamespaceMatchesCRQ(nsDev, crqNoSelector)
				Expect(err).NotTo(HaveOccurred())
				Expect(matches).To(BeFalse())
			})
		})

		Context("when namespace labels match the CRQ selector", func() {
			It("should return true", func() {
				matches, err := crqClient.NamespaceMatchesCRQ(nsDev, crq1)
				Expect(err).NotTo(HaveOccurred())
				Expect(matches).To(BeTrue())
			})
		})

		Context("when namespace labels do not match the CRQ selector", func() {
			It("should return false", func() {
				matches, err := crqClient.NamespaceMatchesCRQ(nsProd, crq1) // nsProd (env:production) vs crq1 (env:development)
				Expect(err).NotTo(HaveOccurred())
				Expect(matches).To(BeFalse())
			})
		})

		Context("when namespace has no labels", func() {
			It("should return false if selector requires labels", func() {
				nsNoLabels := &corev1.Namespace{ObjectMeta: metav1.ObjectMeta{Name: "nolabels"}}
				matches, err := crqClient.NamespaceMatchesCRQ(nsNoLabels, crq1)
				Expect(err).NotTo(HaveOccurred())
				Expect(matches).To(BeFalse())
			})
		})

		Context("when CRQ selector is invalid", func() {
			It("should return an error", func() {
				crqInvalidSelector := crq1.DeepCopy()
				crqInvalidSelector.Spec.NamespaceSelector = &metav1.LabelSelector{
					MatchExpressions: []metav1.LabelSelectorRequirement{
						{Key: "env", Operator: "InvalidOperator", Values: []string{"development"}},
					},
				}
				_, err := crqClient.NamespaceMatchesCRQ(nsDev, crqInvalidSelector)
				Expect(err).To(HaveOccurred())
			})
		})
	})

	Describe("GetCRQByNamespace", func() {
		Context("when namespace matches exactly one CRQ", func() {
			BeforeEach(func() {
				k8sClient = fake.NewClientBuilder().WithScheme(sch).WithObjects(crq1, crq2, nsDev, nsProd).Build()
			})
			It("should return the matching CRQ", func() {
				crq, err := crqClient.GetCRQByNamespace(context.Background(), nsDev)
				Expect(err).NotTo(HaveOccurred())
				Expect(crq).NotTo(BeNil())
				Expect(crq.Name).To(Equal("crq-dev"))
			})
		})

		Context("when namespace matches multiple CRQs", func() {
			BeforeEach(func() {
				crqBoth := &quotav1alpha1.ClusterResourceQuota{
					ObjectMeta: metav1.ObjectMeta{Name: "crq-both"},
					Spec: quotav1alpha1.ClusterResourceQuotaSpec{
						NamespaceSelector: &metav1.LabelSelector{
							MatchLabels: map[string]string{"env": "development"}, // Matches nsDev
						},
					},
				}
				k8sClient = fake.NewClientBuilder().WithScheme(sch).WithObjects(crq1, crq2, crqBoth, nsDev, nsProd).Build()
			})
			It("should return an error", func() {
				crq, err := crqClient.GetCRQByNamespace(context.Background(), nsDev)
				Expect(err).To(HaveOccurred())
				Expect(crq).To(BeNil())
				Expect(err.Error()).To(ContainSubstring("multiple ClusterResourceQuotas select namespace"))
				Expect(err.Error()).To(ContainSubstring("crq-dev"))
				Expect(err.Error()).To(ContainSubstring("crq-both"))
			})
		})

		Context("when namespace matches no CRQs", func() {
			BeforeEach(func() {
				k8sClient = fake.NewClientBuilder().WithScheme(sch).WithObjects(crq1, crq2, nsTest).Build()
			})
			It("should return nil without error", func() {
				crq, err := crqClient.GetCRQByNamespace(context.Background(), nsTest)
				Expect(err).NotTo(HaveOccurred())
				Expect(crq).To(BeNil())
			})
		})

		Context("when no CRQs exist in the cluster", func() {
			BeforeEach(func() {
				k8sClient = fake.NewClientBuilder().WithScheme(sch).WithObjects(nsDev).Build()
			})
			It("should return nil without error", func() {
				crq, err := crqClient.GetCRQByNamespace(context.Background(), nsDev)
				Expect(err).NotTo(HaveOccurred())
				Expect(crq).To(BeNil())
			})
		})

		Context("when NamespaceMatchesCRQ returns an error", func() {
			BeforeEach(func() {
				crqInvalidSelector := crq1.DeepCopy()
				crqInvalidSelector.Name = "crq-invalid"
				crqInvalidSelector.Spec.NamespaceSelector = &metav1.LabelSelector{
					MatchExpressions: []metav1.LabelSelectorRequirement{
						{Key: "env", Operator: "InvalidOperator", Values: []string{"development"}},
					},
				}
				k8sClient = fake.NewClientBuilder().WithScheme(sch).WithObjects(crqInvalidSelector, nsDev).Build()
			})
			It("should propagate the error", func() {
				_, err := crqClient.GetCRQByNamespace(context.Background(), nsDev)
				Expect(err).To(HaveOccurred())
			})
		})
	})

	Describe("GetNamespacesFromStatus", func() {
		BeforeEach(func() {
			// k8sClient is not strictly needed for this method
			k8sClient = fake.NewClientBuilder().WithScheme(sch).Build()
		})
		Context("when CRQ status has namespaces", func() {
			It("should return the list of namespace names", func() {
				namespaces := crqClient.GetNamespacesFromStatus(crq1)
				Expect(namespaces).To(Equal([]string{"dev1", "dev2"}))
			})
		})

		Context("when CRQ status has no namespaces (nil)", func() {
			It("should return nil", func() {
				crqNoStatusNs := crq1.DeepCopy()
				crqNoStatusNs.Status.Namespaces = nil
				namespaces := crqClient.GetNamespacesFromStatus(crqNoStatusNs)
				Expect(namespaces).To(BeNil())
			})
		})

		Context("when CRQ status has an empty list of namespaces", func() {
			It("should return an empty slice", func() {
				crqEmptyStatusNs := crq1.DeepCopy()
				crqEmptyStatusNs.Status.Namespaces = []quotav1alpha1.ResourceQuotaStatusByNamespace{}
				namespaces := crqClient.GetNamespacesFromStatus(crqEmptyStatusNs)
				Expect(namespaces).To(BeEmpty())
				Expect(namespaces).NotTo(BeNil())
			})
		})
	})
})
