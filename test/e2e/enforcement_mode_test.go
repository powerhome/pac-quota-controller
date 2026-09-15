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

		// checkQuotaThresholds skips a zero hard limit entirely (never emits QuotaExceeded
		// for it), so use "1" here rather than "0" to actually exercise event recording.
		crq, err := testutils.CreateClusterResourceQuota(ctx, k8sClient, "enf-crq-"+suffix,
			&metav1.LabelSelector{MatchLabels: map[string]string{"team": team}},
			quotav1alpha1.ResourceList{corev1.ResourcePods: resource.MustParse("1")})
		Expect(err).NotTo(HaveOccurred())
		DeferCleanup(func() { _ = k8sClient.Delete(ctx, crq) })

		Expect(testutils.WaitForCRQResourceUsage(
			ctx, k8sClient, crq.Name, corev1.ResourcePods, resource.MustParse("0"),
		)).To(Succeed())

		By("switching the CRQ to ReportOnly")
		// Re-fetch: the reconciler has status-patched the CRQ since CreateClusterResourceQuota
		// returned it, so the resourceVersion we're holding is stale.
		Expect(k8sClient.Get(ctx, client.ObjectKey{Name: crq.Name}, crq)).To(Succeed())
		crq.Spec.EnforcementMode = quotav1alpha1.EnforcementModeReportOnly
		Expect(k8sClient.Update(ctx, crq)).To(Succeed())

		By("admitting pods past the pod-count limit instead of denying them")
		for _, name := range []string{"enf-p1-" + suffix, "enf-p2-" + suffix} {
			pod, perr := testutils.CreatePod(ctx, k8sClient, ns.Name, name, nil, nil)
			Expect(perr).NotTo(HaveOccurred())
			DeferCleanup(func() { _ = k8sClient.Delete(ctx, pod) })
		}

		By("recording QuotaExceeded for the over-limit resource")
		Eventually(func() []string {
			return crqEventReasons(crq.Name)
		}, Timeout, Interval).Should(ContainElement("QuotaExceeded"))

		By("reflecting the real over-limit usage in status")
		Expect(testutils.WaitForCRQResourceUsage(
			ctx, k8sClient, crq.Name, corev1.ResourcePods, resource.MustParse("2"),
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
		// The CRD default fills this in as Blocking on read, even though the CRQ we sent
		// to Create never set it — this is the "no mode set" case a pre-upgrade CRQ hits.
		Expect(crq.Spec.EnforcementMode).To(Equal(quotav1alpha1.EnforcementModeBlocking))

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
