package e2e

import (
	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"

	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/resource"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"sigs.k8s.io/controller-runtime/pkg/client"

	quotav1alpha1 "github.com/powerhome/pac-quota-controller/api/v1alpha1"
	testutils "github.com/powerhome/pac-quota-controller/test/utils"
)

var _ = Describe("CRQ Enforcement Mode E2E", func() {
	var suffix, team string

	BeforeEach(func() {
		suffix = testutils.GenerateTestSuffix()
		team = "enf-" + suffix
	})

	It("admits a request in ReportOnly mode and still records the violation", func() {
		ns, err := testutils.CreateNamespace(ctx, k8sClient, "enf-ns-"+suffix, map[string]string{"team": team})
		Expect(err).NotTo(HaveOccurred())
		DeferCleanup(func() { _ = k8sClient.Delete(ctx, ns) })

		crq, err := testutils.CreateClusterResourceQuota(ctx, k8sClient, "enf-crq-"+suffix,
			&metav1.LabelSelector{MatchLabels: map[string]string{"team": team}},
			quotav1alpha1.ResourceList{corev1.ResourcePods: resource.MustParse("0")})
		Expect(err).NotTo(HaveOccurred())
		DeferCleanup(func() { _ = k8sClient.Delete(ctx, crq) })

		Expect(testutils.WaitForCRQResourceUsage(
			ctx, k8sClient, crq.Name, corev1.ResourcePods, resource.MustParse("0"),
		)).To(Succeed())

		By("switching the CRQ to ReportOnly")
		crq.Spec.EnforcementMode = quotav1alpha1.EnforcementModeReportOnly
		Expect(k8sClient.Update(ctx, crq)).To(Succeed())

		By("admitting a pod that would exceed the pod-count limit instead of denying it")
		pod, err := testutils.CreatePod(ctx, k8sClient, ns.Name, "enf-p1-"+suffix, nil, nil)
		Expect(err).NotTo(HaveOccurred())
		DeferCleanup(func() { _ = k8sClient.Delete(ctx, pod) })

		By("recording QuotaExceeded for the over-limit resource")
		Eventually(func() []string {
			return crqEventReasons(crq.Name)
		}, Timeout, Interval).Should(ContainElement("QuotaExceeded"))

		By("reflecting the real over-limit usage in status")
		Expect(testutils.WaitForCRQResourceUsage(
			ctx, k8sClient, crq.Name, corev1.ResourcePods, resource.MustParse("1"),
		)).To(Succeed())
	})

	It("keeps blocking a CRQ with no mode set, then switches to ReportOnly without recreation", func() {
		ns, err := testutils.CreateNamespace(ctx, k8sClient, "enf-up-ns-"+suffix, map[string]string{"team": team})
		Expect(err).NotTo(HaveOccurred())
		DeferCleanup(func() { _ = k8sClient.Delete(ctx, ns) })

		crq, err := testutils.CreateClusterResourceQuota(ctx, k8sClient, "enf-up-crq-"+suffix,
			&metav1.LabelSelector{MatchLabels: map[string]string{"team": team}},
			quotav1alpha1.ResourceList{corev1.ResourcePods: resource.MustParse("0")})
		Expect(err).NotTo(HaveOccurred())
		DeferCleanup(func() { _ = k8sClient.Delete(ctx, crq) })
		Expect(crq.Spec.EnforcementMode).To(BeEmpty(), "mode should be unset, matching a pre-upgrade CRQ")

		Expect(testutils.WaitForCRQResourceUsage(
			ctx, k8sClient, crq.Name, corev1.ResourcePods, resource.MustParse("0"),
		)).To(Succeed())

		By("denying pod creation exactly as today, with mode left unset")
		denyErr := testutils.EventuallyDenied(ctx, k8sClient, func() (client.Object, error) {
			return testutils.CreatePod(ctx, k8sClient, ns.Name, "enf-up-p1-"+suffix, nil, nil)
		})
		Expect(denyErr).To(HaveOccurred())

		By("switching to ReportOnly on the same CRQ, without recreating it")
		Expect(k8sClient.Get(ctx, client.ObjectKey{Name: crq.Name}, crq)).To(Succeed())
		crq.Spec.EnforcementMode = quotav1alpha1.EnforcementModeReportOnly
		Expect(k8sClient.Update(ctx, crq)).To(Succeed())

		By("admitting the same request once ReportOnly takes effect")
		pod, err := testutils.CreatePod(ctx, k8sClient, ns.Name, "enf-up-p2-"+suffix, nil, nil)
		Expect(err).NotTo(HaveOccurred())
		DeferCleanup(func() { _ = k8sClient.Delete(ctx, pod) })
	})
})
