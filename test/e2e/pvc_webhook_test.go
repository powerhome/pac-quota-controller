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
		ctx        context.Context
		testNS     string
		testNSObj  *corev1.Namespace
		testCRQ    *quotav1alpha1.ClusterResourceQuota
		crqName    string
		nameSuffix string
	)

	BeforeEach(func() {
		ctx = context.Background()
		nameSuffix = testutils.GenerateTestSuffix()
		testNS = fmt.Sprintf("pvc-webhook-test-%s", nameSuffix)
		crqName = fmt.Sprintf("pvc-webhook-crq-%s", nameSuffix)

		// Create test namespace with labels
		testNSObj = &corev1.Namespace{
			ObjectMeta: metav1.ObjectMeta{
				Name: testNS,
				Labels: map[string]string{
					"quota-test": "pvc-webhook",
				},
			},
		}
		Expect(k8sClient.Create(ctx, testNSObj)).To(Succeed())

		// Create ClusterResourceQuota with storage limits
		testCRQ = &quotav1alpha1.ClusterResourceQuota{
			ObjectMeta: metav1.ObjectMeta{
				Name: crqName,
			},
			Spec: quotav1alpha1.ClusterResourceQuotaSpec{
				Hard: quotav1alpha1.ResourceList{
					corev1.ResourceRequestsStorage:        resource.MustParse("2Gi"),
					corev1.ResourcePersistentVolumeClaims: resource.MustParse("2"),
				},
				NamespaceSelector: &metav1.LabelSelector{
					MatchLabels: map[string]string{
						"quota-test": "pvc-webhook",
					},
				},
			},
		}
		Expect(k8sClient.Create(ctx, testCRQ)).To(Succeed())

		// Wait for the CRQ to be ready
		Eventually(func() error {
			crq := &quotav1alpha1.ClusterResourceQuota{}
			if err := k8sClient.Get(ctx, types.NamespacedName{Name: crqName}, crq); err != nil {
				return err
			}
			return nil
		}, timeout, interval).Should(Succeed())
	})

	AfterEach(func() {
		// Clean up test resources
		if testCRQ != nil {
			_ = k8sClient.Delete(ctx, testCRQ)
		}
		if testNSObj != nil {
			_ = k8sClient.Delete(ctx, testNSObj)
		}
	})

	Context("Storage Quota Validation", func() {
		It("should allow PVC creation within storage limits", func() {
			pvc := &corev1.PersistentVolumeClaim{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "test-pvc-allowed",
					Namespace: testNS,
				},
				Spec: corev1.PersistentVolumeClaimSpec{
					AccessModes: []corev1.PersistentVolumeAccessMode{
						corev1.ReadWriteOnce,
					},
					Resources: corev1.VolumeResourceRequirements{
						Requests: corev1.ResourceList{
							corev1.ResourceStorage: resource.MustParse("1Gi"),
						},
					},
				},
			}

			// This should succeed
			Expect(k8sClient.Create(ctx, pvc)).To(Succeed())

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
			firstPVC := &corev1.PersistentVolumeClaim{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "first-pvc",
					Namespace: testNS,
				},
				Spec: corev1.PersistentVolumeClaimSpec{
					AccessModes: []corev1.PersistentVolumeAccessMode{
						corev1.ReadWriteOnce,
					},
					Resources: corev1.VolumeResourceRequirements{
						Requests: corev1.ResourceList{
							corev1.ResourceStorage: resource.MustParse("1.5Gi"),
						},
					},
				},
			}
			Expect(k8sClient.Create(ctx, firstPVC)).To(Succeed())

			// Wait for the controller to update the status
			Eventually(func() bool {
				crq := &quotav1alpha1.ClusterResourceQuota{}
				if err := k8sClient.Get(ctx, types.NamespacedName{Name: crqName}, crq); err != nil {
					return false
				}
				// Check if usage has been updated
				if used, exists := crq.Status.Total.Used[corev1.ResourceRequestsStorage]; exists {
					return !used.IsZero()
				}
				return false
			}, timeout, interval).Should(BeTrue())

			// Now try to create another PVC that would exceed the quota
			secondPVC := &corev1.PersistentVolumeClaim{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "second-pvc-blocked",
					Namespace: testNS,
				},
				Spec: corev1.PersistentVolumeClaimSpec{
					AccessModes: []corev1.PersistentVolumeAccessMode{
						corev1.ReadWriteOnce,
					},
					Resources: corev1.VolumeResourceRequirements{
						Requests: corev1.ResourceList{
							corev1.ResourceStorage: resource.MustParse("1Gi"), // 1.5Gi + 1Gi > 2Gi limit
						},
					},
				},
			}

			// This should fail due to quota violation
			err := k8sClient.Create(ctx, secondPVC)
			Expect(err).To(HaveOccurred())
			Expect(err.Error()).To(ContainSubstring("ClusterResourceQuota"))
			Expect(err.Error()).To(ContainSubstring("storage limit"))
		})

		It("should block PVC creation when PVC count quota would be exceeded", func() {
			// Create 2 PVCs to reach the limit (quota allows 2 PVCs)
			for i := 0; i < 2; i++ {
				pvc := &corev1.PersistentVolumeClaim{
					ObjectMeta: metav1.ObjectMeta{
						Name:      fmt.Sprintf("pvc-%d", i),
						Namespace: testNS,
					},
					Spec: corev1.PersistentVolumeClaimSpec{
						AccessModes: []corev1.PersistentVolumeAccessMode{
							corev1.ReadWriteOnce,
						},
						Resources: corev1.VolumeResourceRequirements{
							Requests: corev1.ResourceList{
								corev1.ResourceStorage: resource.MustParse("500Mi"),
							},
						},
					},
				}
				Expect(k8sClient.Create(ctx, pvc)).To(Succeed())
			}

			// Try to create a third PVC
			thirdPVC := &corev1.PersistentVolumeClaim{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "pvc-blocked-by-count",
					Namespace: testNS,
				},
				Spec: corev1.PersistentVolumeClaimSpec{
					AccessModes: []corev1.PersistentVolumeAccessMode{
						corev1.ReadWriteOnce,
					},
					Resources: corev1.VolumeResourceRequirements{
						Requests: corev1.ResourceList{
							corev1.ResourceStorage: resource.MustParse("100Mi"),
						},
					},
				},
			}

			// This should fail due to PVC count quota violation
			err := k8sClient.Create(ctx, thirdPVC)
			Expect(err).To(HaveOccurred())
			Expect(err.Error()).To(ContainSubstring("ClusterResourceQuota"))
			Expect(err.Error()).To(ContainSubstring("PVC count limit"))
		})

		It("should allow PVC creation in namespaces not matching the selector", func() {
			// Create a namespace without the quota-test label
			nonQuotaNS := &corev1.Namespace{
				ObjectMeta: metav1.ObjectMeta{
					Name: fmt.Sprintf("non-quota-ns-%s", nameSuffix),
					Labels: map[string]string{
						"app": "test", // Different label, won't match quota selector
					},
				},
			}
			Expect(k8sClient.Create(ctx, nonQuotaNS)).To(Succeed())
			defer func() {
				_ = k8sClient.Delete(ctx, nonQuotaNS)
			}()

			// Create a large PVC in the non-quota namespace
			pvc := &corev1.PersistentVolumeClaim{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "large-pvc-no-quota",
					Namespace: nonQuotaNS.Name,
				},
				Spec: corev1.PersistentVolumeClaimSpec{
					AccessModes: []corev1.PersistentVolumeAccessMode{
						corev1.ReadWriteOnce,
					},
					Resources: corev1.VolumeResourceRequirements{
						Requests: corev1.ResourceList{
							corev1.ResourceStorage: resource.MustParse("10Gi"), // Much larger than quota limit
						},
					},
				},
			}

			// This should succeed because the namespace doesn't match the quota selector
			Expect(k8sClient.Create(ctx, pvc)).To(Succeed())
		})
	})

	Context("PVC Updates", func() {
		It("should validate storage quota on PVC creation that would exceed limits", func() {
			// Create a small PVC first to partially consume quota
			pvc1 := &corev1.PersistentVolumeClaim{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "test-pvc-small",
					Namespace: testNS,
				},
				Spec: corev1.PersistentVolumeClaimSpec{
					AccessModes: []corev1.PersistentVolumeAccessMode{
						corev1.ReadWriteOnce,
					},
					Resources: corev1.VolumeResourceRequirements{
						Requests: corev1.ResourceList{
							corev1.ResourceStorage: resource.MustParse("500Mi"),
						},
					},
				},
			}
			Expect(k8sClient.Create(ctx, pvc1)).To(Succeed())
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
			pvc2 := &corev1.PersistentVolumeClaim{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "test-pvc-large",
					Namespace: testNS,
				},
				Spec: corev1.PersistentVolumeClaimSpec{
					AccessModes: []corev1.PersistentVolumeAccessMode{
						corev1.ReadWriteOnce,
					},
					Resources: corev1.VolumeResourceRequirements{
						Requests: corev1.ResourceList{
							corev1.ResourceStorage: resource.MustParse("2Gi"),
						},
					},
				},
			}
			err := k8sClient.Create(ctx, pvc2)

			// This should fail due to quota violation
			Expect(err).To(HaveOccurred())
			Expect(err.Error()).To(ContainSubstring("ClusterResourceQuota"))

			// Clean up the second PVC if it was created
			defer func() {
				_ = k8sClient.Delete(ctx, pvc2)
			}()
		})
	})
})
