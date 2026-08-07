package quota

import (
	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"

	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/resource"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"

	quotav1alpha1 "github.com/powerhome/pac-quota-controller/api/v1alpha1"
)

func scopedCRQ(
	hard quotav1alpha1.ResourceList,
	scopes []corev1.ResourceQuotaScope,
	sel *corev1.ScopeSelector,
) *quotav1alpha1.ClusterResourceQuota {
	return &quotav1alpha1.ClusterResourceQuota{
		ObjectMeta: metav1.ObjectMeta{Name: "crq"},
		Spec: quotav1alpha1.ClusterResourceQuotaSpec{
			Hard:          hard,
			Scopes:        scopes,
			ScopeSelector: sel,
		},
	}
}

var _ = Describe("ValidateScopeSpec", func() {
	podsHard := quotav1alpha1.ResourceList{corev1.ResourcePods: resource.MustParse("5")}
	computeHard := quotav1alpha1.ResourceList{
		corev1.ResourcePods:        resource.MustParse("5"),
		corev1.ResourceRequestsCPU: resource.MustParse("2"),
	}

	DescribeTable("rejections",
		func(crq *quotav1alpha1.ClusterResourceQuota, substr string) {
			_, err := ValidateScopeSpec(crq)
			Expect(err).To(HaveOccurred())
			Expect(err.Error()).To(ContainSubstring(substr))
		},
		Entry("invalid scope name via scopes",
			scopedCRQ(podsHard, []corev1.ResourceQuotaScope{"Bogus"}, nil),
			"invalid quota scope"),
		Entry("invalid scope name via scopeSelector",
			scopedCRQ(podsHard, nil, &corev1.ScopeSelector{
				MatchExpressions: []corev1.ScopedResourceSelectorRequirement{{
					ScopeName: corev1.ResourceQuotaScope("Bogus"),
					Operator:  corev1.ScopeSelectorOpExists,
				}},
			}),
			"invalid quota scope"),
		Entry("duplicate scope",
			scopedCRQ(podsHard, []corev1.ResourceQuotaScope{
				corev1.ResourceQuotaScopeBestEffort, corev1.ResourceQuotaScopeBestEffort}, nil),
			"duplicate scope"),
		Entry("BestEffort with NotBestEffort",
			scopedCRQ(podsHard, []corev1.ResourceQuotaScope{
				corev1.ResourceQuotaScopeBestEffort, corev1.ResourceQuotaScopeNotBestEffort}, nil),
			"mutually exclusive"),
		Entry("Terminating with NotTerminating",
			scopedCRQ(podsHard, []corev1.ResourceQuotaScope{
				corev1.ResourceQuotaScopeTerminating, corev1.ResourceQuotaScopeNotTerminating}, nil),
			"mutually exclusive"),
		Entry("BestEffort with NotBestEffort entirely within scopeSelector",
			scopedCRQ(podsHard, nil, &corev1.ScopeSelector{
				MatchExpressions: []corev1.ScopedResourceSelectorRequirement{
					{ScopeName: corev1.ResourceQuotaScopeBestEffort, Operator: corev1.ScopeSelectorOpExists},
					{ScopeName: corev1.ResourceQuotaScopeNotBestEffort, Operator: corev1.ScopeSelectorOpExists},
				},
			}),
			"mutually exclusive"),
		Entry("non-Exists operator on BestEffort",
			scopedCRQ(podsHard, nil, &corev1.ScopeSelector{
				MatchExpressions: []corev1.ScopedResourceSelectorRequirement{{
					ScopeName: corev1.ResourceQuotaScopeBestEffort,
					Operator:  corev1.ScopeSelectorOpIn,
					Values:    []string{"x"},
				}},
			}),
			"only supports the Exists operator"),
		Entry("In without values",
			scopedCRQ(podsHard, nil, &corev1.ScopeSelector{
				MatchExpressions: []corev1.ScopedResourceSelectorRequirement{{
					ScopeName: corev1.ResourceQuotaScopePriorityClass,
					Operator:  corev1.ScopeSelectorOpIn,
				}},
			}),
			"requires values"),
		Entry("Exists with values",
			scopedCRQ(podsHard, nil, &corev1.ScopeSelector{
				MatchExpressions: []corev1.ScopedResourceSelectorRequirement{{
					ScopeName: corev1.ResourceQuotaScopePriorityClass,
					Operator:  corev1.ScopeSelectorOpExists,
					Values:    []string{"x"},
				}},
			}),
			"must not have values"),
		Entry("invalid operator",
			scopedCRQ(podsHard, nil, &corev1.ScopeSelector{
				MatchExpressions: []corev1.ScopedResourceSelectorRequirement{{
					ScopeName: corev1.ResourceQuotaScopePriorityClass,
					Operator:  corev1.ScopeSelectorOperator("Maybe"),
				}},
			}),
			"invalid operator"),
		Entry("services under a Terminating scope",
			scopedCRQ(quotav1alpha1.ResourceList{
				corev1.ResourcePods:     resource.MustParse("5"),
				corev1.ResourceServices: resource.MustParse("5"),
			}, []corev1.ResourceQuotaScope{corev1.ResourceQuotaScopeTerminating}, nil),
			"unsupported scope"),
		Entry("compute resource under BestEffort",
			scopedCRQ(computeHard, []corev1.ResourceQuotaScope{corev1.ResourceQuotaScopeBestEffort}, nil),
			"unsupported scope"),
		Entry("ephemeral-storage under a Terminating scope",
			scopedCRQ(quotav1alpha1.ResourceList{
				corev1.ResourcePods:                     resource.MustParse("5"),
				corev1.ResourceRequestsEphemeralStorage: resource.MustParse("1Gi"),
			}, []corev1.ResourceQuotaScope{corev1.ResourceQuotaScopeTerminating}, nil),
			"unsupported scope"),
		Entry("storage-class resource under a scopeSelector",
			scopedCRQ(quotav1alpha1.ResourceList{
				corev1.ResourceName("fast.storageclass.storage.k8s.io/requests.storage"): resource.MustParse("1Gi"),
			}, nil, &corev1.ScopeSelector{
				MatchExpressions: []corev1.ScopedResourceSelectorRequirement{{
					ScopeName: corev1.ResourceQuotaScopePriorityClass,
					Operator:  corev1.ScopeSelectorOpExists,
				}},
			}),
			"unsupported scope"),
	)

	DescribeTable("accepted",
		func(crq *quotav1alpha1.ClusterResourceQuota) {
			warnings, err := ValidateScopeSpec(crq)
			Expect(err).NotTo(HaveOccurred())
			Expect(warnings).To(BeEmpty())
		},
		Entry("no scopes with any resources",
			scopedCRQ(quotav1alpha1.ResourceList{
				corev1.ResourceServices: resource.MustParse("5"),
			}, nil, nil)),
		Entry("Terminating with pod compute resources",
			scopedCRQ(computeHard, []corev1.ResourceQuotaScope{corev1.ResourceQuotaScopeTerminating}, nil)),
		Entry("BestEffort with pods only",
			scopedCRQ(podsHard, []corev1.ResourceQuotaScope{corev1.ResourceQuotaScopeBestEffort}, nil)),
		Entry("extended resource bypasses the restriction",
			scopedCRQ(quotav1alpha1.ResourceList{
				corev1.ResourcePods:                            resource.MustParse("5"),
				corev1.ResourceName("requests.nvidia.com/gpu"): resource.MustParse("4"),
			}, []corev1.ResourceQuotaScope{corev1.ResourceQuotaScopeNotTerminating}, nil)),
		Entry("PriorityClass In via scopeSelector",
			scopedCRQ(computeHard, nil, &corev1.ScopeSelector{
				MatchExpressions: []corev1.ScopedResourceSelectorRequirement{{
					ScopeName: corev1.ResourceQuotaScopePriorityClass,
					Operator:  corev1.ScopeSelectorOpIn,
					Values:    []string{"high"},
				}},
			})),
	)

	It("warns on contradictory scopes across scopes and scopeSelector", func() {
		crq := scopedCRQ(podsHard,
			[]corev1.ResourceQuotaScope{corev1.ResourceQuotaScopeBestEffort},
			&corev1.ScopeSelector{
				MatchExpressions: []corev1.ScopedResourceSelectorRequirement{{
					ScopeName: corev1.ResourceQuotaScopeNotBestEffort,
					Operator:  corev1.ScopeSelectorOpExists,
				}},
			})
		warnings, err := ValidateScopeSpec(crq)
		Expect(err).NotTo(HaveOccurred())
		Expect(warnings).To(HaveLen(1))
		Expect(warnings[0]).To(ContainSubstring("match no pods"))
	})
})
