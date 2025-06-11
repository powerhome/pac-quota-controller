/*
Copyright 2025.

Licensed under the Apache License, Version 2.0 (the "License");
you may not use this file except in compliance with the License.
You may obtain a copy of the License at

    http://www.apache.org/licenses/LICENSE-2.0

Unless required by applicable law or agreed to in writing, software
distributed under the License is distributed on an "AS IS" BASIS,
WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
See the License for the specific language governing permissions and
limitations under the License.
*/

package e2e

import (
	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"

	"math/rand"
	"strconv"
	"time"

	quotav1alpha1 "github.com/powerhome/pac-quota-controller/api/v1alpha1"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"sigs.k8s.io/controller-runtime/pkg/client"
)

func getCRQStatusNamespaces(crqName string) []string {
	crq := &quotav1alpha1.ClusterResourceQuota{}
	err := k8sClient.Get(ctx, client.ObjectKey{Name: crqName}, crq)
	if err != nil || crq.Status.Namespaces == nil {
		return nil
	}
	nsList := make([]string, 0, len(crq.Status.Namespaces))
	for _, ns := range crq.Status.Namespaces {
		nsList = append(nsList, ns.Namespace)
	}
	return nsList
}

func testNonMatchingNamespaceNotInStatus(crqName, nsName string, nsLabels map[string]string, deleteNS bool) {
	nonMatching := &corev1.Namespace{
		ObjectMeta: metav1.ObjectMeta{
			Name:   nsName,
			Labels: nsLabels,
		},
	}
	Expect(k8sClient.Create(ctx, nonMatching)).To(Succeed())
	crq := &quotav1alpha1.ClusterResourceQuota{
		ObjectMeta: metav1.ObjectMeta{
			Name: crqName,
		},
		Spec: quotav1alpha1.ClusterResourceQuotaSpec{
			NamespaceSelector: &metav1.LabelSelector{
				MatchLabels: map[string]string{"quota": "limited"},
			},
			Hard: quotav1alpha1.ResourceList{},
		},
	}
	Expect(k8sClient.Create(ctx, crq)).To(Succeed())
	DeferCleanup(func() {
		_ = k8sClient.Delete(ctx, crq)
		if !deleteNS {
			_ = k8sClient.Delete(ctx, nonMatching)
		}
	})
	if deleteNS {
		_ = k8sClient.Delete(ctx, nonMatching)
		Eventually(func() []string {
			return getCRQStatusNamespaces(crqName)
		}, "10s", "1s").ShouldNot(ContainElement(nsName))
	} else {
		Consistently(func() []string {
			return getCRQStatusNamespaces(crqName)
		}, "5s", "1s").ShouldNot(ContainElement(nsName))
	}
}

func testNonMatchingNamespaceNotInStatusWithLabels(crqName, nsName string, nsLabels map[string]string, labelKey, labelValue string, deleteNS bool) {
	nonMatching := &corev1.Namespace{
		ObjectMeta: metav1.ObjectMeta{
			Name:   nsName,
			Labels: nsLabels,
		},
	}
	Expect(k8sClient.Create(ctx, nonMatching)).To(Succeed())
	crq := &quotav1alpha1.ClusterResourceQuota{
		ObjectMeta: metav1.ObjectMeta{
			Name: crqName,
		},
		Spec: quotav1alpha1.ClusterResourceQuotaSpec{
			NamespaceSelector: &metav1.LabelSelector{
				MatchLabels: map[string]string{labelKey: labelValue},
			},
			Hard: quotav1alpha1.ResourceList{},
		},
	}
	Expect(k8sClient.Create(ctx, crq)).To(Succeed())
	DeferCleanup(func() {
		_ = k8sClient.Delete(ctx, crq)
		if !deleteNS {
			_ = k8sClient.Delete(ctx, nonMatching)
		}
	})
	if deleteNS {
		_ = k8sClient.Delete(ctx, nonMatching)
		Eventually(func() []string {
			return getCRQStatusNamespaces(crqName)
		}, "10s", "1s").ShouldNot(ContainElement(nsName))
	} else {
		Consistently(func() []string {
			return getCRQStatusNamespaces(crqName)
		}, "5s", "1s").ShouldNot(ContainElement(nsName))
	}
}

func ensureNamespaceDeleted(name string) {
	ns := &corev1.Namespace{}
	_ = k8sClient.Get(ctx, client.ObjectKey{Name: name}, ns)
	_ = k8sClient.Delete(ctx, ns)
	// Wait for namespace to be deleted
	Eventually(func() error {
		return k8sClient.Get(ctx, client.ObjectKey{Name: name}, ns)
	}, "10s", "250ms").ShouldNot(Succeed())
}

