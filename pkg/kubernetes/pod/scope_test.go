package pod

import (
	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"

	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/resource"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/utils/ptr"
)

func bestEffortPod() *corev1.Pod {
	return &corev1.Pod{
		ObjectMeta: metav1.ObjectMeta{Name: "best-effort"},
		Spec: corev1.PodSpec{
			Containers: []corev1.Container{{Name: "c1"}},
		},
	}
}

func burstablePod() *corev1.Pod {
	return &corev1.Pod{
		ObjectMeta: metav1.ObjectMeta{Name: "burstable"},
		Spec: corev1.PodSpec{
			Containers: []corev1.Container{{
				Name: "c1",
				Resources: corev1.ResourceRequirements{
					Requests: corev1.ResourceList{corev1.ResourceCPU: resource.MustParse("100m")},
				},
			}},
		},
	}
}

func selectorWith(reqs ...corev1.ScopedResourceSelectorRequirement) *corev1.ScopeSelector {
	return &corev1.ScopeSelector{MatchExpressions: reqs}
}

var _ = Describe("Scope matching", func() {
	Describe("HasScopes", func() {
		It("should be false without scopes or selector", func() {
			Expect(HasScopes(nil, nil)).To(BeFalse())
			Expect(HasScopes(nil, &corev1.ScopeSelector{})).To(BeFalse())
		})

		It("should be true with scopes or selector expressions", func() {
			Expect(HasScopes([]corev1.ResourceQuotaScope{corev1.ResourceQuotaScopeBestEffort}, nil)).To(BeTrue())
			Expect(HasScopes(nil, selectorWith(corev1.ScopedResourceSelectorRequirement{
				ScopeName: corev1.ResourceQuotaScopeBestEffort,
				Operator:  corev1.ScopeSelectorOpExists,
			}))).To(BeTrue())
		})
	})

	Describe("BuildScopeRequirements", func() {
		It("should normalize plain scopes to Exists requirements and append selector expressions", func() {
			reqs := BuildScopeRequirements(
				[]corev1.ResourceQuotaScope{corev1.ResourceQuotaScopeTerminating},
				selectorWith(corev1.ScopedResourceSelectorRequirement{
					ScopeName: corev1.ResourceQuotaScopePriorityClass,
					Operator:  corev1.ScopeSelectorOpIn,
					Values:    []string{"high"},
				}),
			)
			Expect(reqs).To(HaveLen(2))
			Expect(reqs[0].ScopeName).To(Equal(corev1.ResourceQuotaScopeTerminating))
			Expect(reqs[0].Operator).To(Equal(corev1.ScopeSelectorOpExists))
			Expect(reqs[1].ScopeName).To(Equal(corev1.ResourceQuotaScopePriorityClass))
		})
	})

	Describe("PodInScope", func() {
		It("should match every pod when no scopes are set", func() {
			inScope, err := PodInScope(bestEffortPod(), nil, nil)
			Expect(err).NotTo(HaveOccurred())
			Expect(inScope).To(BeTrue())
		})

		It("should not match a nil pod", func() {
			inScope, err := PodInScope(nil, nil, nil)
			Expect(err).NotTo(HaveOccurred())
			Expect(inScope).To(BeFalse())
		})

		Context("Terminating / NotTerminating", func() {
			terminating := []corev1.ResourceQuotaScope{corev1.ResourceQuotaScopeTerminating}
			notTerminating := []corev1.ResourceQuotaScope{corev1.ResourceQuotaScopeNotTerminating}

			It("should classify by activeDeadlineSeconds", func() {
				withADS := bestEffortPod()
				withADS.Spec.ActiveDeadlineSeconds = ptr.To(int64(300))
				withoutADS := bestEffortPod()

				inScope, err := PodInScope(withADS, terminating, nil)
				Expect(err).NotTo(HaveOccurred())
				Expect(inScope).To(BeTrue())

				inScope, err = PodInScope(withoutADS, terminating, nil)
				Expect(err).NotTo(HaveOccurred())
				Expect(inScope).To(BeFalse())

				inScope, err = PodInScope(withADS, notTerminating, nil)
				Expect(err).NotTo(HaveOccurred())
				Expect(inScope).To(BeFalse())

				inScope, err = PodInScope(withoutADS, notTerminating, nil)
				Expect(err).NotTo(HaveOccurred())
				Expect(inScope).To(BeTrue())
			})

			It("should treat a zero deadline as terminating", func() {
				pod := bestEffortPod()
				pod.Spec.ActiveDeadlineSeconds = ptr.To(int64(0))
				inScope, err := PodInScope(pod, terminating, nil)
				Expect(err).NotTo(HaveOccurred())
				Expect(inScope).To(BeTrue())
			})
		})

		Context("BestEffort / NotBestEffort", func() {
			It("should classify by computed QoS", func() {
				inScope, err := PodInScope(bestEffortPod(),
					[]corev1.ResourceQuotaScope{corev1.ResourceQuotaScopeBestEffort}, nil)
				Expect(err).NotTo(HaveOccurred())
				Expect(inScope).To(BeTrue())

				inScope, err = PodInScope(burstablePod(),
					[]corev1.ResourceQuotaScope{corev1.ResourceQuotaScopeBestEffort}, nil)
				Expect(err).NotTo(HaveOccurred())
				Expect(inScope).To(BeFalse())

				inScope, err = PodInScope(burstablePod(),
					[]corev1.ResourceQuotaScope{corev1.ResourceQuotaScopeNotBestEffort}, nil)
				Expect(err).NotTo(HaveOccurred())
				Expect(inScope).To(BeTrue())
			})
		})

		Context("PriorityClass", func() {
			priorityPod := func(name string) *corev1.Pod {
				p := bestEffortPod()
				p.Spec.PriorityClassName = name
				return p
			}

			It("should match Exists only when a priority class is set", func() {
				scopes := []corev1.ResourceQuotaScope{corev1.ResourceQuotaScopePriorityClass}
				inScope, err := PodInScope(priorityPod("high"), scopes, nil)
				Expect(err).NotTo(HaveOccurred())
				Expect(inScope).To(BeTrue())

				inScope, err = PodInScope(priorityPod(""), scopes, nil)
				Expect(err).NotTo(HaveOccurred())
				Expect(inScope).To(BeFalse())
			})

			It("should match In against the priority class name", func() {
				sel := selectorWith(corev1.ScopedResourceSelectorRequirement{
					ScopeName: corev1.ResourceQuotaScopePriorityClass,
					Operator:  corev1.ScopeSelectorOpIn,
					Values:    []string{"high", "critical"},
				})
				inScope, err := PodInScope(priorityPod("high"), nil, sel)
				Expect(err).NotTo(HaveOccurred())
				Expect(inScope).To(BeTrue())

				inScope, err = PodInScope(priorityPod("low"), nil, sel)
				Expect(err).NotTo(HaveOccurred())
				Expect(inScope).To(BeFalse())

				inScope, err = PodInScope(priorityPod(""), nil, sel)
				Expect(err).NotTo(HaveOccurred())
				Expect(inScope).To(BeFalse())
			})

			It("should match NotIn and DoesNotExist for pods without a priority class", func() {
				notIn := selectorWith(corev1.ScopedResourceSelectorRequirement{
					ScopeName: corev1.ResourceQuotaScopePriorityClass,
					Operator:  corev1.ScopeSelectorOpNotIn,
					Values:    []string{"high"},
				})
				inScope, err := PodInScope(priorityPod(""), nil, notIn)
				Expect(err).NotTo(HaveOccurred())
				Expect(inScope).To(BeTrue())

				inScope, err = PodInScope(priorityPod("high"), nil, notIn)
				Expect(err).NotTo(HaveOccurred())
				Expect(inScope).To(BeFalse())

				doesNotExist := selectorWith(corev1.ScopedResourceSelectorRequirement{
					ScopeName: corev1.ResourceQuotaScopePriorityClass,
					Operator:  corev1.ScopeSelectorOpDoesNotExist,
				})
				inScope, err = PodInScope(priorityPod(""), nil, doesNotExist)
				Expect(err).NotTo(HaveOccurred())
				Expect(inScope).To(BeTrue())

				inScope, err = PodInScope(priorityPod("high"), nil, doesNotExist)
				Expect(err).NotTo(HaveOccurred())
				Expect(inScope).To(BeFalse())
			})

			It("should error on a NotIn requirement with no values (a CRQ that bypassed admission)", func() {
				sel := selectorWith(corev1.ScopedResourceSelectorRequirement{
					ScopeName: corev1.ResourceQuotaScopePriorityClass,
					Operator:  corev1.ScopeSelectorOpNotIn,
				})
				_, err := PodInScope(priorityPod("high"), nil, sel)
				Expect(err).To(HaveOccurred())
			})
		})

		Context("CrossNamespacePodAffinity", func() {
			scopes := []corev1.ResourceQuotaScope{corev1.ResourceQuotaScopeCrossNamespacePodAffinity}
			term := func(cross bool) corev1.PodAffinityTerm {
				t := corev1.PodAffinityTerm{TopologyKey: "kubernetes.io/hostname"}
				if cross {
					t.Namespaces = []string{"other"}
				}
				return t
			}

			It("should match required affinity terms with namespaces", func() {
				pod := bestEffortPod()
				pod.Spec.Affinity = &corev1.Affinity{PodAffinity: &corev1.PodAffinity{
					RequiredDuringSchedulingIgnoredDuringExecution: []corev1.PodAffinityTerm{term(true)},
				}}
				inScope, err := PodInScope(pod, scopes, nil)
				Expect(err).NotTo(HaveOccurred())
				Expect(inScope).To(BeTrue())
			})

			It("should match preferred anti-affinity terms with a namespaceSelector", func() {
				pod := bestEffortPod()
				t := term(false)
				t.NamespaceSelector = &metav1.LabelSelector{}
				pod.Spec.Affinity = &corev1.Affinity{PodAntiAffinity: &corev1.PodAntiAffinity{
					PreferredDuringSchedulingIgnoredDuringExecution: []corev1.WeightedPodAffinityTerm{
						{Weight: 1, PodAffinityTerm: t},
					},
				}}
				inScope, err := PodInScope(pod, scopes, nil)
				Expect(err).NotTo(HaveOccurred())
				Expect(inScope).To(BeTrue())
			})

			It("should not match same-namespace affinity terms", func() {
				pod := bestEffortPod()
				pod.Spec.Affinity = &corev1.Affinity{PodAffinity: &corev1.PodAffinity{
					RequiredDuringSchedulingIgnoredDuringExecution: []corev1.PodAffinityTerm{term(false)},
				}}
				inScope, err := PodInScope(pod, scopes, nil)
				Expect(err).NotTo(HaveOccurred())
				Expect(inScope).To(BeFalse())
			})

			It("should not match pods without affinity", func() {
				inScope, err := PodInScope(bestEffortPod(), scopes, nil)
				Expect(err).NotTo(HaveOccurred())
				Expect(inScope).To(BeFalse())
			})
		})

		Context("combined scopes", func() {
			It("should require all requirements to match (AND semantics)", func() {
				pod := bestEffortPod()
				pod.Spec.ActiveDeadlineSeconds = ptr.To(int64(300))
				scopes := []corev1.ResourceQuotaScope{
					corev1.ResourceQuotaScopeTerminating,
					corev1.ResourceQuotaScopeBestEffort,
				}
				inScope, err := PodInScope(pod, scopes, nil)
				Expect(err).NotTo(HaveOccurred())
				Expect(inScope).To(BeTrue())

				burstable := burstablePod()
				burstable.Spec.ActiveDeadlineSeconds = ptr.To(int64(300))
				inScope, err = PodInScope(burstable, scopes, nil)
				Expect(err).NotTo(HaveOccurred())
				Expect(inScope).To(BeFalse())
			})
		})

		It("should error on an unknown scope", func() {
			sel := selectorWith(corev1.ScopedResourceSelectorRequirement{
				ScopeName: corev1.ResourceQuotaScope("Bogus"),
				Operator:  corev1.ScopeSelectorOpExists,
			})
			_, err := PodInScope(bestEffortPod(), nil, sel)
			Expect(err).To(HaveOccurred())
		})

		It("should error on an unknown PriorityClass operator", func() {
			sel := selectorWith(corev1.ScopedResourceSelectorRequirement{
				ScopeName: corev1.ResourceQuotaScopePriorityClass,
				Operator:  corev1.ScopeSelectorOperator("Bogus"),
			})
			_, err := PodInScope(bestEffortPod(), nil, sel)
			Expect(err).To(HaveOccurred())
		})
	})

	Describe("FilterInScope", func() {
		It("should return the input slice unchanged when no scopes are set", func() {
			pods := []corev1.Pod{*bestEffortPod(), *burstablePod()}
			filtered, err := FilterInScope(pods, nil, nil)
			Expect(err).NotTo(HaveOccurred())
			Expect(filtered).To(HaveLen(2))
		})

		It("should keep only in-scope pods", func() {
			pods := []corev1.Pod{*bestEffortPod(), *burstablePod()}
			filtered, err := FilterInScope(pods,
				[]corev1.ResourceQuotaScope{corev1.ResourceQuotaScopeBestEffort}, nil)
			Expect(err).NotTo(HaveOccurred())
			Expect(filtered).To(HaveLen(1))
			Expect(filtered[0].Name).To(Equal("best-effort"))
		})

		It("should propagate matcher errors", func() {
			pods := []corev1.Pod{*bestEffortPod()}
			sel := selectorWith(corev1.ScopedResourceSelectorRequirement{
				ScopeName: corev1.ResourceQuotaScope("Bogus"),
				Operator:  corev1.ScopeSelectorOpExists,
			})
			_, err := FilterInScope(pods, nil, sel)
			Expect(err).To(HaveOccurred())
		})
	})
})
