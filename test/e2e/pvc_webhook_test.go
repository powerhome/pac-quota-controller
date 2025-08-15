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
	"time"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/resource"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"

	quotav1alpha1 "github.com/powerhome/pac-quota-controller/api/v1alpha1"
	testutils "github.com/powerhome/pac-quota-controller/test/utils"
)

const (
	timeout  = time.Second * 30
	interval = time.Second * 5
)

var _ = Describe("PVC Webhook E2E Tests", func() {
	var (
		testNamespace string
		ns            *corev1.Namespace
		crq           *quotav1alpha1.ClusterResourceQuota
		testCRQName   string
		testSuffix    string
	)

	BeforeEach(func() {
		testSuffix = testutils.GenerateTestSuffix()
		testNamespace = testutils.GenerateResourceName("pod-webhook-ns")
		testCRQName = testutils.GenerateResourceName("pod-webhook-crq")

		var err error
		ns, err = testutils.CreateNamespace(ctx, k8sClient, testNamespace, map[string]string{
			"pod-webhook-test": "test-label-" + testSuffix,
		})
		Expect(err).ToNot(HaveOccurred())

		// Ensure no other CRQs target the namespace
		existingCRQs, err := testutils.ListClusterResourceQuotas(ctx, k8sClient, &metav1.LabelSelector{
			MatchLabels: map[string]string{
				"pod-webhook-test": "test-label-" + testSuffix,
			},
		})
		Expect(err).NotTo(HaveOccurred())
		Expect(existingCRQs.Items).To(BeEmpty(), "Namespace already targeted by another CRQ")

		crq, err = testutils.CreateClusterResourceQuota(ctx, k8sClient, testCRQName, &metav1.LabelSelector{
			MatchLabels: map[string]string{
				"pod-webhook-test": "test-label-" + testSuffix,
			},
		}, quotav1alpha1.ResourceList{
			corev1.ResourceRequestsStorage:        resource.MustParse("2Gi"),
			corev1.ResourcePersistentVolumeClaims: resource.MustParse("2"),
		})
		Expect(err).NotTo(HaveOccurred())
	})

	AfterEach(func() {
		DeferCleanup(func() {
			_ = k8sClient.Delete(ctx, ns)
		})
		DeferCleanup(func() {
			_ = k8sClient.Delete(ctx, crq)
		})
	})

	Context("Storage Quota Validation", func() {
		It("should allow PVC creation within storage limits", func() {
			pvc, err := testutils.CreatePVC(ctx, k8sClient, ns.Name, "test-pvc-allowed", "1Gi", nil)
			Expect(err).ToNot(HaveOccurred())

			// Verify PVC was created
			Eventually(func() error {
				createdPVC := &corev1.PersistentVolumeClaim{}
				return k8sClient.Get(ctx, types.NamespacedName{
					Name:      pvc.Name,
					Namespace: pvc.Namespace,
				}, createdPVC)
			}, timeout, interval).Should(Succeed())
		})

		It("should block PVC creation when storage quota would be exceeded", func() {
			// First, create a PVC that uses most of the quota
			_, err := testutils.CreatePVC(ctx, k8sClient, ns.Name, "first-pvc", "1.5Gi", nil)
			Expect(err).ToNot(HaveOccurred())

			// Wait for the controller to update the status
			Eventually(func() bool {
				reconciledCRQ := &quotav1alpha1.ClusterResourceQuota{}
				if err := k8sClient.Get(ctx, types.NamespacedName{Name: crq.Name}, reconciledCRQ); err != nil {
					return false
				}
				// Check if usage has been updated
				if used, exists := reconciledCRQ.Status.Total.Used[corev1.ResourceRequestsStorage]; exists {
					return !used.IsZero()
				}
				return false
			}, timeout, interval).Should(BeTrue())

			// Now try to create another PVC that would exceed the quota
			_, err = testutils.CreatePVC(ctx, k8sClient, ns.Name, "second-pvc-blocked", "1Gi", nil)
			Expect(err).To(HaveOccurred())
			Expect(err.Error()).To(SatisfyAll(
				ContainSubstring("ClusterResourceQuota"),
				ContainSubstring(crq.Name),
				ContainSubstring("storage"),
				ContainSubstring("exceeded"),
			))
		})

		It("should allow PVC creation in namespaces not matching the selector", func() {
			ns, err := testutils.CreateNamespace(ctx, k8sClient, "non-quota-ns-"+testSuffix, nil)
			Expect(err).ToNot(HaveOccurred())
			defer func() {
				_ = k8sClient.Delete(ctx, ns)
			}()

			// Create a large PVC in the non-quota namespace
			_, err = testutils.CreatePVC(ctx, k8sClient, ns.Name, "large-pvc-no-quota", "10Gi", nil)
			Expect(err).ToNot(HaveOccurred())
		})
	})

	Context("PVC Updates", func() {
		It("should validate storage quota on PVC creation that would exceed limits", func() {
			// Create a small PVC first to partially consume quota
			pvc1, err := testutils.CreatePVC(ctx, k8sClient, ns.Name, "test-pvc-small", "500Mi", nil)
			Expect(err).ToNot(HaveOccurred())
			defer func() {
				_ = k8sClient.Delete(ctx, pvc1)
			}()

			// Wait for PVC to be created
			Eventually(func() error {
				return k8sClient.Get(ctx, types.NamespacedName{
					Name:      pvc1.Name,
					Namespace: pvc1.Namespace,
				}, pvc1)
			}, timeout, interval).Should(Succeed())

			// Try to create another PVC that would exceed the 2Gi quota (500Mi + 2Gi = 2.5Gi > 2Gi)
			pvc2, err := testutils.CreatePVC(ctx, k8sClient, ns.Name, "test-pvc-large", "2Gi", nil)
			Expect(err).To(HaveOccurred())
			Expect(err.Error()).To(SatisfyAll(
				ContainSubstring("ClusterResourceQuota"),
				ContainSubstring(crq.Name),
			))

			// Clean up the second PVC if it was created
			defer func() {
				_ = k8sClient.Delete(ctx, pvc2)
			}()
		})
	})

	Context("PVC Count Quota Tests", func() {
		It("should allow PVC creation when within PVC count limits", func() {
			// Create first PVC
			pvc1, err := testutils.CreatePVC(ctx, k8sClient, ns.Name, testutils.GenerateResourceName("test-pvc-1"), "1Gi", nil)
			Expect(err).NotTo(HaveOccurred())

			// Create second PVC - should succeed
			pvc2, err := testutils.CreatePVC(ctx, k8sClient, ns.Name, testutils.GenerateResourceName("test-pvc-2"), "1Gi", nil)
			Expect(err).NotTo(HaveOccurred())

			// Cleanup
			Expect(k8sClient.Delete(ctx, pvc1)).To(Succeed())
			Expect(k8sClient.Delete(ctx, pvc2)).To(Succeed())
		})

		It("should deny PVC creation when it would exceed PVC count limits", func() {
			// Create first PVC - should succeed
			_, err := testutils.CreatePVC(ctx, k8sClient, ns.Name, testutils.GenerateResourceName("test-pvc-1"), "1Gi", nil)
			Expect(err).NotTo(HaveOccurred())

			// Create second PVC - should allow
			_, err = testutils.CreatePVC(ctx, k8sClient, ns.Name, testutils.GenerateResourceName("test-pvc-2"), "1Gi", nil)
			Expect(err).NotTo(HaveOccurred())

			// Create third PVC - should fail
			_, err = testutils.CreatePVC(ctx, k8sClient, ns.Name, testutils.GenerateResourceName("test-pvc-3"), "1Gi", nil)
			Expect(err).To(HaveOccurred())
			Expect(err.Error()).To(SatisfyAll(
				ContainSubstring("ClusterResourceQuota"),
				ContainSubstring(crq.Name),
			))
		})
	})
})