func ensureCRQDeleted(name string) {
	crq := &quotav1alpha1.ClusterResourceQuota{}
	_ = k8sClient.Get(ctx, client.ObjectKey{Name: name}, crq)
	_ = k8sClient.Delete(ctx, crq)
	// Wait for CRQ to be deleted
	Eventually(func() error {
		return k8sClient.Get(ctx, client.ObjectKey{Name: name}, crq)
	}, "10s", "250ms").ShouldNot(Succeed())
}

var _ = Describe("ClusterResourceQuota Namespace Selection", func() {
	BeforeEach(func() {
		rand.Seed(time.Now().UnixNano())
	})

	It("Should add a matching namespace to the CRQ status", func() {
		suffix := strconv.Itoa(rand.Intn(1000000))
		labelKey := "quota-" + strconv.Itoa(rand.Intn(1000000))
		labelValue := "limited-" + strconv.Itoa(rand.Intn(1000000))
		crqName := "test-namespaceselection-quota-" + suffix
		matchingNS := "test-namespaceselection-ns-" + suffix

		ensureNamespaceDeleted(matchingNS)
		ensureCRQDeleted(crqName)

		matching := &corev1.Namespace{
			ObjectMeta: metav1.ObjectMeta{
				Name:   matchingNS,
				Labels: map[string]string{labelKey: labelValue},
			},
		}
		Expect(k8sClient.Create(ctx, matching)).To(Succeed())
		crq := &quotav1alpha1.ClusterResourceQuota{
			ObjectMeta: metav1.ObjectMeta{
				Name: crqName,
			},
			Spec: quotav1alpha1.ClusterResourceQuotaSpec{
				NamespaceSelector: &metav1.LabelSelector{
					MatchLabels: map[string]string{labelKey: labelValue},
				},
				Hard: quotav1alpha1.ResourceList{},
			},
		}
		Expect(k8sClient.Create(ctx, crq)).To(Succeed())
		DeferCleanup(func() {
			_ = k8sClient.Delete(ctx, matching)
			_ = k8sClient.Delete(ctx, crq)
		})
		By("Waiting for namespace to appear in CRQ status")
		Eventually(func() []string {
			return getCRQStatusNamespaces(crqName)
		}, "10s", "1s").Should(ContainElement(matchingNS))
	})

	It("Should not add a non-matching namespace to the CRQ status (creation)", func() {
		suffix := strconv.Itoa(rand.Intn(1000000))
		labelKey := "quota-" + strconv.Itoa(rand.Intn(1000000))
		labelValue := "limited-" + strconv.Itoa(rand.Intn(1000000))
		crqName := "test-namespaceselection-quota-" + suffix
		nonMatchingNS := "test-namespaceselection-ns-wrong-label-" + suffix

		ensureNamespaceDeleted(nonMatchingNS)
		ensureCRQDeleted(crqName)

		testNonMatchingNamespaceNotInStatusWithLabels(crqName, nonMatchingNS, map[string]string{labelKey: "other-" + strconv.Itoa(rand.Intn(1000000))}, labelKey, labelValue, false)
	})

	It("Should not add a non-matching namespace to the CRQ status (deletion)", func() {
		suffix := strconv.Itoa(rand.Intn(1000000))
		labelKey := "quota-" + strconv.Itoa(rand.Intn(1000000))
		labelValue := "limited-" + strconv.Itoa(rand.Intn(1000000))
		crqName := "test-namespaceselection-quota-" + suffix
		matchingNS := "test-namespaceselection-ns-" + suffix

		ensureNamespaceDeleted(matchingNS)
		ensureCRQDeleted(crqName)

		testNonMatchingNamespaceNotInStatusWithLabels(crqName, matchingNS, map[string]string{labelKey: labelValue}, labelKey, labelValue, true)
	})

	It("Should update CRQ status when a namespace label is changed to match", func() {
		suffix := strconv.Itoa(rand.Intn(1000000))
		labelKey := "quota-" + strconv.Itoa(rand.Intn(1000000))
		labelValue := "limited-" + strconv.Itoa(rand.Intn(1000000))
		crqName := "test-namespaceselection-quota-" + suffix
		matchingNS := "test-namespaceselection-ns-" + suffix
		nonMatchingNS := "test-namespaceselection-ns-wrong-label-" + suffix

		ensureNamespaceDeleted(matchingNS)
		ensureNamespaceDeleted(nonMatchingNS)
		ensureCRQDeleted(crqName)

		matching := &corev1.Namespace{
			ObjectMeta: metav1.ObjectMeta{
				Name:   matchingNS,
				Labels: map[string]string{labelKey: labelValue},
			},
		}
		nonMatching := &corev1.Namespace{
			ObjectMeta: metav1.ObjectMeta{
				Name:   nonMatchingNS,
				Labels: map[string]string{labelKey: "other-" + strconv.Itoa(rand.Intn(1000000))},
			},
		}
		Expect(k8sClient.Create(ctx, matching)).To(Succeed())
		Expect(k8sClient.Create(ctx, nonMatching)).To(Succeed())
		crq := &quotav1alpha1.ClusterResourceQuota{
			ObjectMeta: metav1.ObjectMeta{
				Name: crqName,
			},
			Spec: quotav1alpha1.ClusterResourceQuotaSpec{
				NamespaceSelector: &metav1.LabelSelector{
					MatchLabels: map[string]string{labelKey: labelValue},
				},
				Hard: quotav1alpha1.ResourceList{},
			},
		}
		Expect(k8sClient.Create(ctx, crq)).To(Succeed())
		DeferCleanup(func() {
			_ = k8sClient.Delete(ctx, matching)
			_ = k8sClient.Delete(ctx, nonMatching)
			_ = k8sClient.Delete(ctx, crq)
		})
		// Change label to match
		Expect(k8sClient.Get(ctx, client.ObjectKey{Name: nonMatchingNS}, nonMatching)).To(Succeed())
		nonMatching.Labels[labelKey] = labelValue
		Expect(k8sClient.Update(ctx, nonMatching)).To(Succeed())
		Eventually(func() []string {
			return getCRQStatusNamespaces(crqName)
		}, "10s", "1s").Should(ContainElement(nonMatchingNS))
	})

	It("Should update CRQ status when a namespace label is changed to exclude it", func() {
		suffix := strconv.Itoa(rand.Intn(1000000))
		labelKey := "quota-" + strconv.Itoa(rand.Intn(1000000))
		labelValue := "limited-" + strconv.Itoa(rand.Intn(1000000))
		crqName := "test-namespaceselection-quota-" + suffix
		matchingNS := "test-namespaceselection-ns-" + suffix

		ensureNamespaceDeleted(matchingNS)
		ensureCRQDeleted(crqName)

		matching := &corev1.Namespace{
			ObjectMeta: metav1.ObjectMeta{
				Name:   matchingNS,
				Labels: map[string]string{labelKey: labelValue},
			},
		}
		Expect(k8sClient.Create(ctx, matching)).To(Succeed())
		crq := &quotav1alpha1.ClusterResourceQuota{
			ObjectMeta: metav1.ObjectMeta{
				Name: crqName,
			},
			Spec: quotav1alpha1.ClusterResourceQuotaSpec{
				NamespaceSelector: &metav1.LabelSelector{
					MatchLabels: map[string]string{labelKey: labelValue},
				},
				Hard: quotav1alpha1.ResourceList{},
			},
		}
		Expect(k8sClient.Create(ctx, crq)).To(Succeed())
		DeferCleanup(func() {
			_ = k8sClient.Delete(ctx, matching)
			_ = k8sClient.Delete(ctx, crq)
		})
		// Remove label to exclude
		Expect(k8sClient.Get(ctx, client.ObjectKey{Name: matchingNS}, matching)).To(Succeed())
		delete(matching.Labels, labelKey)
		Expect(k8sClient.Update(ctx, matching)).To(Succeed())
		Eventually(func() []string {
			return getCRQStatusNamespaces(crqName)
		}, "10s", "1s").ShouldNot(ContainElement(matchingNS))
	})

	It("Should update CRQ status when a namespace is deleted", func() {
		suffix := strconv.Itoa(rand.Intn(1000000))
		labelKey := "quota-" + strconv.Itoa(rand.Intn(1000000))
		labelValue := "limited-" + strconv.Itoa(rand.Intn(1000000))
		crqName := "test-namespaceselection-quota-" + suffix
		matchingNS := "test-namespaceselection-ns-" + suffix

		ensureNamespaceDeleted(matchingNS)
		ensureCRQDeleted(crqName)

		matching := &corev1.Namespace{
			ObjectMeta: metav1.ObjectMeta{
				Name:   matchingNS,
				Labels: map[string]string{labelKey: labelValue},
			},
		}
		Expect(k8sClient.Create(ctx, matching)).To(Succeed())
		crq := &quotav1alpha1.ClusterResourceQuota{
			ObjectMeta: metav1.ObjectMeta{
				Name: crqName,
			},
			Spec: quotav1alpha1.ClusterResourceQuotaSpec{
				NamespaceSelector: &metav1.LabelSelector{
					MatchLabels: map[string]string{labelKey: labelValue},
				},
				Hard: quotav1alpha1.ResourceList{},
			},
		}
		Expect(k8sClient.Create(ctx, crq)).To(Succeed())
		DeferCleanup(func() {
			_ = k8sClient.Delete(ctx, crq)
		})
		_ = k8sClient.Delete(ctx, matching)
		Eventually(func() []string {
			return getCRQStatusNamespaces(crqName)
		}, "10s", "1s").ShouldNot(ContainElement(matchingNS))
	})

	It("Should select multiple namespaces matching the selector", func() {
		suffix := strconv.Itoa(rand.Intn(1000000))
		labelKey := "team-" + strconv.Itoa(rand.Intn(1000000))
		labelValue := "a-" + strconv.Itoa(rand.Intn(1000000))
		crqName := "test-namespaceselection-multi-" + suffix
		ns1 := "test-ns1-" + suffix
		ns2 := "test-ns2-" + suffix
		matching1 := &corev1.Namespace{ObjectMeta: metav1.ObjectMeta{Name: ns1, Labels: map[string]string{labelKey: labelValue}}}
		matching2 := &corev1.Namespace{ObjectMeta: metav1.ObjectMeta{Name: ns2, Labels: map[string]string{labelKey: labelValue}}}
		ensureNamespaceDeleted(ns1)
		ensureNamespaceDeleted(ns2)
		ensureCRQDeleted(crqName)
		Expect(k8sClient.Create(ctx, matching1)).To(Succeed())
		Expect(k8sClient.Create(ctx, matching2)).To(Succeed())
		crq := &quotav1alpha1.ClusterResourceQuota{
			ObjectMeta: metav1.ObjectMeta{Name: crqName},
			Spec: quotav1alpha1.ClusterResourceQuotaSpec{
				NamespaceSelector: &metav1.LabelSelector{MatchLabels: map[string]string{labelKey: labelValue}},
				Hard:              quotav1alpha1.ResourceList{},
			},
		}
		Expect(k8sClient.Create(ctx, crq)).To(Succeed())
		DeferCleanup(func() {
			_ = k8sClient.Delete(ctx, matching1)
			_ = k8sClient.Delete(ctx, matching2)
			_ = k8sClient.Delete(ctx, crq)
		})
		By("Waiting for both namespaces to appear in CRQ status")
		Eventually(func() []string { return getCRQStatusNamespaces(crqName) }, "10s", "1s").Should(ContainElements(ns1, ns2))
	})

	It("Should have empty status.namespaces if selector matches no namespaces", func() {
		suffix := strconv.Itoa(rand.Intn(1000000))
		crqName := "test-namespaceselection-none-" + suffix

		ensureCRQDeleted(crqName)

		crq := &quotav1alpha1.ClusterResourceQuota{
			ObjectMeta: metav1.ObjectMeta{Name: crqName},
			Spec: quotav1alpha1.ClusterResourceQuotaSpec{
				NamespaceSelector: &metav1.LabelSelector{MatchLabels: map[string]string{"foo": "bar"}},
				Hard:              quotav1alpha1.ResourceList{},
			},
		}
		Expect(k8sClient.Create(ctx, crq)).To(Succeed())
		DeferCleanup(func() { _ = k8sClient.Delete(ctx, crq) })
		Consistently(func() []string { return getCRQStatusNamespaces(crqName) }, "5s", "1s").Should(BeEmpty())
	})

	It("Should support matchExpressions in NamespaceSelector", func() {
		suffix := strconv.Itoa(rand.Intn(1000000))
		crqName := "test-namespaceselection-expr-" + suffix
		ns1 := "test-ns1-" + suffix
		ns2 := "test-ns2-" + suffix

		ensureNamespaceDeleted(ns1)
		ensureNamespaceDeleted(ns2)
		ensureCRQDeleted(crqName)

		matching1 := &corev1.Namespace{ObjectMeta: metav1.ObjectMeta{Name: ns1, Labels: map[string]string{"env": "prod"}}}
		matching2 := &corev1.Namespace{ObjectMeta: metav1.ObjectMeta{Name: ns2, Labels: map[string]string{"env": "staging"}}}
		Expect(k8sClient.Create(ctx, matching1)).To(Succeed())
		Expect(k8sClient.Create(ctx, matching2)).To(Succeed())
		crq := &quotav1alpha1.ClusterResourceQuota{
			ObjectMeta: metav1.ObjectMeta{Name: crqName},
			Spec: quotav1alpha1.ClusterResourceQuotaSpec{
				NamespaceSelector: &metav1.LabelSelector{
					MatchExpressions: []metav1.LabelSelectorRequirement{{
						Key: "env", Operator: metav1.LabelSelectorOpIn, Values: []string{"prod", "staging"},
					}},
				},
				Hard: quotav1alpha1.ResourceList{},
			},
		}
		Expect(k8sClient.Create(ctx, crq)).To(Succeed())
		DeferCleanup(func() {
			_ = k8sClient.Delete(ctx, matching1)
			_ = k8sClient.Delete(ctx, matching2)
			_ = k8sClient.Delete(ctx, crq)
		})
		Eventually(func() []string { return getCRQStatusNamespaces(crqName) }, "10s", "1s").Should(ContainElements(ns1, ns2))
	})
})
