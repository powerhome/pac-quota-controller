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
	"time"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	quotav1alpha1 "github.com/powerhome/pac-quota-controller/api/v1alpha1"
	testutils "github.com/powerhome/pac-quota-controller/test/utils"
	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/resource"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"
)

var _ = Describe("ClusterResourceQuota Webhook", func() {
	var (
		ctx     = context.Background()
		suffix  string
		crqName string
	)

	BeforeEach(func() {
		suffix = testutils.GenerateTestSuffix()
		crqName = testutils.GenerateResourceName("test-crq")
	})

	Context("Create scenarios", func() {
		It("should create a ClusterResourceQuota with valid spec", func() {
			By("Creating a ClusterResourceQuota with valid spec")
			crq, err := testutils.CreateClusterResourceQuota(ctx, k8sClient, crqName, &metav1.LabelSelector{
				MatchLabels: map[string]string{"quota": "limited-" + suffix},
			}, quotav1alpha1.ResourceList{
				corev1.ResourcePods:   resource.MustParse("10"),
				corev1.ResourceCPU:    resource.MustParse("2"),
				corev1.ResourceMemory: resource.MustParse("4Gi"),
			})
			Expect(err).ToNot(HaveOccurred())

			By("Cleaning up the ClusterResourceQuota")
			Expect(k8sClient.Delete(ctx, crq)).To(Succeed())
		})
	})

	Context("Update scenarios", func() {
		It("should update a ClusterResourceQuota spec successfully", func() {
			By("Creating a ClusterResourceQuota with initial spec")
			_, err := testutils.CreateClusterResourceQuota(
				ctx,
				k8sClient, crqName,
				&metav1.LabelSelector{
					MatchLabels: map[string]string{"quota": "limited-" + suffix},
				},
				quotav1alpha1.ResourceList{
					corev1.ResourcePods:   resource.MustParse("10"),
					corev1.ResourceCPU:    resource.MustParse("2"),
					corev1.ResourceMemory: resource.MustParse("4Gi"),
				})
			Expect(err).ToNot(HaveOccurred())
			time.Sleep(2 * time.Second) // Ensure CRQ is updated by reconciliation

			By("Updating the ClusterResourceQuota spec")
			latestCRQ := &quotav1alpha1.ClusterResourceQuota{}
			Expect(k8sClient.Get(ctx, types.NamespacedName{Name: crqName}, latestCRQ)).To(Succeed())
			latestCRQ.Spec.Hard = quotav1alpha1.ResourceList{
				corev1.ResourcePods: resource.MustParse("10"),
				corev1.ResourceCPU:  resource.MustParse("4"),
			}
			Expect(k8sClient.Update(ctx, latestCRQ)).To(Succeed())

			By("Cleaning up the ClusterResourceQuota")
			Expect(k8sClient.Delete(ctx, latestCRQ)).To(Succeed())
		})
	})

	Context("Edge cases", func() {
		It("should deny creation when multiple CRQs match the same namespace", func() {
			By("Creating a namespace that matches the selector")
			namespace := &corev1.Namespace{
				ObjectMeta: metav1.ObjectMeta{
					Name:   "test-namespace-" + suffix,
					Labels: map[string]string{"quota": "limited-" + suffix},
				},
			}
			Expect(k8sClient.Create(ctx, namespace)).To(Succeed())

			By("Creating the first ClusterResourceQuota")
			crq1, err := testutils.CreateClusterResourceQuota(ctx, k8sClient, "crq1", &metav1.LabelSelector{
				MatchLabels: map[string]string{"quota": "limited-" + suffix},
			}, quotav1alpha1.ResourceList{
				corev1.ResourcePods: resource.MustParse("10"),
			})
			Expect(err).ToNot(HaveOccurred())

			By("Attempting to create a second ClusterResourceQuota with overlapping namespace selector")
			_, err = testutils.CreateClusterResourceQuota(ctx, k8sClient, "crq2", &metav1.LabelSelector{
				MatchLabels: map[string]string{"quota": "limited-" + suffix},
			}, quotav1alpha1.ResourceList{
				corev1.ResourcePods: resource.MustParse("20"),
			})
			Expect(err).To(HaveOccurred())

			By("Cleaning up resources")
			Expect(k8sClient.Delete(ctx, crq1)).To(Succeed())
			Expect(k8sClient.Delete(ctx, namespace)).To(Succeed())
		})
	})
})
