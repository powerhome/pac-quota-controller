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

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	quotav1alpha1 "github.com/powerhome/pac-quota-controller/api/v1alpha1"
	testutils "github.com/powerhome/pac-quota-controller/test/utils"
	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/resource"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"
)

var _ = Describe("Storage Resources E2E", func() {
	var (
		ctx     = context.Background()
		suffix  string
		crqName string
	)

	BeforeEach(func() {
		suffix = testutils.GenerateTestSuffix()
		crqName = testutils.GenerateResourceName("storage-crq")
	})

	Context("PVC Storage Usage", func() {
		It("should track storage usage from PVCs", func() {
			By("Creating a namespace with the appropriate label")
			namespace := &corev1.Namespace{
				ObjectMeta: metav1.ObjectMeta{
					Name:   "storage-test-" + suffix,
					Labels: map[string]string{"storage-quota": "enabled-" + suffix},
				},
			}
			Expect(k8sClient.Create(ctx, namespace)).To(Succeed())
			defer func() {
				Expect(k8sClient.Delete(ctx, namespace)).To(Succeed())
			}()

			By("Creating a ClusterResourceQuota with storage limits")
			crq, err := testutils.CreateClusterResourceQuota(ctx, k8sClient, crqName, &metav1.LabelSelector{
				MatchLabels: map[string]string{"storage-quota": "enabled-" + suffix},
			}, quotav1alpha1.ResourceList{
				corev1.ResourceRequestsStorage: resource.MustParse("10Gi"),
			})
			Expect(err).ToNot(HaveOccurred())
			defer func() {
				Expect(k8sClient.Delete(ctx, crq)).To(Succeed())
			}()

			By("Creating a PVC with storage request")
			pvc := &corev1.PersistentVolumeClaim{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "test-pvc-" + suffix,
					Namespace: namespace.Name,
				},
				Spec: corev1.PersistentVolumeClaimSpec{
					AccessModes: []corev1.PersistentVolumeAccessMode{
						corev1.ReadWriteOnce,
					},
					Resources: corev1.VolumeResourceRequirements{
						Requests: corev1.ResourceList{
							corev1.ResourceStorage: resource.MustParse("5Gi"),
						},
					},
				},
			}
			Expect(k8sClient.Create(ctx, pvc)).To(Succeed())
			defer func() {
				Expect(k8sClient.Delete(ctx, pvc)).To(Succeed())
			}()

			By("Waiting for the CRQ status to be updated with storage usage")
			Eventually(func() bool {
				latestCRQ := &quotav1alpha1.ClusterResourceQuota{}
				if err := k8sClient.Get(ctx, types.NamespacedName{Name: crqName}, latestCRQ); err != nil {
					return false
				}

				// Check if storage usage is reported
				if usage, exists := latestCRQ.Status.Total.Used[corev1.ResourceRequestsStorage]; exists {
					expectedUsage := resource.MustParse("5Gi")
					return usage.Equal(expectedUsage)
				}
				return false
			}, "30s", "1s").Should(BeTrue())
		})
	})

	Context("Ephemeral Storage Usage", func() {
		It("should track ephemeral storage usage from Pods", func() {
			By("Creating a namespace with the appropriate label")
			namespace := &corev1.Namespace{
				ObjectMeta: metav1.ObjectMeta{
					Name:   "ephemeral-test-" + suffix,
					Labels: map[string]string{"ephemeral-quota": "enabled-" + suffix},
				},
			}
			Expect(k8sClient.Create(ctx, namespace)).To(Succeed())
			defer func() {
				Expect(k8sClient.Delete(ctx, namespace)).To(Succeed())
			}()

			By("Creating a ClusterResourceQuota with ephemeral storage limits")
			crq, err := testutils.CreateClusterResourceQuota(ctx, k8sClient, crqName, &metav1.LabelSelector{
				MatchLabels: map[string]string{"ephemeral-quota": "enabled-" + suffix},
			}, quotav1alpha1.ResourceList{
				corev1.ResourceRequestsEphemeralStorage: resource.MustParse("2Gi"),
			})
			Expect(err).ToNot(HaveOccurred())
			defer func() {
				Expect(k8sClient.Delete(ctx, crq)).To(Succeed())
			}()

			By("Creating a Pod with ephemeral storage request")
			pod := &corev1.Pod{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "test-pod-" + suffix,
					Namespace: namespace.Name,
				},
				Spec: corev1.PodSpec{
					Containers: []corev1.Container{
						{
							Name:  "test-container",
							Image: "busybox:latest",
							Resources: corev1.ResourceRequirements{
								Requests: corev1.ResourceList{
									corev1.ResourceEphemeralStorage: resource.MustParse("1Gi"),
								},
							},
							Command: []string{"sleep", "3600"},
						},
					},
				},
			}
			Expect(k8sClient.Create(ctx, pod)).To(Succeed())
			defer func() {
				Expect(k8sClient.Delete(ctx, pod)).To(Succeed())
			}()

			By("Waiting for the CRQ status to be updated with ephemeral storage usage")
			Eventually(func() bool {
				latestCRQ := &quotav1alpha1.ClusterResourceQuota{}
				if err := k8sClient.Get(ctx, types.NamespacedName{Name: crqName}, latestCRQ); err != nil {
					return false
				}

				// Check if ephemeral storage usage is reported
				if usage, exists := latestCRQ.Status.Total.Used[corev1.ResourceRequestsEphemeralStorage]; exists {
					expectedUsage := resource.MustParse("1Gi")
					return usage.Equal(expectedUsage)
				}
				return false
			}, "30s", "1s").Should(BeTrue())
		})
	})

	Context("Combined Storage Usage", func() {
		It("should track both PVC and ephemeral storage usage", func() {
			By("Creating a namespace with the appropriate label")
			namespace := &corev1.Namespace{
				ObjectMeta: metav1.ObjectMeta{
					Name:   "combined-test-" + suffix,
					Labels: map[string]string{"combined-quota": "enabled-" + suffix},
				},
			}
			Expect(k8sClient.Create(ctx, namespace)).To(Succeed())
			defer func() {
				Expect(k8sClient.Delete(ctx, namespace)).To(Succeed())
			}()

			By("Creating a ClusterResourceQuota with both storage limits")
			crq, err := testutils.CreateClusterResourceQuota(ctx, k8sClient, crqName, &metav1.LabelSelector{
				MatchLabels: map[string]string{"combined-quota": "enabled-" + suffix},
			}, quotav1alpha1.ResourceList{
				corev1.ResourceRequestsStorage:          resource.MustParse("10Gi"),
				corev1.ResourceRequestsEphemeralStorage: resource.MustParse("2Gi"),
			})
			Expect(err).ToNot(HaveOccurred())
			defer func() {
				Expect(k8sClient.Delete(ctx, crq)).To(Succeed())
			}()

			By("Creating a PVC with storage request")
			pvc := &corev1.PersistentVolumeClaim{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "combined-pvc-" + suffix,
					Namespace: namespace.Name,
				},
				Spec: corev1.PersistentVolumeClaimSpec{
					AccessModes: []corev1.PersistentVolumeAccessMode{
						corev1.ReadWriteOnce,
					},
					Resources: corev1.VolumeResourceRequirements{
						Requests: corev1.ResourceList{
							corev1.ResourceStorage: resource.MustParse("3Gi"),
						},
					},
				},
			}
			Expect(k8sClient.Create(ctx, pvc)).To(Succeed())
			defer func() {
				Expect(k8sClient.Delete(ctx, pvc)).To(Succeed())
			}()

			By("Creating a Pod with ephemeral storage request")
			pod := &corev1.Pod{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "combined-pod-" + suffix,
					Namespace: namespace.Name,
				},
				Spec: corev1.PodSpec{
					Containers: []corev1.Container{
						{
							Name:  "test-container",
							Image: "busybox:latest",
							Resources: corev1.ResourceRequirements{
								Requests: corev1.ResourceList{
									corev1.ResourceEphemeralStorage: resource.MustParse("500Mi"),
								},
							},
							Command: []string{"sleep", "3600"},
						},
					},
				},
			}
			Expect(k8sClient.Create(ctx, pod)).To(Succeed())
			defer func() {
				Expect(k8sClient.Delete(ctx, pod)).To(Succeed())
			}()

			By("Waiting for the CRQ status to be updated with both storage usages")
			Eventually(func() bool {
				latestCRQ := &quotav1alpha1.ClusterResourceQuota{}
				if err := k8sClient.Get(ctx, types.NamespacedName{Name: crqName}, latestCRQ); err != nil {
					return false
				}

				// Check if both storage usages are reported correctly
				storageUsage, storageExists := latestCRQ.Status.Total.Used[corev1.ResourceRequestsStorage]
				ephemeralUsage, ephemeralExists := latestCRQ.Status.Total.Used[corev1.ResourceRequestsEphemeralStorage]

				if !storageExists || !ephemeralExists {
					return false
				}

				expectedStorage := resource.MustParse("3Gi")
				expectedEphemeral := resource.MustParse("500Mi")

				return storageUsage.Equal(expectedStorage) && ephemeralUsage.Equal(expectedEphemeral)
			}, "30s", "1s").Should(BeTrue())
		})
	})
})
