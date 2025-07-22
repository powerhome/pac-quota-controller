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

package storage

import (
	"context"
	"testing"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/resource"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/client/fake"
)

func TestStorage(t *testing.T) {
	RegisterFailHandler(Fail)
	RunSpecs(t, "Storage Package Suite")
}

var _ = Describe("StorageResourceCalculator", func() {
	var (
		ctx        context.Context
		k8sClient  client.Client
		calculator *StorageResourceCalculator
		scheme     *runtime.Scheme
	)

	BeforeEach(func() {
		ctx = context.Background()
		scheme = runtime.NewScheme()
		Expect(corev1.AddToScheme(scheme)).To(Succeed())
		k8sClient = fake.NewClientBuilder().WithScheme(scheme).Build()
		calculator = NewStorageResourceCalculator(k8sClient)
	})

	Describe("CalculateStorageUsage", func() {
		It("should return zero when no PVCs exist", func() {
			usage, err := calculator.CalculateStorageUsage(ctx, "test-namespace")
			Expect(err).NotTo(HaveOccurred())
			Expect(usage.IsZero()).To(BeTrue())
		})

		It("should calculate storage usage from PVC requests", func() {
			// Create PVCs with different storage requests
			pvc1 := &corev1.PersistentVolumeClaim{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "pvc1",
					Namespace: "test-namespace",
				},
				Spec: corev1.PersistentVolumeClaimSpec{
					Resources: corev1.VolumeResourceRequirements{
						Requests: corev1.ResourceList{
							corev1.ResourceStorage: resource.MustParse("10Gi"),
						},
					},
				},
			}

			pvc2 := &corev1.PersistentVolumeClaim{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "pvc2",
					Namespace: "test-namespace",
				},
				Spec: corev1.PersistentVolumeClaimSpec{
					Resources: corev1.VolumeResourceRequirements{
						Requests: corev1.ResourceList{
							corev1.ResourceStorage: resource.MustParse("5Gi"),
						},
					},
				},
			}

			Expect(k8sClient.Create(ctx, pvc1)).To(Succeed())
			Expect(k8sClient.Create(ctx, pvc2)).To(Succeed())

			usage, err := calculator.CalculateStorageUsage(ctx, "test-namespace")
			Expect(err).NotTo(HaveOccurred())

			expectedUsage := resource.MustParse("15Gi")
			Expect(usage.Equal(expectedUsage)).To(BeTrue(), "Expected %s, got %s", expectedUsage.String(), usage.String())
		})

		It("should handle PVCs without storage requests", func() {
			pvcWithoutRequest := &corev1.PersistentVolumeClaim{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "pvc-no-request",
					Namespace: "test-namespace",
				},
				Spec: corev1.PersistentVolumeClaimSpec{
					// No Resources field
				},
			}

			pvcWithRequest := &corev1.PersistentVolumeClaim{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "pvc-with-request",
					Namespace: "test-namespace",
				},
				Spec: corev1.PersistentVolumeClaimSpec{
					Resources: corev1.VolumeResourceRequirements{
						Requests: corev1.ResourceList{
							corev1.ResourceStorage: resource.MustParse("8Gi"),
						},
					},
				},
			}

			Expect(k8sClient.Create(ctx, pvcWithoutRequest)).To(Succeed())
			Expect(k8sClient.Create(ctx, pvcWithRequest)).To(Succeed())

			usage, err := calculator.CalculateStorageUsage(ctx, "test-namespace")
			Expect(err).NotTo(HaveOccurred())

			expectedUsage := resource.MustParse("8Gi")
			Expect(usage.Equal(expectedUsage)).To(BeTrue())
		})

		It("should only calculate storage for the specified namespace", func() {
			// Create PVC in target namespace
			pvcInTarget := &corev1.PersistentVolumeClaim{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "pvc-target",
					Namespace: "target-namespace",
				},
				Spec: corev1.PersistentVolumeClaimSpec{
					Resources: corev1.VolumeResourceRequirements{
						Requests: corev1.ResourceList{
							corev1.ResourceStorage: resource.MustParse("10Gi"),
						},
					},
				},
			}

			// Create PVC in different namespace
			pvcInOther := &corev1.PersistentVolumeClaim{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "pvc-other",
					Namespace: "other-namespace",
				},
				Spec: corev1.PersistentVolumeClaimSpec{
					Resources: corev1.VolumeResourceRequirements{
						Requests: corev1.ResourceList{
							corev1.ResourceStorage: resource.MustParse("20Gi"),
						},
					},
				},
			}

			Expect(k8sClient.Create(ctx, pvcInTarget)).To(Succeed())
			Expect(k8sClient.Create(ctx, pvcInOther)).To(Succeed())

			usage, err := calculator.CalculateStorageUsage(ctx, "target-namespace")
			Expect(err).NotTo(HaveOccurred())

			expectedUsage := resource.MustParse("10Gi")
			Expect(usage.Equal(expectedUsage)).To(BeTrue())
		})
	})

	Describe("getPVCStorageRequest", func() {
		It("should extract storage request correctly", func() {
			pvc := &corev1.PersistentVolumeClaim{
				Spec: corev1.PersistentVolumeClaimSpec{
					Resources: corev1.VolumeResourceRequirements{
						Requests: corev1.ResourceList{
							corev1.ResourceStorage: resource.MustParse("5Gi"),
						},
					},
				},
			}

			request := getPVCStorageRequest(pvc)
			expectedRequest := resource.MustParse("5Gi")
			Expect(request.Equal(expectedRequest)).To(BeTrue())
		})

		It("should return zero when no resources are specified", func() {
			pvc := &corev1.PersistentVolumeClaim{
				Spec: corev1.PersistentVolumeClaimSpec{
					// No Resources field
				},
			}

			request := getPVCStorageRequest(pvc)
			Expect(request.IsZero()).To(BeTrue())
		})

		It("should return zero when no storage request is specified", func() {
			pvc := &corev1.PersistentVolumeClaim{
				Spec: corev1.PersistentVolumeClaimSpec{
					Resources: corev1.VolumeResourceRequirements{
						Requests: corev1.ResourceList{
							// No storage request, only other resources
							corev1.ResourceCPU: resource.MustParse("100m"),
						},
					},
				},
			}

			request := getPVCStorageRequest(pvc)
			Expect(request.IsZero()).To(BeTrue())
		})
	})

	Describe("CalculateStorageClassUsage", func() {
		BeforeEach(func() {
			// Create test PVCs with different storage classes
			pvc1 := createTestPVC("pvc1", "test-namespace", "2Gi", "fast-ssd")
			pvc2 := createTestPVC("pvc2", "test-namespace", "1Gi", "slow-hdd")
			pvc3 := createTestPVC("pvc3", "test-namespace", "500Mi", "fast-ssd")
			pvc4 := createTestPVC("pvc4", "test-namespace", "1Gi", "") // No storage class

			Expect(k8sClient.Create(ctx, pvc1)).To(Succeed())
			Expect(k8sClient.Create(ctx, pvc2)).To(Succeed())
			Expect(k8sClient.Create(ctx, pvc3)).To(Succeed())
			Expect(k8sClient.Create(ctx, pvc4)).To(Succeed())
		})

		It("should calculate storage usage for specific storage class", func() {
			usage, err := calculator.CalculateStorageClassUsage(ctx, "test-namespace", "fast-ssd")
			Expect(err).NotTo(HaveOccurred())

			// Expected: 2Gi + 500Mi = 2.5Gi (only fast-ssd PVCs)
			// Note: We need to match the exact binary representation
			twoGi := resource.MustParse("2Gi")
			fiveHundredMi := resource.MustParse("500Mi")
			expected := twoGi.DeepCopy()
			expected.Add(fiveHundredMi)

			Expect(usage.Equal(expected)).To(BeTrue(), "Expected %s, got %s", expected.String(), usage.String())
		})

		It("should calculate storage usage for different storage class", func() {
			usage, err := calculator.CalculateStorageClassUsage(ctx, "test-namespace", "slow-hdd")
			Expect(err).NotTo(HaveOccurred())

			expected := resource.MustParse("1Gi")
			Expect(usage.Equal(expected)).To(BeTrue())
		})

		It("should return zero for non-existent storage class", func() {
			usage, err := calculator.CalculateStorageClassUsage(ctx, "test-namespace", "non-existent")
			Expect(err).NotTo(HaveOccurred())
			Expect(usage.IsZero()).To(BeTrue())
		})

		It("should handle empty storage class", func() {
			usage, err := calculator.CalculateStorageClassUsage(ctx, "test-namespace", "")
			Expect(err).NotTo(HaveOccurred())

			expected := resource.MustParse("1Gi")
			Expect(usage.Equal(expected)).To(BeTrue())
		})

		It("should only count storage from the specified namespace", func() {
			// Create PVC in different namespace with same storage class
			otherPVC := createTestPVC("other-pvc", "other-namespace", "10Gi", "fast-ssd")
			Expect(k8sClient.Create(ctx, otherPVC)).To(Succeed())

			usage, err := calculator.CalculateStorageClassUsage(ctx, "test-namespace", "fast-ssd")
			Expect(err).NotTo(HaveOccurred())

			// Should still be 2Gi + 500Mi from test-namespace, not including the 10Gi from other-namespace
			twoGi := resource.MustParse("2Gi")
			fiveHundredMi := resource.MustParse("500Mi")
			expected := twoGi.DeepCopy()
			expected.Add(fiveHundredMi)

			Expect(usage.Equal(expected)).To(BeTrue(), "Expected %s, got %s", expected.String(), usage.String())
		})
	})

	Describe("CalculateStorageClassCount", func() {
		BeforeEach(func() {
			// Create test PVCs with different storage classes
			pvc1 := createTestPVC("pvc1", "test-namespace", "1Gi", "fast-ssd")
			pvc2 := createTestPVC("pvc2", "test-namespace", "1Gi", "slow-hdd")
			pvc3 := createTestPVC("pvc3", "test-namespace", "1Gi", "fast-ssd")
			pvc4 := createTestPVC("pvc4", "test-namespace", "1Gi", "") // No storage class

			Expect(k8sClient.Create(ctx, pvc1)).To(Succeed())
			Expect(k8sClient.Create(ctx, pvc2)).To(Succeed())
			Expect(k8sClient.Create(ctx, pvc3)).To(Succeed())
			Expect(k8sClient.Create(ctx, pvc4)).To(Succeed())
		})

		It("should count PVCs for specific storage class", func() {
			count, err := calculator.CalculateStorageClassCount(ctx, "test-namespace", "fast-ssd")
			Expect(err).NotTo(HaveOccurred())
			Expect(count).To(Equal(int64(2)))
		})

		It("should count PVCs for different storage class", func() {
			count, err := calculator.CalculateStorageClassCount(ctx, "test-namespace", "slow-hdd")
			Expect(err).NotTo(HaveOccurred())
			Expect(count).To(Equal(int64(1)))
		})

		It("should return zero for non-existent storage class", func() {
			count, err := calculator.CalculateStorageClassCount(ctx, "test-namespace", "non-existent")
			Expect(err).NotTo(HaveOccurred())
			Expect(count).To(Equal(int64(0)))
		})

		It("should handle empty storage class", func() {
			count, err := calculator.CalculateStorageClassCount(ctx, "test-namespace", "")
			Expect(err).NotTo(HaveOccurred())
			Expect(count).To(Equal(int64(1)))
		})

		It("should only count PVCs from the specified namespace", func() {
			// Create PVC in different namespace with same storage class
			otherPVC := createTestPVC("other-pvc", "other-namespace", "1Gi", "fast-ssd")
			Expect(k8sClient.Create(ctx, otherPVC)).To(Succeed())

			count, err := calculator.CalculateStorageClassCount(ctx, "test-namespace", "fast-ssd")
			Expect(err).NotTo(HaveOccurred())
			Expect(count).To(Equal(int64(2))) // Should still be 2 from test-namespace
		})
	})

	Describe("getPVCStorageClass", func() {
		It("should extract storage class correctly", func() {
			pvc := createTestPVC("test-pvc", "test-namespace", "1Gi", "premium")
			storageClass := getPVCStorageClass(pvc)
			Expect(storageClass).To(Equal("premium"))
		})

		It("should return empty string for PVC without storage class", func() {
			pvc := &corev1.PersistentVolumeClaim{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "no-class-pvc",
					Namespace: "test-namespace",
				},
				Spec: corev1.PersistentVolumeClaimSpec{
					Resources: corev1.VolumeResourceRequirements{
						Requests: corev1.ResourceList{
							corev1.ResourceStorage: resource.MustParse("1Gi"),
						},
					},
				},
			}
			storageClass := getPVCStorageClass(pvc)
			Expect(storageClass).To(Equal(""))
		})
	})
})

// Helper function to create test PVCs
func createTestPVC(name, namespace, storageSize, storageClass string) *corev1.PersistentVolumeClaim {
	pvc := &corev1.PersistentVolumeClaim{
		ObjectMeta: metav1.ObjectMeta{
			Name:      name,
			Namespace: namespace,
		},
		Spec: corev1.PersistentVolumeClaimSpec{
			AccessModes: []corev1.PersistentVolumeAccessMode{
				corev1.ReadWriteOnce,
			},
			Resources: corev1.VolumeResourceRequirements{
				Requests: corev1.ResourceList{
					corev1.ResourceStorage: resource.MustParse(storageSize),
				},
			},
		},
	}

	if storageClass != "" {
		pvc.Spec.StorageClassName = &storageClass
	}

	return pvc
}
