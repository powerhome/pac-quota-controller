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

var _ = Describe("ClusterResourceQuota Namespace Selection", func() {
	It("Should create a ClusterResourceQuota and check if it exists", func() {
		suffix := strconv.Itoa(rand.Intn(1000000))
		crqName := "test-namespaceselection-quota-" + suffix
		matchingNS := "test-namespaceselection-ns-" + suffix
		nonMatchingNS := "test-namespaceselection-ns-wrong-label-" + suffix

		matching := &corev1.Namespace{
			ObjectMeta: metav1.ObjectMeta{
				Name:   matchingNS,
				Labels: map[string]string{"quota": "limited"},
			},
		}
		nonMatching := &corev1.Namespace{
			ObjectMeta: metav1.ObjectMeta{
				Name:   nonMatchingNS,
				Labels: map[string]string{"quota": "other"},
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
					MatchLabels: map[string]string{"quota": "limited"},
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

		Eventually(func() error {
			return k8sClient.Get(ctx, client.ObjectKey{Name: crqName}, crq)
		}, "10s", "1s").Should(Succeed())
		Expect(crq.Name).To(Equal(crqName))
	})

	It("Should add a matching namespace to the CRQ status", func() {
		suffix := strconv.Itoa(rand.Intn(1000000))
		crqName := "test-namespaceselection-quota-" + suffix
		matchingNS := "test-namespaceselection-ns-" + suffix

		matching := &corev1.Namespace{
			ObjectMeta: metav1.ObjectMeta{
				Name:   matchingNS,
				Labels: map[string]string{"quota": "limited"},
			},
		}
		Expect(k8sClient.Create(ctx, matching)).To(Succeed())
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
			_ = k8sClient.Delete(ctx, matching)
			_ = k8sClient.Delete(ctx, crq)
		})
		By("Waiting for namespace to appear in CRQ status")
		Eventually(func() []string {
			return getCRQStatusNamespaces(crqName)
		}, "10s", "1s").Should(ContainElement(matchingNS))
	})

	It("Should not add a non-matching namespace to the CRQ status", func() {
		suffix := strconv.Itoa(rand.Intn(1000000))
		crqName := "test-namespaceselection-quota-" + suffix
		nonMatchingNS := "test-namespaceselection-ns-wrong-label-" + suffix

		nonMatching := &corev1.Namespace{
			ObjectMeta: metav1.ObjectMeta{
				Name:   nonMatchingNS,
				Labels: map[string]string{"quota": "other"},
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
			_ = k8sClient.Delete(ctx, nonMatching)
			_ = k8sClient.Delete(ctx, crq)
		})
		Consistently(func() []string {
			return getCRQStatusNamespaces(crqName)
		}, "5s", "1s").ShouldNot(ContainElement(nonMatchingNS))
	})

	It("Should update CRQ status when a namespace label is changed to match", func() {
		suffix := strconv.Itoa(rand.Intn(1000000))
		crqName := "test-namespaceselection-quota-" + suffix
		matchingNS := "test-namespaceselection-ns-" + suffix
		nonMatchingNS := "test-namespaceselection-ns-wrong-label-" + suffix

		matching := &corev1.Namespace{
			ObjectMeta: metav1.ObjectMeta{
				Name:   matchingNS,
				Labels: map[string]string{"quota": "limited"},
			},
		}
		nonMatching := &corev1.Namespace{
			ObjectMeta: metav1.ObjectMeta{
				Name:   nonMatchingNS,
				Labels: map[string]string{"quota": "other"},
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
					MatchLabels: map[string]string{"quota": "limited"},
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
		nonMatching.Labels["quota"] = "limited"
		Expect(k8sClient.Update(ctx, nonMatching)).To(Succeed())
		Eventually(func() []string {
			return getCRQStatusNamespaces(crqName)
		}, "10s", "1s").Should(ContainElement(nonMatchingNS))
	})

	It("Should update CRQ status when a namespace label is changed to exclude it", func() {
		suffix := strconv.Itoa(rand.Intn(1000000))
		crqName := "test-namespaceselection-quota-" + suffix
		matchingNS := "test-namespaceselection-ns-" + suffix

		matching := &corev1.Namespace{
			ObjectMeta: metav1.ObjectMeta{
				Name:   matchingNS,
				Labels: map[string]string{"quota": "limited"},
			},
		}
		Expect(k8sClient.Create(ctx, matching)).To(Succeed())
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
			_ = k8sClient.Delete(ctx, matching)
			_ = k8sClient.Delete(ctx, crq)
		})
		// Remove label to exclude
		Expect(k8sClient.Get(ctx, client.ObjectKey{Name: matchingNS}, matching)).To(Succeed())
		delete(matching.Labels, "quota")
		Expect(k8sClient.Update(ctx, matching)).To(Succeed())
		Eventually(func() []string {
			return getCRQStatusNamespaces(crqName)
		}, "10s", "1s").ShouldNot(ContainElement(matchingNS))
	})

	It("Should update CRQ status when a namespace is deleted", func() {
		suffix := strconv.Itoa(rand.Intn(1000000))
		crqName := "test-namespaceselection-quota-" + suffix
		nonMatchingNS := "test-namespaceselection-ns-wrong-label-" + suffix

		nonMatching := &corev1.Namespace{
			ObjectMeta: metav1.ObjectMeta{
				Name:   nonMatchingNS,
				Labels: map[string]string{"quota": "other"},
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
			_ = k8sClient.Delete(ctx, nonMatching)
			_ = k8sClient.Delete(ctx, crq)
		})
		_ = k8sClient.Delete(ctx, nonMatching)
		Eventually(func() []string {
			return getCRQStatusNamespaces(crqName)
		}, "10s", "1s").ShouldNot(ContainElement(nonMatchingNS))
	})
})
