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

package controller

import (
	"context"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/api/resource"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"
	"sigs.k8s.io/controller-runtime/pkg/reconcile"

	quotav1alpha1 "github.com/powerhome/pac-quota-controller/api/v1alpha1"
)

var _ = Describe("ClusterResourceQuota Controller", func() {
	Context("When reconciling a resource", func() {
		const resourceName = "test-resource"

		ctx := context.Background()

		typeNamespacedName := types.NamespacedName{
			Name: resourceName,
			// ClusterResourceQuota is cluster-scoped, so no namespace
		}
		clusterresourcequota := &quotav1alpha1.ClusterResourceQuota{}

		BeforeEach(func() {
			By("creating the custom resource for the Kind ClusterResourceQuota")
			err := k8sClient.Get(ctx, typeNamespacedName, clusterresourcequota)
			if err != nil && errors.IsNotFound(err) {
				resource := &quotav1alpha1.ClusterResourceQuota{
					ObjectMeta: metav1.ObjectMeta{
						Name: resourceName,
						// Note: ClusterResourceQuota is cluster-scoped, so no namespace
					},
					Spec: quotav1alpha1.ClusterResourceQuotaSpec{
						Hard: quotav1alpha1.ResourceList{
							"pods":            resource.MustParse("10"),
							"requests.cpu":    resource.MustParse("1"),
							"requests.memory": resource.MustParse("1Gi"),
							"limits.cpu":      resource.MustParse("2"),
							"limits.memory":   resource.MustParse("2Gi"),
						},
						NamespaceSelector: &metav1.LabelSelector{
							MatchLabels: map[string]string{
								"quota": "limited",
							},
						},
						Scopes: []corev1.ResourceQuotaScope{
							corev1.ResourceQuotaScopeNotTerminating,
						},
					},
				}
				Expect(k8sClient.Create(ctx, resource)).To(Succeed())
			}
		})

		AfterEach(func() {
			// TODO(user): Cleanup logic after each test, like removing the resource instance.
			resource := &quotav1alpha1.ClusterResourceQuota{}
			err := k8sClient.Get(ctx, typeNamespacedName, resource)
			Expect(err).NotTo(HaveOccurred())

			By("Cleanup the specific resource instance ClusterResourceQuota")
			Expect(k8sClient.Delete(ctx, resource)).To(Succeed())
		})
		It("should successfully reconcile the resource", func() {
			By("Reconciling the created resource")
			controllerReconciler := &ClusterResourceQuotaReconciler{
				Client: k8sClient,
				Scheme: k8sClient.Scheme(),
			}

			_, err := controllerReconciler.Reconcile(ctx, reconcile.Request{
				NamespacedName: typeNamespacedName,
			})
			Expect(err).NotTo(HaveOccurred())

			// Verify the resource can be fetched
			fetchedResource := &quotav1alpha1.ClusterResourceQuota{}
			err = k8sClient.Get(ctx, typeNamespacedName, fetchedResource)
			Expect(err).NotTo(HaveOccurred())

			// Since our controller doesn't have any real logic yet,
			// we'll just verify the resource exists and its name matches what we expect
			Expect(fetchedResource.Name).To(Equal(resourceName))
			// Note: Other fields would be checked in a full implementation
		})
		It("should handle ScopeSelector field", func() {
			By("Creating a ClusterResourceQuota with ScopeSelector")
			resourceWithSelector := &quotav1alpha1.ClusterResourceQuota{
				ObjectMeta: metav1.ObjectMeta{
					Name: "test-scope-selector",
				},
				Spec: quotav1alpha1.ClusterResourceQuotaSpec{
					Hard: quotav1alpha1.ResourceList{
						"pods": resource.MustParse("5"),
					},
					NamespaceSelector: &metav1.LabelSelector{
						MatchLabels: map[string]string{
							"environment": "test",
						},
					},
					ScopeSelector: &corev1.ScopeSelector{
						MatchExpressions: []corev1.ScopedResourceSelectorRequirement{
							{
								ScopeName: corev1.ResourceQuotaScopePriorityClass,
								Operator:  corev1.ScopeSelectorOpIn,
								Values:    []string{"high"},
							},
						},
					},
				},
			}

			// Create the resource
			Expect(k8sClient.Create(ctx, resourceWithSelector)).To(Succeed())

			// Clean up after test
			defer func() {
				Expect(k8sClient.Delete(ctx, resourceWithSelector)).To(Succeed())
			}() // Verify it was created correctly
			fetchedResource := &quotav1alpha1.ClusterResourceQuota{}
			Eventually(func() error {
				return k8sClient.Get(ctx, types.NamespacedName{Name: "test-scope-selector"}, fetchedResource)
			}, "30s", "1s").Should(Succeed())

			// Since our controller doesn't modify the resource,
			// we'll just check that it exists and has the correct name
			Expect(fetchedResource.Name).To(Equal("test-scope-selector"))
		})
	})
})
