package e2e

import (
	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"

	corev1 "k8s.io/api/core/v1"
	schedulingv1 "k8s.io/api/scheduling/v1"
	"k8s.io/apimachinery/pkg/api/resource"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"sigs.k8s.io/controller-runtime/pkg/client"

	quotav1alpha1 "github.com/powerhome/pac-quota-controller/api/v1alpha1"
	testutils "github.com/powerhome/pac-quota-controller/test/utils"
)

var _ = Describe("Scoped ClusterResourceQuota Tests", func() {
	var (
		testNamespace string
		testSuffix    string
		nsLabels      map[string]string
		ns            *corev1.Namespace
	)

	createScopedCRQ := func(
		name string,
		hard quotav1alpha1.ResourceList,
		scopes []corev1.ResourceQuotaScope,
		sel *corev1.ScopeSelector,
	) *quotav1alpha1.ClusterResourceQuota {
		crq := &quotav1alpha1.ClusterResourceQuota{
			ObjectMeta: metav1.ObjectMeta{Name: name},
			Spec: quotav1alpha1.ClusterResourceQuotaSpec{
				NamespaceSelector: &metav1.LabelSelector{MatchLabels: nsLabels},
				Hard:              hard,
				Scopes:            scopes,
				ScopeSelector:     sel,
			},
		}
		Expect(k8sClient.Create(ctx, crq)).To(Succeed())
		DeferCleanup(func() { _ = k8sClient.Delete(ctx, crq) })
		return crq
	}

	BeforeEach(func() {
		testSuffix = testutils.GenerateTestSuffix()
		testNamespace = testutils.GenerateResourceName("scope-ns-" + testSuffix)
		nsLabels = map[string]string{"scope-test": "test-label-" + testSuffix}

		var err error
		ns, err = testutils.CreateNamespace(ctx, k8sClient, testNamespace, nsLabels)
		Expect(err).NotTo(HaveOccurred())
		DeferCleanup(func() { _ = k8sClient.Delete(ctx, ns) })
	})

	Context("BestEffort scope", func() {
		It("counts and enforces only best-effort pods", func() {
			crqName := testutils.GenerateResourceName("scope-crq-" + testSuffix)
			createScopedCRQ(crqName,
				quotav1alpha1.ResourceList{corev1.ResourcePods: resource.MustParse("2")},
				[]corev1.ResourceQuotaScope{corev1.ResourceQuotaScopeBestEffort}, nil)

			for _, name := range []string{"be-1-" + testSuffix, "be-2-" + testSuffix} {
				pod, err := testutils.CreatePod(ctx, k8sClient, testNamespace, name, nil, nil)
				Expect(err).NotTo(HaveOccurred())
				DeferCleanup(func() { _ = k8sClient.Delete(ctx, pod) })
			}
			Expect(testutils.WaitForCRQResourceUsage(
				ctx, k8sClient, crqName, corev1.ResourcePods, resource.MustParse("2"),
			)).To(Succeed())

			// Third best-effort pod exceeds the scoped count.
			err := testutils.EventuallyDenied(ctx, k8sClient, func() (client.Object, error) {
				return testutils.CreatePod(ctx, k8sClient, testNamespace, "be-3-"+testSuffix, nil, nil)
			})
			Expect(err.Error()).To(ContainSubstring("pods limit exceeded"))

			// A burstable pod is out of scope and admitted even though the quota is full.
			pod, err := testutils.CreatePod(ctx, k8sClient, testNamespace, "burstable-"+testSuffix,
				corev1.ResourceList{corev1.ResourceCPU: resource.MustParse("10m")}, nil)
			Expect(err).NotTo(HaveOccurred())
			DeferCleanup(func() { _ = k8sClient.Delete(ctx, pod) })

			// The out-of-scope pod never shows up in usage.
			Expect(testutils.WaitForCRQResourceUsage(
				ctx, k8sClient, crqName, corev1.ResourcePods, resource.MustParse("2"),
			)).To(Succeed())
		})
	})

	Context("Terminating scope", func() {
		makePodWithADS := func(name, cpu string, ads *int64) *corev1.Pod {
			return &corev1.Pod{
				ObjectMeta: metav1.ObjectMeta{Name: name, Namespace: testNamespace},
				Spec: corev1.PodSpec{
					ActiveDeadlineSeconds: ads,
					Containers: []corev1.Container{{
						Name:  "test-container",
						Image: "nginx:latest",
						Resources: corev1.ResourceRequirements{
							Requests: corev1.ResourceList{corev1.ResourceCPU: resource.MustParse(cpu)},
						},
					}},
				},
			}
		}

		It("only charges pods with an active deadline", func() {
			ads := int64(600)
			crqName := testutils.GenerateResourceName("scope-crq-" + testSuffix)
			createScopedCRQ(crqName,
				quotav1alpha1.ResourceList{corev1.ResourceRequestsCPU: resource.MustParse("100m")},
				[]corev1.ResourceQuotaScope{corev1.ResourceQuotaScopeTerminating}, nil)

			// Out of scope: no deadline, exceeds the cpu limit on its own, still admitted.
			nonTerminating := makePodWithADS("no-ads-"+testSuffix, "200m", nil)
			Expect(k8sClient.Create(ctx, nonTerminating)).To(Succeed())
			DeferCleanup(func() { _ = k8sClient.Delete(ctx, nonTerminating) })

			// In scope: charged against the quota.
			terminating := makePodWithADS("ads-1-"+testSuffix, "60m", &ads)
			Expect(k8sClient.Create(ctx, terminating)).To(Succeed())
			DeferCleanup(func() { _ = k8sClient.Delete(ctx, terminating) })

			Expect(testutils.WaitForCRQResourceUsage(
				ctx, k8sClient, crqName, corev1.ResourceRequestsCPU, resource.MustParse("60m"),
			)).To(Succeed())

			// Second in-scope pod would exceed 100m.
			err := testutils.EventuallyDenied(ctx, k8sClient, func() (client.Object, error) {
				pod := makePodWithADS(testutils.GenerateResourceName("ads-2"), "60m", &ads)
				return pod, k8sClient.Create(ctx, pod)
			})
			Expect(err.Error()).To(ContainSubstring("CPU requests validation failed"))
		})
	})

	Context("PriorityClass scopeSelector", func() {
		It("only charges pods with a matching priority class", func() {
			pcName := "scope-high-" + testSuffix
			pc := &schedulingv1.PriorityClass{
				ObjectMeta: metav1.ObjectMeta{Name: pcName},
				Value:      1000,
			}
			Expect(k8sClient.Create(ctx, pc)).To(Succeed())
			DeferCleanup(func() { _ = k8sClient.Delete(ctx, pc) })

			crqName := testutils.GenerateResourceName("scope-crq-" + testSuffix)
			createScopedCRQ(crqName,
				quotav1alpha1.ResourceList{corev1.ResourcePods: resource.MustParse("1")},
				nil,
				&corev1.ScopeSelector{
					MatchExpressions: []corev1.ScopedResourceSelectorRequirement{{
						ScopeName: corev1.ResourceQuotaScopePriorityClass,
						Operator:  corev1.ScopeSelectorOpIn,
						Values:    []string{pcName},
					}},
				})

			priorityPod := func(name string) *corev1.Pod {
				return &corev1.Pod{
					ObjectMeta: metav1.ObjectMeta{Name: name, Namespace: testNamespace},
					Spec: corev1.PodSpec{
						PriorityClassName: pcName,
						Containers: []corev1.Container{{
							Name:  "test-container",
							Image: "nginx:latest",
						}},
					},
				}
			}

			first := priorityPod("prio-1-" + testSuffix)
			Expect(k8sClient.Create(ctx, first)).To(Succeed())
			DeferCleanup(func() { _ = k8sClient.Delete(ctx, first) })

			Expect(testutils.WaitForCRQResourceUsage(
				ctx, k8sClient, crqName, corev1.ResourcePods, resource.MustParse("1"),
			)).To(Succeed())

			err := testutils.EventuallyDenied(ctx, k8sClient, func() (client.Object, error) {
				pod := priorityPod(testutils.GenerateResourceName("prio-2"))
				return pod, k8sClient.Create(ctx, pod)
			})
			Expect(err.Error()).To(ContainSubstring("pods limit exceeded"))

			// No priority class: out of scope, admitted despite the full quota.
			plain, err := testutils.CreatePod(ctx, k8sClient, testNamespace, "plain-"+testSuffix, nil, nil)
			Expect(err).NotTo(HaveOccurred())
			DeferCleanup(func() { _ = k8sClient.Delete(ctx, plain) })
		})
	})

	Context("Scope spec validation", func() {
		It("rejects a scoped CRQ with non-pod resources via the webhook", func() {
			crq := &quotav1alpha1.ClusterResourceQuota{
				ObjectMeta: metav1.ObjectMeta{Name: testutils.GenerateResourceName("bad-crq-" + testSuffix)},
				Spec: quotav1alpha1.ClusterResourceQuotaSpec{
					NamespaceSelector: &metav1.LabelSelector{MatchLabels: nsLabels},
					Hard: quotav1alpha1.ResourceList{
						corev1.ResourcePods:     resource.MustParse("5"),
						corev1.ResourceServices: resource.MustParse("5"),
					},
					Scopes: []corev1.ResourceQuotaScope{corev1.ResourceQuotaScopeTerminating},
				},
			}
			err := k8sClient.Create(ctx, crq)
			if err == nil {
				DeferCleanup(func() { _ = k8sClient.Delete(ctx, crq) })
			}
			Expect(err).To(HaveOccurred())
			Expect(err.Error()).To(ContainSubstring("unsupported scope"))
		})

		It("rejects mutually exclusive scopes via CRD CEL validation", func() {
			crq := &quotav1alpha1.ClusterResourceQuota{
				ObjectMeta: metav1.ObjectMeta{Name: testutils.GenerateResourceName("cel-crq-" + testSuffix)},
				Spec: quotav1alpha1.ClusterResourceQuotaSpec{
					NamespaceSelector: &metav1.LabelSelector{MatchLabels: nsLabels},
					Hard:              quotav1alpha1.ResourceList{corev1.ResourcePods: resource.MustParse("5")},
					Scopes: []corev1.ResourceQuotaScope{
						corev1.ResourceQuotaScopeBestEffort,
						corev1.ResourceQuotaScopeNotBestEffort,
					},
				},
			}
			err := k8sClient.Create(ctx, crq)
			if err == nil {
				DeferCleanup(func() { _ = k8sClient.Delete(ctx, crq) })
			}
			Expect(err).To(HaveOccurred())
			Expect(err.Error()).To(ContainSubstring("mutually exclusive"))
		})
	})
})
