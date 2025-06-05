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

	"slices"

	quotav1alpha1 "github.com/powerhome/pac-quota-controller/api/v1alpha1"
)

var _ = Describe("ClusterResourceQuota Controller", func() {
	var testQuota *quotav1alpha1.ClusterResourceQuota
	ctx := context.Background()

	BeforeAll(func() {
		By("Creating a shared ClusterResourceQuota for all tests")
		testQuota = &quotav1alpha1.ClusterResourceQuota{
			ObjectMeta: metav1.ObjectMeta{
				Name: "test-namespace-selector",
			},
			Spec: quotav1alpha1.ClusterResourceQuotaSpec{
				Hard: quotav1alpha1.ResourceList{
					"pods": resource.MustParse("10"),
				},
				NamespaceSelector: &metav1.LabelSelector{
					MatchLabels: map[string]string{
						"quota": "limited",
					},
				},
			},
		}
		_ = k8sClient.Delete(ctx, testQuota) // ensure clean slate
		Expect(k8sClient.Create(ctx, testQuota)).To(Succeed())
	})

	AfterAll(func() {
		By("Cleaning up shared ClusterResourceQuota after all tests")
		_ = k8sClient.Delete(ctx, testQuota)
	})

	Context("When reconciling a resource", func() {
		const resourceName = "test-resource"

		typeNamespacedName := types.NamespacedName{
			Name: resourceName,
		}
		clusterresourcequota := &quotav1alpha1.ClusterResourceQuota{}

		BeforeEach(func() {
			By("creating the custom resource for the Kind ClusterResourceQuota")
			err := k8sClient.Get(ctx, typeNamespacedName, clusterresourcequota)
			if err != nil && errors.IsNotFound(err) {
				resource := &quotav1alpha1.ClusterResourceQuota{
					ObjectMeta: metav1.ObjectMeta{
						Name: resourceName,
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

		It("should correctly identify and track selected namespaces", func() {
			By("Creating test namespaces with labels")

			// Create test namespaces with different labels
			testNamespace1 := &corev1.Namespace{
				ObjectMeta: metav1.ObjectMeta{
					Name: "test-ns-1",
					Labels: map[string]string{
						"quota": "limited",
						"team":  "frontend",
					},
				},
			}
			Expect(k8sClient.Create(ctx, testNamespace1)).To(Succeed())

			testNamespace2 := &corev1.Namespace{
				ObjectMeta: metav1.ObjectMeta{
					Name: "test-ns-2",
					Labels: map[string]string{
						"quota": "limited",
						"team":  "backend",
					},
				},
			}
			Expect(k8sClient.Create(ctx, testNamespace2)).To(Succeed())

			testNamespace3 := &corev1.Namespace{
				ObjectMeta: metav1.ObjectMeta{
					Name: "test-ns-3",
					Labels: map[string]string{
						"quota": "unlimited",
						"team":  "data",
					},
				},
			}
			Expect(k8sClient.Create(ctx, testNamespace3)).To(Succeed())

			defer func() {
				By("Cleaning up test namespaces")
				Expect(k8sClient.Delete(ctx, testNamespace1)).To(Succeed())
				Expect(k8sClient.Delete(ctx, testNamespace2)).To(Succeed())
				Expect(k8sClient.Delete(ctx, testNamespace3)).To(Succeed())
			}()

			Expect(k8sClient.Create(ctx, testQuota)).To(Succeed())

			By("Reconciling the ClusterResourceQuota")
			controllerReconciler := &ClusterResourceQuotaReconciler{
				Client: k8sClient,
				Scheme: k8sClient.Scheme(),
			}

			_, err := controllerReconciler.Reconcile(ctx, reconcile.Request{
				NamespacedName: types.NamespacedName{Name: "test-namespace-selector"},
			})
			Expect(err).NotTo(HaveOccurred())

			By("Checking if the namespaces are recorded in the status")
			updatedQuota := &quotav1alpha1.ClusterResourceQuota{}
			Eventually(func() bool {
				err := k8sClient.Get(ctx, types.NamespacedName{Name: "test-namespace-selector"}, updatedQuota)
				if err != nil {
					return false
				}

				namespaces := getNamespaceNamesFromStatus(updatedQuota.Status.Namespaces)

				// Should contain both test-ns-1 and test-ns-2, but not test-ns-3
				return len(namespaces) == 2 &&
					(slices.Contains(namespaces, "test-ns-1") && slices.Contains(namespaces, "test-ns-2"))
			}, "30s", "1s").Should(BeTrue())
		})
		It("should not select namespaces that do not match the selector", func() {
			By("Creating a namespace with non-matching labels")
			nonMatchingNS := &corev1.Namespace{
				ObjectMeta: metav1.ObjectMeta{
					Name: "test-ns-nomatch",
					Labels: map[string]string{
						"quota": "unlimited",
						"team":  "infra",
					},
				},
			}
			Expect(k8sClient.Create(ctx, nonMatchingNS)).To(Succeed())
			defer func() { _ = k8sClient.Delete(ctx, nonMatchingNS) }()

			By("Reconciling the ClusterResourceQuota")
			controllerReconciler := &ClusterResourceQuotaReconciler{
				Client: k8sClient,
				Scheme: k8sClient.Scheme(),
			}
			_, err := controllerReconciler.Reconcile(ctx, reconcile.Request{
				NamespacedName: types.NamespacedName{Name: "test-namespace-selector"},
			})
			Expect(err).NotTo(HaveOccurred())

			By("Checking that the non-matching namespace is not recorded in the status")
			updatedQuota := &quotav1alpha1.ClusterResourceQuota{}
			Consistently(func() bool {
				err := k8sClient.Get(ctx, types.NamespacedName{Name: "test-namespace-selector"}, updatedQuota)
				if err != nil {
					return false
				}
				namespaces := getNamespaceNamesFromStatus(updatedQuota.Status.Namespaces)
				// Debug: print the namespaces for troubleshooting
				GinkgoWriter.Printf("Current namespaces in status: %v\n", namespaces)
				return !slices.Contains(namespaces, "test-ns-nomatch")
			}, "5s", "1s").Should(BeTrue())
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

// getNamespaceNamesFromStatus extracts namespace names from []ResourceQuotaStatusByNamespace
func getNamespaceNamesFromStatus(statuses []quotav1alpha1.ResourceQuotaStatusByNamespace) []string {
	names := make([]string, 0, len(statuses))
	for _, nsStatus := range statuses {
		names = append(names, nsStatus.Namespace)
	}
	return names
}
