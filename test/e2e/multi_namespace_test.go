package e2e

import (
	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"

	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/resource"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"

	quotav1alpha1 "github.com/powerhome/pac-quota-controller/api/v1alpha1"
	testutils "github.com/powerhome/pac-quota-controller/test/utils"
)

var _ = Describe("ClusterResourceQuota multi-namespace aggregation", func() {
	var (
		suffix string
		team   string
		crq    *quotav1alpha1.ClusterResourceQuota
		ns1    *corev1.Namespace
		ns2    *corev1.Namespace
	)

	BeforeEach(func() {
		suffix = testutils.GenerateTestSuffix()
		team = "multi-" + suffix

		var err error
		ns1, err = testutils.CreateNamespace(ctx, k8sClient, "multi-ns1-"+suffix, map[string]string{"team": team})
		Expect(err).NotTo(HaveOccurred())
		ns2, err = testutils.CreateNamespace(ctx, k8sClient, "multi-ns2-"+suffix, map[string]string{"team": team})
		Expect(err).NotTo(HaveOccurred())

		crq, err = testutils.CreateClusterResourceQuota(ctx, k8sClient, "multi-crq-"+suffix,
			&metav1.LabelSelector{MatchLabels: map[string]string{"team": team}},
			quotav1alpha1.ResourceList{
				corev1.ResourceRequestsCPU: resource.MustParse("10"),
				corev1.ResourcePods:        resource.MustParse("10"),
			})
		Expect(err).NotTo(HaveOccurred())
	})

	AfterEach(func() {
		_ = k8sClient.Delete(ctx, crq)
		_ = k8sClient.Delete(ctx, ns1)
		_ = k8sClient.Delete(ctx, ns2)
	})

	It("sums usage across all selected namespaces into the CRQ total", func() {
		p1, err := testutils.CreatePod(ctx, k8sClient, ns1.Name, "pod1-"+suffix,
			corev1.ResourceList{corev1.ResourceCPU: resource.MustParse("500m")}, nil)
		Expect(err).NotTo(HaveOccurred())
		DeferCleanup(func() { _ = k8sClient.Delete(ctx, p1) })

		p2, err := testutils.CreatePod(ctx, k8sClient, ns2.Name, "pod2-"+suffix,
			corev1.ResourceList{corev1.ResourceCPU: resource.MustParse("300m")}, nil)
		Expect(err).NotTo(HaveOccurred())
		DeferCleanup(func() { _ = k8sClient.Delete(ctx, p2) })

		By("aggregating usage from both namespaces into status.total")
		Eventually(func() error {
			usage := testutils.GetRefreshedCRQStatusUsage(ctx, k8sClient, crq.Name)
			return testutils.ExpectCRQUsageToMatch(usage, map[string]string{
				"requests.cpu": "800m", // 500m (ns1) + 300m (ns2)
				"pods":         "2",
			})
		}, Timeout, Interval).Should(Succeed())

		By("reporting both namespaces in status")
		Eventually(func() []string {
			return testutils.GetRefreshedCRQStatusNamespaces(ctx, k8sClient, crq.Name)
		}, Timeout, Interval).Should(ContainElements(ns1.Name, ns2.Name))
	})
})
