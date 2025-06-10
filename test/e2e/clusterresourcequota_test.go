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
	"k8s.io/apimachinery/pkg/api/resource"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"sigs.k8s.io/controller-runtime/pkg/client"
)

var _ = Describe("ClusterResourceQuota", func() {
	It("should create a ClusterResourceQuota and update its status with matching namespaces", func() {
		suffix := strconv.Itoa(rand.Intn(1000000))
		testNamespace := "test-clusterresourcequota-ns-" + suffix
		quotaName := "test-clusterresourcequota-quota-" + suffix

		ns := &corev1.Namespace{
			ObjectMeta: metav1.ObjectMeta{
				Name:   testNamespace,
				Labels: map[string]string{"quota": "limited"},
			},
		}
		Expect(k8sClient.Create(ctx, ns)).To(Succeed(), "Failed to create test namespace")

		crq := &quotav1alpha1.ClusterResourceQuota{
			ObjectMeta: metav1.ObjectMeta{
				Name: quotaName,
			},
			Spec: quotav1alpha1.ClusterResourceQuotaSpec{
				NamespaceSelector: &metav1.LabelSelector{
					MatchLabels: map[string]string{"quota": "limited"},
				},
				Hard: quotav1alpha1.ResourceList{
					corev1.ResourcePods: resourceMustParse("10"),
				},
			},
		}
		Expect(k8sClient.Create(ctx, crq)).To(Succeed(), "Failed to create ClusterResourceQuota")

		DeferCleanup(func() {
			_ = k8sClient.Delete(ctx, ns, client.PropagationPolicy(metav1.DeletePropagationForeground))
			_ = k8sClient.Delete(ctx, crq, client.PropagationPolicy(metav1.DeletePropagationForeground))
		})

		By("Verifying that the ClusterResourceQuota exists")
		fetched := &quotav1alpha1.ClusterResourceQuota{}
		Eventually(func() error {
			return k8sClient.Get(ctx, client.ObjectKey{Name: quotaName}, fetched)
		}, "10s", "1s").Should(Succeed())

		By("Verifying that the status field is updated with matching namespaces")
		Eventually(func() []string {
			_ = k8sClient.Get(ctx, client.ObjectKey{Name: quotaName}, fetched)
			if fetched.Status.Namespaces == nil {
				return nil
			}
			var nsList []string
			for _, ns := range fetched.Status.Namespaces {
				nsList = append(nsList, ns.Namespace)
			}
			return nsList
		}, "10s", "1s").Should(ContainElement(testNamespace))
	})
})

func resourceMustParse(val string) resource.Quantity {
	return resource.MustParse(val)
}
