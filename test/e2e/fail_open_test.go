package e2e

import (
	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"

	appsv1 "k8s.io/api/apps/v1"
	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/resource"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"sigs.k8s.io/controller-runtime/pkg/client"

	quotav1alpha1 "github.com/powerhome/pac-quota-controller/api/v1alpha1"
	testutils "github.com/powerhome/pac-quota-controller/test/utils"
)

const (
	controllerNamespace  = "pac-quota-controller-system"
	controllerDeployment = "pac-quota-controller-manager"
)

var _ = Describe("Webhook fail-open behavior", Ordered, func() {
	var (
		suffix string
		ns     *corev1.Namespace
		crq    *quotav1alpha1.ClusterResourceQuota
		depKey = client.ObjectKey{Namespace: controllerNamespace, Name: controllerDeployment}
	)

	scaleController := func(replicas int32) {
		dep := &appsv1.Deployment{}
		Expect(k8sClient.Get(ctx, depKey, dep)).To(Succeed())
		dep.Spec.Replicas = &replicas
		Expect(k8sClient.Update(ctx, dep)).To(Succeed())
	}

	BeforeEach(func() {
		suffix = testutils.GenerateTestSuffix()
		team := "failopen-" + suffix

		var err error
		ns, err = testutils.CreateNamespace(ctx, k8sClient, "failopen-ns-"+suffix, map[string]string{"team": team})
		Expect(err).NotTo(HaveOccurred())

		// pods quota of 1: a second pod would normally be denied at admission.
		crq, err = testutils.CreateClusterResourceQuota(ctx, k8sClient, "failopen-crq-"+suffix,
			&metav1.LabelSelector{MatchLabels: map[string]string{"team": team}},
			quotav1alpha1.ResourceList{corev1.ResourcePods: resource.MustParse("1")})
		Expect(err).NotTo(HaveOccurred())
	})

	AfterEach(func() {
		_ = k8sClient.Delete(ctx, crq)
		_ = k8sClient.Delete(ctx, ns)
	})

	It("admits an over-quota pod while the controller is unavailable", func() {
		// Fill the pods=1 quota and wait for the controller to record it, so the
		// webhook would deny the next pod if it were reachable.
		p1, err := testutils.CreatePod(ctx, k8sClient, ns.Name, "pod-1-"+suffix,
			corev1.ResourceList{corev1.ResourceCPU: resource.MustParse("100m")}, nil)
		Expect(err).NotTo(HaveOccurred())
		DeferCleanup(func() { _ = k8sClient.Delete(ctx, p1) })

		Eventually(func() error {
			usage := testutils.GetRefreshedCRQStatusUsage(ctx, k8sClient, crq.Name)
			return testutils.ExpectCRQUsageToMatch(usage, map[string]string{"pods": "1"})
		}, Timeout, Interval).Should(Succeed())

		// Capture the current replica count and scale the controller down.
		dep := &appsv1.Deployment{}
		Expect(k8sClient.Get(ctx, depKey, dep)).To(Succeed())
		original := *dep.Spec.Replicas
		DeferCleanup(func() {
			scaleController(original)
			Eventually(func() int32 {
				restored := &appsv1.Deployment{}
				Expect(k8sClient.Get(ctx, depKey, restored)).To(Succeed())
				return restored.Status.AvailableReplicas
			}, Timeout, Interval).Should(BeNumerically(">=", 1))
		})

		By("scaling the controller (and its webhook) to zero")
		scaleController(0)
		Eventually(func() int32 {
			down := &appsv1.Deployment{}
			Expect(k8sClient.Get(ctx, depKey, down)).To(Succeed())
			return down.Status.ReadyReplicas
		}, Timeout, Interval).Should(BeZero())

		By("admitting a pod that would exceed the quota (failurePolicy: Ignore)")
		var p2 *corev1.Pod
		Eventually(func() error {
			p2, err = testutils.CreatePod(ctx, k8sClient, ns.Name, "pod-2-"+suffix,
				corev1.ResourceList{corev1.ResourceCPU: resource.MustParse("100m")}, nil)
			return err
		}, Timeout, Interval).Should(Succeed())
		DeferCleanup(func() { _ = k8sClient.Delete(ctx, p2) })
	})
})
