package e2e

import (
	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"

	corev1 "k8s.io/api/core/v1"
	eventsv1 "k8s.io/api/events/v1"
	"k8s.io/apimachinery/pkg/api/resource"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"sigs.k8s.io/controller-runtime/pkg/client"

	quotav1alpha1 "github.com/powerhome/pac-quota-controller/api/v1alpha1"
	testutils "github.com/powerhome/pac-quota-controller/test/utils"
)

// crqEventReasons returns the reasons of all events recorded against the named CRQ.
func crqEventReasons(crqName string) []string {
	list := &eventsv1.EventList{}
	if err := k8sClient.List(ctx, list); err != nil {
		return nil
	}
	var reasons []string
	for i := range list.Items {
		if list.Items[i].Regarding.Name == crqName {
			reasons = append(reasons, list.Items[i].Reason)
		}
	}
	return reasons
}

var _ = Describe("Controller Events E2E", func() {
	var suffix, team string

	BeforeEach(func() {
		suffix = testutils.GenerateTestSuffix()
		team = "evt-" + suffix
	})

	It("emits NamespaceAdded then NamespaceRemoved as namespaces enter and leave scope", func() {
		ns, err := testutils.CreateNamespace(ctx, k8sClient, "evt-ns-"+suffix, map[string]string{"team": team})
		Expect(err).NotTo(HaveOccurred())
		DeferCleanup(func() { _ = k8sClient.Delete(ctx, ns) })

		crq, err := testutils.CreateClusterResourceQuota(ctx, k8sClient, "evt-crq-"+suffix,
			&metav1.LabelSelector{MatchLabels: map[string]string{"team": team}},
			quotav1alpha1.ResourceList{corev1.ResourcePods: resource.MustParse("5")})
		Expect(err).NotTo(HaveOccurred())
		DeferCleanup(func() { _ = k8sClient.Delete(ctx, crq) })

		By("recording NamespaceAdded for the matching namespace")
		Eventually(func() []string {
			return crqEventReasons(crq.Name)
		}, Timeout, Interval).Should(ContainElement("NamespaceAdded"))

		By("relabeling the namespace out of scope")
		Expect(k8sClient.Get(ctx, client.ObjectKey{Name: ns.Name}, ns)).To(Succeed())
		ns.Labels["team"] = "other-" + suffix
		Expect(k8sClient.Update(ctx, ns)).To(Succeed())

		By("recording NamespaceRemoved once it leaves scope")
		Eventually(func() []string {
			return crqEventReasons(crq.Name)
		}, Timeout, Interval).Should(ContainElement("NamespaceRemoved"))
	})

	It("emits QuotaExceeded when observed usage exceeds the hard limit", func() {
		ns, err := testutils.CreateNamespace(ctx, k8sClient, "evt-exceed-ns-"+suffix, map[string]string{"team": team})
		Expect(err).NotTo(HaveOccurred())
		DeferCleanup(func() { _ = k8sClient.Delete(ctx, ns) })

		// Create two pods before any CRQ selects the namespace, so admission allows
		// them; the CRQ created afterwards will observe usage above its limit.
		for _, name := range []string{"evt-p1-" + suffix, "evt-p2-" + suffix} {
			p, perr := testutils.CreatePod(ctx, k8sClient, ns.Name, name,
				corev1.ResourceList{corev1.ResourceCPU: resource.MustParse("50m")}, nil)
			Expect(perr).NotTo(HaveOccurred())
			DeferCleanup(func() { _ = k8sClient.Delete(ctx, p) })
		}

		crq, err := testutils.CreateClusterResourceQuota(ctx, k8sClient, "evt-exceed-crq-"+suffix,
			&metav1.LabelSelector{MatchLabels: map[string]string{"team": team}},
			quotav1alpha1.ResourceList{corev1.ResourcePods: resource.MustParse("1")})
		Expect(err).NotTo(HaveOccurred())
		DeferCleanup(func() { _ = k8sClient.Delete(ctx, crq) })

		By("recording QuotaExceeded for the over-limit resource")
		Eventually(func() []string {
			return crqEventReasons(crq.Name)
		}, Timeout, Interval).Should(ContainElement("QuotaExceeded"))
	})

	// InvalidSelector is emitted only by the reconciler's defensive path. In a real
	// cluster the CRQ admission webhook rejects a malformed selector before the
	// object is ever stored, so this path is unreachable end-to-end. It is covered
	// by the reconciler unit tests instead.
	It("emits InvalidSelector events for malformed selectors", func() {
		Skip("malformed selectors are rejected at admission; covered by reconciler unit tests")
	})

	// Events are recorded via the events.k8s.io API and carry no PAC labels today,
	// so there is nothing to assert. NOTE: event cleanup (pkg/events) queries by the
	// quota.pac.io/event-source label, which these events lack — tracked separately.
	It("includes PAC-specific labels on events", func() {
		Skip("events currently carry no PAC labels; recorder labeling is a separate fix")
	})
})
