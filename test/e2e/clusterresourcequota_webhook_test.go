package e2e

import (
	"time"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	quotav1alpha1 "github.com/powerhome/pac-quota-controller/api/v1alpha1"
	testutils "github.com/powerhome/pac-quota-controller/test/utils"
	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/resource"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"
)

var _ = Describe("ClusterResourceQuota Webhook", func() {
	var (
		suffix  string
		crqName string
	)

	BeforeEach(func() {
		suffix = testutils.GenerateTestSuffix()
		crqName = testutils.GenerateResourceName("test-crq")
	})

	Context("Create scenarios", func() {
		It("should create a ClusterResourceQuota with valid spec", func() {
			By("Creating a ClusterResourceQuota with valid spec")
			crq, err := testutils.CreateClusterResourceQuota(ctx, k8sClient, crqName, &metav1.LabelSelector{
				MatchLabels: map[string]string{"quota": "limited-" + suffix},
			}, quotav1alpha1.ResourceList{
				corev1.ResourcePods:   resource.MustParse("10"),
				corev1.ResourceCPU:    resource.MustParse("2"),
				corev1.ResourceMemory: resource.MustParse("4Gi"),
			})
			Expect(err).ToNot(HaveOccurred())

			By("Cleaning up the ClusterResourceQuota")
			Expect(k8sClient.Delete(ctx, crq)).To(Succeed())
		})
	})
	Context("Update scenarios", func() {
		It("should update a ClusterResourceQuota spec successfully", func() {
			By("Creating a ClusterResourceQuota with initial spec")
			_, err := testutils.CreateClusterResourceQuota(
				ctx,
				k8sClient, crqName,
				&metav1.LabelSelector{
					MatchLabels: map[string]string{"quota": "limited-" + suffix},
				},
				quotav1alpha1.ResourceList{
					corev1.ResourcePods:   resource.MustParse("10"),
					corev1.ResourceCPU:    resource.MustParse("2"),
					corev1.ResourceMemory: resource.MustParse("4Gi"),
				})
			Expect(err).ToNot(HaveOccurred())
			time.Sleep(2 * time.Second) // Ensure CRQ is updated by reconciliation

			By("Updating the ClusterResourceQuota spec")
			err = testutils.UpdateClusterResourceQuotaSpec(
				ctx, k8sClient, crqName,
				func(crq *quotav1alpha1.ClusterResourceQuota) error {
					crq.Spec.Hard = quotav1alpha1.ResourceList{
						corev1.ResourcePods: resource.MustParse("10"),
						corev1.ResourceCPU:  resource.MustParse("4"),
					}
					return nil
				})
			Expect(err).ToNot(HaveOccurred())

			By("Cleaning up the ClusterResourceQuota")
			latestCRQ := &quotav1alpha1.ClusterResourceQuota{}
			Expect(k8sClient.Get(ctx, types.NamespacedName{Name: crqName}, latestCRQ)).To(Succeed())
			Expect(k8sClient.Delete(ctx, latestCRQ)).To(Succeed())
		})
	})

	Context("Edge cases", func() {
		It("should deny creation when multiple CRQs match the same namespace", func() {
			By("Creating a namespace that matches the selector")
			namespace, err := testutils.CreateNamespace(
				ctx, k8sClient, "test-namespace-"+suffix,
				map[string]string{"quota": "limited-" + suffix},
			)
			Expect(err).ToNot(HaveOccurred())

			By("Creating the first ClusterResourceQuota")
			crq1, err := testutils.CreateClusterResourceQuota(ctx, k8sClient, "crq1-"+suffix, &metav1.LabelSelector{
				MatchLabels: map[string]string{"quota": "limited-" + suffix},
			}, quotav1alpha1.ResourceList{
				corev1.ResourcePods: resource.MustParse("10"),
			})
			Expect(err).ToNot(HaveOccurred())

			By("Waiting for the first CRQ to be reconciled and have status.namespaces populated")
			Eventually(func() bool {
				updatedCRQ := &quotav1alpha1.ClusterResourceQuota{}
				if err := k8sClient.Get(ctx, types.NamespacedName{Name: crq1.Name}, updatedCRQ); err != nil {
					return false
				}
				// Check if the namespace is in the status
				for _, nsStatus := range updatedCRQ.Status.Namespaces {
					if nsStatus.Namespace == namespace.Name {
						return true
					}
				}
				return false
			}, time.Second*30, time.Second*1).Should(BeTrue(), "First CRQ should have the namespace in its status")

			By("Attempting to create a second ClusterResourceQuota with overlapping namespace selector")
			_, err = testutils.CreateClusterResourceQuota(ctx, k8sClient, "crq2-"+suffix, &metav1.LabelSelector{
				MatchLabels: map[string]string{"quota": "limited-" + suffix},
			}, quotav1alpha1.ResourceList{
				corev1.ResourcePods: resource.MustParse("20"),
			})
			Expect(err).To(HaveOccurred())
			Expect(err.Error()).To(ContainSubstring("namespace ownership conflict"))

			By("Cleaning up resources")
			Expect(k8sClient.Delete(ctx, crq1)).To(Succeed())
			Expect(k8sClient.Delete(ctx, namespace)).To(Succeed())
		})
	})
})
