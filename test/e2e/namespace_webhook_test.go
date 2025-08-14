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
	"fmt"
	"time"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	"k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"

	quotav1alpha1 "github.com/powerhome/pac-quota-controller/api/v1alpha1"
	testutils "github.com/powerhome/pac-quota-controller/test/utils"
)

var _ = Describe("Namespace Webhook", func() {
	var (
		suffix     string
		testNsName string
		crq1       *quotav1alpha1.ClusterResourceQuota
		crq2       *quotav1alpha1.ClusterResourceQuota
	)

	BeforeEach(func() {
		suffix = testutils.GenerateTestSuffix()
		testNsName = "test-ns-webhook-" + suffix

		var err error
		crq1, err = testutils.CreateClusterResourceQuota(ctx, k8sClient, "crq1-"+suffix, &metav1.LabelSelector{
			MatchLabels: map[string]string{
				"app": "backend-" + suffix,
			},
		}, quotav1alpha1.ResourceList{})
		Expect(err).NotTo(HaveOccurred())

		crq2, err = testutils.CreateClusterResourceQuota(ctx, k8sClient, "crq2-"+suffix, &metav1.LabelSelector{
			MatchLabels: map[string]string{
				"env": "prod-" + suffix,
			},
		}, quotav1alpha1.ResourceList{})
		Expect(err).NotTo(HaveOccurred())
	})

	AfterEach(func() {
		DeferCleanup(func() {
			_ = k8sClient.Delete(ctx, crq1)
		})
		DeferCleanup(func() {
			_ = k8sClient.Delete(ctx, crq2)
		})
	})

	Context("Namespace Validation", func() {
		It("should allow update if labels do not change", func() {
			By("Creating a namespace with initial labels")
			namespace, err := testutils.CreateNamespace(
				ctx,
				k8sClient,
				testNsName,
				map[string]string{
					"e2e-test": "namespace-validation-" + suffix,
				},
			)
			DeferCleanup(func() {
				_ = k8sClient.Delete(ctx, namespace)
			})
			Expect(err).NotTo(HaveOccurred())
			Expect(namespace).NotTo(BeNil())

			By("Updating an annotation without changing labels")
			Eventually(func() error {
				namespace.Annotations = map[string]string{"updated": "true"}
				return k8sClient.Update(ctx, namespace)
			}, time.Minute, 5*time.Second).Should(Succeed())
		})

		It("should allow update if new labels match no CRQs", func() {
			By("Creating a namespace with labels that do not match the CRQ")
			ns, err := testutils.CreateNamespace(
				ctx,
				k8sClient,
				testNsName,
				map[string]string{"app": "frontend-" + suffix},
			)
			Expect(err).NotTo(HaveOccurred())
			Expect(ns).NotTo(BeNil())

			DeferCleanup(func() {
				_ = k8sClient.Delete(ctx, ns)
			})

			By("Updating the namespace labels to a new value that matches no CRQs")
			Eventually(func() error {
				ns.Labels = map[string]string{"app": "database-" + suffix} // Matches no CRQs
				return k8sClient.Update(ctx, ns)
			}, time.Minute, 5*time.Second).Should(Succeed())
		})

		It("should allow update if new labels match exactly one CRQ", func() {
			By("Creating a namespace with labels that match the CRQ app=backend-" + suffix)
			ns, err := testutils.CreateNamespace(
				ctx,
				k8sClient,
				testNsName,
				map[string]string{"app": "frontend-" + suffix},
			)
			Expect(err).NotTo(HaveOccurred())
			Expect(ns).NotTo(BeNil())

			DeferCleanup(func() {
				_ = k8sClient.Delete(ctx, ns)
			})

			By("Updating the namespace labels to a new value that matches exactly one CRQ")
			Eventually(func() error {
				ns.Labels = map[string]string{"app": "backend-" + suffix} // Matches crq1 only
				return k8sClient.Update(ctx, ns)
			}, time.Minute, 5*time.Second).Should(Succeed())
		})

		It("should deny update if new labels would cause matching multiple CRQs", func() {
			By("Creating a namespace with labels that match one CRQs")
			ns, err := testutils.CreateNamespace(
				ctx,
				k8sClient,
				testNsName,
				map[string]string{"app": "frontend-" + suffix},
			)
			Expect(err).NotTo(HaveOccurred())
			Expect(ns).NotTo(BeNil())

			DeferCleanup(func() {
				_ = k8sClient.Delete(ctx, ns)
			})

			var updateErr error
			By("Updating the namespace labels to a new value that matches multiple CRQs")
			Eventually(func() bool {
				ns.Labels = map[string]string{
					"app": "backend-" + suffix,
					"env": "prod-" + suffix,
				} // Matches crq1 and crq2
				updateErr = k8sClient.Update(ctx, ns)
				return updateErr != nil
			}, time.Minute, 5*time.Second).Should(BeTrue(), "Update should eventually fail")

			Expect(updateErr).To(HaveOccurred())
			Expect(errors.IsForbidden(updateErr)).To(BeTrue(), "error should be a forbidden error")
			Expect(updateErr.Error()).To(SatisfyAll(
				ContainSubstring(fmt.Sprintf("multiple ClusterResourceQuotas select namespace \"%s\"", testNsName)),
				ContainSubstring(crq1.Name),
				ContainSubstring(crq2.Name),
			))
		})
		Context("Namespace Create Validation", func() {
			It("should allow creation when no CRQs match", func() {
				ns, err := testutils.CreateNamespace(
					ctx,
					k8sClient,
					testNsName,
					map[string]string{
						"app": "foo-" + suffix,
						"env": "bar-" + suffix,
					},
				)
				Expect(err).NotTo(HaveOccurred())
				Expect(ns).NotTo(BeNil())

				DeferCleanup(func() {
					_ = k8sClient.Delete(ctx, ns)
				})
			})

			It("should allow creation when only one CRQ matches", func() {
				By("Creating a namespace with labels that match the CRQ")
				ns, err := testutils.CreateNamespace(
					ctx,
					k8sClient,
					testNsName,
					map[string]string{"app": "foo-" + suffix},
				)
				Expect(err).NotTo(HaveOccurred())
				Expect(ns).NotTo(BeNil())

				DeferCleanup(func() {
					_ = k8sClient.Delete(ctx, ns)
				})
			})

			It("should deny creation when multiple CRQs with different selectors match", func() {
				_, err := testutils.CreateNamespace(ctx, k8sClient, testNsName, map[string]string{
					"app": "backend-" + suffix,
					"env": "prod-" + suffix,
				})

				Expect(err).To(HaveOccurred())
				Expect(errors.IsForbidden(err)).To(BeTrue())
				Expect(err.Error()).To(SatisfyAll(
					ContainSubstring(fmt.Sprintf("multiple ClusterResourceQuotas select namespace \"%s\"", testNsName)),
					ContainSubstring(crq1.Name),
					ContainSubstring(crq2.Name),
				))
			})

			Context("Namespace Delete Validation", func() {
				It("should always allow deletion (webhook doesn't validate deletes for this rule)", func() {
					ns, err := testutils.CreateNamespace(
						ctx,
						k8sClient,
						testNsName,
						map[string]string{
							"e2e-test": "delete-validation-" + testutils.GenerateTestSuffix(),
						},
					)
					Expect(err).NotTo(HaveOccurred())

					Expect(k8sClient.Delete(ctx, ns)).To(Succeed())

					// Verify deletion
					Eventually(func() bool {
						_, getErr := testutils.GetNamespace(ctx, k8sClient, testNsName)
						return errors.IsNotFound(getErr)
					}, time.Minute, 5*time.Second).Should(BeTrue())
				})
			})
		})
	})
})
