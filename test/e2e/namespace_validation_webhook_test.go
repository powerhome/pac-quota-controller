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
	"context"
	"fmt"
	"time"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	"k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"

	quotav1alpha1 "github.com/powerhome/pac-quota-controller/api/v1alpha1"
	testutils "github.com/powerhome/pac-quota-controller/test/utils"
)

var _ = Describe("NamespaceValidationWebhook", func() {
	var (
		ctx context.Context

		baseLabels map[string]string
		suffix     string
		testNsName string
		crq1Name   string
		crq2Name   string
	)

	BeforeEach(func() {
		ctx = context.Background()
		suffix = generateTestSuffix()
		testNsName = "test-ns-webhook-" + suffix
		crq1Name = "crq1-" + suffix
		crq2Name = "crq2-" + suffix
		baseLabels = map[string]string{"e2e-test": "namespace-validation-" + suffix}
	})

	Context("When updating a Namespace", func() {

		It("should allow update if labels do not change", func() {
			err := testutils.CreateNamespace(ctx, k8sClient, testNsName, baseLabels)
			Expect(err).NotTo(HaveOccurred())
			DeferCleanup(testutils.DeleteNamespace, ctx, k8sClient, testNsName)

			// Modify an annotation, keeping labels the same
			Eventually(func() error {
				updatedNs, getErr := testutils.GetNamespace(ctx, k8sClient, testNsName)
				if getErr != nil {
					return getErr
				}
				if updatedNs.Annotations == nil {
					updatedNs.Annotations = make(map[string]string)
				}
				updatedNs.Annotations["updated"] = "true"
				return k8sClient.Update(ctx, updatedNs)
			}, time.Minute, 5*time.Second).Should(Succeed())
		})

		It("should allow update if new labels match no CRQs", func() {
			crq1 := &quotav1alpha1.ClusterResourceQuota{
				ObjectMeta: metav1.ObjectMeta{Name: crq1Name},
				Spec: quotav1alpha1.ClusterResourceQuotaSpec{
					NamespaceSelector: &metav1.LabelSelector{
						MatchLabels: map[string]string{
							"app": "backend-" + suffix,
						}},
					Hard: quotav1alpha1.ResourceList{},
				},
			}
			Expect(testutils.CreateClusterResourceQuota(ctx, k8sClient, crq1)).To(Succeed())
			DeferCleanup(testutils.DeleteClusterResourceQuota, ctx, k8sClient, crq1)

			err := testutils.CreateNamespace(ctx, k8sClient, testNsName, map[string]string{"app": "frontend-" + suffix})
			Expect(err).NotTo(HaveOccurred())
			DeferCleanup(testutils.DeleteNamespace, ctx, k8sClient, testNsName)

			Eventually(func() error {
				updatedNs, getErr := testutils.GetNamespace(ctx, k8sClient, testNsName)
				if getErr != nil {
					return getErr
				}
				updatedNs.Labels = map[string]string{"app": "database-" + suffix} // Matches no CRQs
				return k8sClient.Update(ctx, updatedNs)
			}, time.Minute, 5*time.Second).Should(Succeed())
		})

		It("should allow update if new labels match exactly one CRQ", func() {
			crq1 := &quotav1alpha1.ClusterResourceQuota{
				ObjectMeta: metav1.ObjectMeta{Name: crq1Name},
				Spec: quotav1alpha1.ClusterResourceQuotaSpec{
					NamespaceSelector: &metav1.LabelSelector{
						MatchLabels: map[string]string{
							"app": "backend-" + suffix,
						}},
					Hard: quotav1alpha1.ResourceList{},
				},
			}
			Expect(testutils.CreateClusterResourceQuota(ctx, k8sClient, crq1)).To(Succeed())
			DeferCleanup(testutils.DeleteClusterResourceQuota, ctx, k8sClient, crq1)

			crq2 := &quotav1alpha1.ClusterResourceQuota{
				ObjectMeta: metav1.ObjectMeta{Name: crq2Name},
				Spec: quotav1alpha1.ClusterResourceQuotaSpec{
					NamespaceSelector: &metav1.LabelSelector{
						MatchLabels: map[string]string{
							"env": "prod-" + suffix,
						}},
					Hard: quotav1alpha1.ResourceList{},
				},
			}
			Expect(testutils.CreateClusterResourceQuota(ctx, k8sClient, crq2)).To(Succeed())
			DeferCleanup(testutils.DeleteClusterResourceQuota, ctx, k8sClient, crq2)

			err := testutils.CreateNamespace(ctx, k8sClient, testNsName, map[string]string{"app": "frontend-" + suffix})
			Expect(err).NotTo(HaveOccurred())
			DeferCleanup(testutils.DeleteNamespace, ctx, k8sClient, testNsName)

			Eventually(func() error {
				updatedNs, getErr := testutils.GetNamespace(ctx, k8sClient, testNsName)
				if getErr != nil {
					return getErr
				}
				updatedNs.Labels = map[string]string{"app": "backend-" + suffix} // Matches crq1 only
				return k8sClient.Update(ctx, updatedNs)
			}, time.Minute, 5*time.Second).Should(Succeed())
		})

		It("should deny update if new labels would cause matching multiple CRQs", func() {
			crq1 := &quotav1alpha1.ClusterResourceQuota{
				ObjectMeta: metav1.ObjectMeta{Name: crq1Name},
				Spec: quotav1alpha1.ClusterResourceQuotaSpec{
					NamespaceSelector: &metav1.LabelSelector{
						MatchLabels: map[string]string{
							"app": "backend-" + suffix,
						}},
					Hard: quotav1alpha1.ResourceList{},
				},
			}
			Expect(testutils.CreateClusterResourceQuota(ctx, k8sClient, crq1)).To(Succeed())
			DeferCleanup(testutils.DeleteClusterResourceQuota, ctx, k8sClient, crq1)

			crq2 := &quotav1alpha1.ClusterResourceQuota{
				ObjectMeta: metav1.ObjectMeta{Name: crq2Name},
				Spec: quotav1alpha1.ClusterResourceQuotaSpec{
					NamespaceSelector: &metav1.LabelSelector{
						MatchLabels: map[string]string{
							"env": "prod-" + suffix,
						}},
					Hard: quotav1alpha1.ResourceList{},
				},
			}
			Expect(testutils.CreateClusterResourceQuota(ctx, k8sClient, crq2)).To(Succeed())
			DeferCleanup(testutils.DeleteClusterResourceQuota, ctx, k8sClient, crq2)

			err := testutils.CreateNamespace(ctx, k8sClient, testNsName, map[string]string{"app": "frontend-" + suffix})
			Expect(err).NotTo(HaveOccurred())
			DeferCleanup(testutils.DeleteNamespace, ctx, k8sClient, testNsName)

			var updateErr error
			Eventually(func() bool {
				updatedNs, getErr := testutils.GetNamespace(ctx, k8sClient, testNsName)
				if getErr != nil {
					return false // retry
				}
				updatedNs.Labels = map[string]string{
					"app": "backend-" + suffix,
					"env": "prod-" + suffix,
				} // Matches crq1 and crq2
				updateErr = k8sClient.Update(ctx, updatedNs)
				return updateErr != nil
			}, time.Minute, 5*time.Second).Should(BeTrue(), "Update should eventually fail")

			Expect(updateErr).To(HaveOccurred())
			Expect(errors.IsForbidden(updateErr)).To(BeTrue(), "error should be a forbidden error")
			Expect(updateErr.Error()).To(SatisfyAll(
				ContainSubstring(fmt.Sprintf("multiple ClusterResourceQuotas select namespace \"%s\"", testNsName)),
				ContainSubstring(crq1Name),
				ContainSubstring(crq2Name),
			))
		})
	})
	Context("Namespace Create Validation", func() {
		It("should allow creation when no CRQs match", func() {
			suffix := generateTestSuffix()
			testNsName := "ns-create-" + suffix
			err := testutils.CreateNamespace(
				ctx,
				k8sClient,
				testNsName,
				map[string]string{
					"app": "foo-" + suffix,
					"env": "bar-" + suffix,
				},
			)
			Expect(err).NotTo(HaveOccurred())
			DeferCleanup(testutils.DeleteNamespace, ctx, k8sClient, testNsName)
		})

		It("should allow creation when only one CRQ matches", func() {
			crq := &quotav1alpha1.ClusterResourceQuota{
				ObjectMeta: metav1.ObjectMeta{Name: crq1Name},
				Spec: quotav1alpha1.ClusterResourceQuotaSpec{
					NamespaceSelector: &metav1.LabelSelector{
						MatchLabels: map[string]string{
							"app": "foo-" + suffix,
						},
					},
					Hard: quotav1alpha1.ResourceList{},
				},
			}
			Expect(testutils.CreateClusterResourceQuota(ctx, k8sClient, crq)).To(Succeed())
			DeferCleanup(testutils.DeleteClusterResourceQuota, ctx, k8sClient, crq)
			err := testutils.CreateNamespace(ctx, k8sClient, testNsName, map[string]string{"app": "foo-" + suffix})
			Expect(err).NotTo(HaveOccurred())
			DeferCleanup(testutils.DeleteNamespace, ctx, k8sClient, testNsName)
		})

		It("should deny creation when multiple CRQs with different selectors match", func() {
			// Create two CRQs with different label selectors
			crq1 := &quotav1alpha1.ClusterResourceQuota{
				ObjectMeta: metav1.ObjectMeta{Name: crq1Name},
				Spec: quotav1alpha1.ClusterResourceQuotaSpec{
					NamespaceSelector: &metav1.LabelSelector{
						MatchLabels: map[string]string{"app": "multi-selector-test"}},
					Hard: quotav1alpha1.ResourceList{},
				},
			}
			crq2 := &quotav1alpha1.ClusterResourceQuota{
				ObjectMeta: metav1.ObjectMeta{Name: crq2Name},
				Spec: quotav1alpha1.ClusterResourceQuotaSpec{
					NamespaceSelector: &metav1.LabelSelector{MatchLabels: map[string]string{"env": "e2e"}},
					Hard:              quotav1alpha1.ResourceList{},
				},
			}

			Expect(testutils.CreateClusterResourceQuota(ctx, k8sClient, crq1)).To(Succeed())
			DeferCleanup(testutils.DeleteClusterResourceQuota, ctx, k8sClient, crq1)
			Expect(testutils.CreateClusterResourceQuota(ctx, k8sClient, crq2)).To(Succeed())
			DeferCleanup(testutils.DeleteClusterResourceQuota, ctx, k8sClient, crq2)

			// Attempt to create a namespace that matches both selectors
			err := testutils.CreateNamespace(ctx, k8sClient, testNsName, map[string]string{
				"app": "multi-selector-test",
				"env": "e2e",
			})

			Expect(err).To(HaveOccurred())
			Expect(errors.IsForbidden(err)).To(BeTrue())
			Expect(err.Error()).To(SatisfyAll(
				ContainSubstring(fmt.Sprintf("multiple ClusterResourceQuotas select namespace \"%s\"", testNsName)),
				ContainSubstring(crq1Name),
				ContainSubstring(crq2Name),
			))
		})

		It("should deny creation when multiple CRQs match", func() {
			crq1 := &quotav1alpha1.ClusterResourceQuota{
				ObjectMeta: metav1.ObjectMeta{Name: crq1Name},
				Spec: quotav1alpha1.ClusterResourceQuotaSpec{
					NamespaceSelector: &metav1.LabelSelector{
						MatchLabels: map[string]string{
							"app": "foo-" + suffix,
						},
					},
					Hard: quotav1alpha1.ResourceList{},
				},
			}
			crq2 := &quotav1alpha1.ClusterResourceQuota{
				ObjectMeta: metav1.ObjectMeta{Name: crq2Name},
				Spec: quotav1alpha1.ClusterResourceQuotaSpec{
					NamespaceSelector: &metav1.LabelSelector{
						MatchLabels: map[string]string{

							"app": "foo-" + suffix,
						},
					},
					Hard: quotav1alpha1.ResourceList{},
				},
			}
			Expect(testutils.CreateClusterResourceQuota(ctx, k8sClient, crq1)).To(Succeed())
			Expect(testutils.CreateClusterResourceQuota(ctx, k8sClient, crq2)).To(Succeed())
			DeferCleanup(testutils.DeleteClusterResourceQuota, ctx, k8sClient, crq1)
			DeferCleanup(testutils.DeleteClusterResourceQuota, ctx, k8sClient, crq2)
			err := testutils.CreateNamespace(ctx, k8sClient, testNsName, map[string]string{"app": "foo-" + suffix})
			Expect(err).To(HaveOccurred())
			Expect(errors.IsForbidden(err)).To(BeTrue())
			Expect(err.Error()).To(ContainSubstring("multiple ClusterResourceQuotas select namespace"))
		})
	})

	Context("Namespace Delete Validation", func() {
		It("should always allow deletion (webhook doesn't validate deletes for this rule)", func() {
			err := testutils.CreateNamespace(
				ctx,
				k8sClient,
				testNsName,
				map[string]string{
					"e2e-test": "delete-validation-" + generateTestSuffix(),
				},
			)
			Expect(err).NotTo(HaveOccurred())

			Expect(testutils.DeleteNamespace(ctx, k8sClient, testNsName)).To(Succeed())

			// Verify deletion
			Eventually(func() bool {
				_, getErr := testutils.GetNamespace(ctx, k8sClient, testNsName)
				return errors.IsNotFound(getErr)
			}, time.Minute, 5*time.Second).Should(BeTrue())
		})
	})
})
