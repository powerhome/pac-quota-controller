package e2e

import (
	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"

	"math/rand"
	"strconv"

	quotav1alpha1 "github.com/powerhome/pac-quota-controller/api/v1alpha1"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	crclient "sigs.k8s.io/controller-runtime/pkg/client"
)

var _ = Describe("ClusterResourceQuota Namespace Ownership Webhook", func() {
	It("should deny creation of a CRQ if a namespace is already owned by another CRQ", func() {
		suffix := strconv.Itoa(rand.Intn(1000000))
		crq1Name := "test-ownership-crq1-" + suffix
		crq2Name := "test-ownership-crq2-" + suffix
		nsName := "test-ownership-ns-" + suffix

		ns := &corev1.Namespace{
			ObjectMeta: metav1.ObjectMeta{
				Name:   nsName,
				Labels: map[string]string{"quota": "limited"},
			},
		}
		Expect(k8sClient.Create(ctx, ns)).To(Succeed())

		crq1 := &quotav1alpha1.ClusterResourceQuota{
			ObjectMeta: metav1.ObjectMeta{Name: crq1Name},
			Spec: quotav1alpha1.ClusterResourceQuotaSpec{
				NamespaceSelector: &metav1.LabelSelector{MatchLabels: map[string]string{"quota": "limited"}},
				Hard:              quotav1alpha1.ResourceList{},
			},
		}
		Expect(k8sClient.Create(ctx, crq1)).To(Succeed())

		// Wait for crq1 status to include the namespace
		Eventually(func(g Gomega) bool {
			updated := &quotav1alpha1.ClusterResourceQuota{}
			err := k8sClient.Get(ctx, crclient.ObjectKey{Name: crq1Name}, updated)
			g.Expect(err).NotTo(HaveOccurred())
			for _, ns := range updated.Status.Namespaces {
				if ns.Namespace == nsName {
					return true
				}
			}
			return false
		}).Should(BeTrue(), "crq1 status should include the namespace before creating crq2")

		crq2 := &quotav1alpha1.ClusterResourceQuota{
			ObjectMeta: metav1.ObjectMeta{Name: crq2Name},
			Spec: quotav1alpha1.ClusterResourceQuotaSpec{
				NamespaceSelector: &metav1.LabelSelector{MatchLabels: map[string]string{"quota": "limited"}},
				Hard:              quotav1alpha1.ResourceList{},
			},
		}
		// Should be denied by webhook
		err := k8sClient.Create(ctx, crq2)
		Expect(err).To(HaveOccurred())
		Expect(err.Error()).To(ContainSubstring("already owned"))

		DeferCleanup(func() {
			_ = k8sClient.Delete(ctx, ns)
			_ = k8sClient.Delete(ctx, crq1)
			_ = k8sClient.Delete(ctx, crq2)
		})
	})

	It("should allow creation of a CRQ if no namespace is owned by another CRQ", func() {
		suffix := strconv.Itoa(rand.Intn(1000000))
		crqName := "test-ownership-crq-" + suffix
		nsName := "test-ownership-ns-" + suffix

		ns := &corev1.Namespace{
			ObjectMeta: metav1.ObjectMeta{
				Name:   nsName,
				Labels: map[string]string{"quota": "unique"},
			},
		}
		Expect(k8sClient.Create(ctx, ns)).To(Succeed())

		crq := &quotav1alpha1.ClusterResourceQuota{
			ObjectMeta: metav1.ObjectMeta{Name: crqName},
			Spec: quotav1alpha1.ClusterResourceQuotaSpec{
				NamespaceSelector: &metav1.LabelSelector{MatchLabels: map[string]string{"quota": "unique"}},
				Hard:              quotav1alpha1.ResourceList{},
			},
		}
		Expect(k8sClient.Create(ctx, crq)).To(Succeed())

		DeferCleanup(func() {
			_ = k8sClient.Delete(ctx, ns)
			_ = k8sClient.Delete(ctx, crq)
		})
	})
})
