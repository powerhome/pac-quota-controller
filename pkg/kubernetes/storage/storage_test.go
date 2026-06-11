package storage

import (
	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/resource"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
)

var _ = Describe("Storage pure helpers", func() {
	Describe("GetPVCStorageRequest", func() {
		It("should extract storage request from PVC", func() {
			pvc := &corev1.PersistentVolumeClaim{
				Spec: corev1.PersistentVolumeClaimSpec{
					Resources: corev1.VolumeResourceRequirements{
						Requests: corev1.ResourceList{
							corev1.ResourceStorage: resource.MustParse("10Gi"),
						},
					},
				},
			}

			storageRequest := GetPVCStorageRequest(pvc)
			expected := resource.MustParse("10Gi")
			Expect(storageRequest.Equal(expected)).To(BeTrue())
		})

		It("should return zero for PVC without storage request", func() {
			pvc := &corev1.PersistentVolumeClaim{
				Spec: corev1.PersistentVolumeClaimSpec{
					Resources: corev1.VolumeResourceRequirements{
						Requests: corev1.ResourceList{},
					},
				},
			}

			storageRequest := GetPVCStorageRequest(pvc)
			Expect(storageRequest.Value()).To(Equal(int64(0)))
		})

		It("should handle large storage values", func() {
			pvc := &corev1.PersistentVolumeClaim{
				Spec: corev1.PersistentVolumeClaimSpec{
					Resources: corev1.VolumeResourceRequirements{
						Requests: corev1.ResourceList{
							corev1.ResourceStorage: resource.MustParse("1Ti"),
						},
					},
				},
			}

			storageRequest := GetPVCStorageRequest(pvc)
			expected := resource.MustParse("1Ti")
			Expect(storageRequest.Equal(expected)).To(BeTrue())
		})

		It("should handle small storage values", func() {
			pvc := &corev1.PersistentVolumeClaim{
				Spec: corev1.PersistentVolumeClaimSpec{
					Resources: corev1.VolumeResourceRequirements{
						Requests: corev1.ResourceList{
							corev1.ResourceStorage: resource.MustParse("1Mi"),
						},
					},
				},
			}

			storageRequest := GetPVCStorageRequest(pvc)
			expected := resource.MustParse("1Mi")
			Expect(storageRequest.Equal(expected)).To(BeTrue())
		})

		It("should handle nil PVC", func() {
			storageRequest := GetPVCStorageRequest(nil)
			Expect(storageRequest.Value()).To(Equal(int64(0)))
		})

		It("should handle PVC with nil resources", func() {
			pvc := &corev1.PersistentVolumeClaim{
				Spec: corev1.PersistentVolumeClaimSpec{},
			}

			storageRequest := GetPVCStorageRequest(pvc)
			Expect(storageRequest.Value()).To(Equal(int64(0)))
		})
	})

	Describe("Storage Resource Edge Cases", func() {
		It("should handle zero storage values", func() {
			pvc := &corev1.PersistentVolumeClaim{
				Spec: corev1.PersistentVolumeClaimSpec{
					Resources: corev1.VolumeResourceRequirements{
						Requests: corev1.ResourceList{
							corev1.ResourceStorage: resource.MustParse("0"),
						},
					},
				},
			}

			storageRequest := GetPVCStorageRequest(pvc)
			Expect(storageRequest.Value()).To(Equal(int64(0)))
		})

		It("should handle different storage units", func() {
			testCases := []struct {
				input    string
				expected string
			}{
				{"1Ki", "1Ki"},
				{"1Mi", "1Mi"},
				{"1Gi", "1Gi"},
				{"1Ti", "1Ti"},
				{"1Pi", "1Pi"},
				{"1Ei", "1Ei"},
			}

			for _, tc := range testCases {
				pvc := &corev1.PersistentVolumeClaim{
					Spec: corev1.PersistentVolumeClaimSpec{
						Resources: corev1.VolumeResourceRequirements{
							Requests: corev1.ResourceList{
								corev1.ResourceStorage: resource.MustParse(tc.input),
							},
						},
					},
				}

				storageRequest := GetPVCStorageRequest(pvc)
				expected := resource.MustParse(tc.expected)
				Expect(storageRequest.Equal(expected)).To(BeTrue(), "Failed for input: %s", tc.input)
			}
		})

		It("should handle decimal storage values", func() {
			pvc := &corev1.PersistentVolumeClaim{
				Spec: corev1.PersistentVolumeClaimSpec{
					Resources: corev1.VolumeResourceRequirements{
						Requests: corev1.ResourceList{
							corev1.ResourceStorage: resource.MustParse("1.5Gi"),
						},
					},
				},
			}

			storageRequest := GetPVCStorageRequest(pvc)
			expected := resource.MustParse("1.5Gi")
			Expect(storageRequest.Equal(expected)).To(BeTrue())
		})
	})

	Describe("PVC Status Handling", func() {
		It("should handle PVC with different phases", func() {
			phases := []corev1.PersistentVolumeClaimPhase{
				corev1.ClaimPending,
				corev1.ClaimBound,
				corev1.ClaimLost,
			}

			for _, phase := range phases {
				pvc := &corev1.PersistentVolumeClaim{
					Spec: corev1.PersistentVolumeClaimSpec{
						Resources: corev1.VolumeResourceRequirements{
							Requests: corev1.ResourceList{
								corev1.ResourceStorage: resource.MustParse("1Gi"),
							},
						},
					},
					Status: corev1.PersistentVolumeClaimStatus{
						Phase: phase,
					},
				}

				storageRequest := GetPVCStorageRequest(pvc)
				expected := resource.MustParse("1Gi")
				Expect(storageRequest.Equal(expected)).To(BeTrue(), "Failed for phase: %s", phase)
			}
		})

		It("should handle PVC with empty status", func() {
			pvc := &corev1.PersistentVolumeClaim{
				Spec: corev1.PersistentVolumeClaimSpec{
					Resources: corev1.VolumeResourceRequirements{
						Requests: corev1.ResourceList{
							corev1.ResourceStorage: resource.MustParse("1Gi"),
						},
					},
				},
				Status: corev1.PersistentVolumeClaimStatus{},
			}

			storageRequest := GetPVCStorageRequest(pvc)
			expected := resource.MustParse("1Gi")
			Expect(storageRequest.Equal(expected)).To(BeTrue())
		})
	})

	Describe("Storage Class Edge Cases", func() {
		It("should handle special storage class names", func() {
			testCases := []string{
				"fast-ssd",
				"slow-hdd",
				"default",
				"",
				"storage-class-with-dashes",
				"storage_class_with_underscores",
				"storageclass123",
			}

			for _, storageClassName := range testCases {
				pvc := &corev1.PersistentVolumeClaim{
					Spec: corev1.PersistentVolumeClaimSpec{
						StorageClassName: &storageClassName,
					},
				}

				Expect(PVCMatchesStorageClass(pvc, storageClassName)).To(BeTrue(), "Failed for storage class: %s", storageClassName)
			}
		})

		It("should handle very long storage class names", func() {
			longName := "very-long-storage-class-name-that-exceeds-normal-length-limits-for-testing-purposes"
			pvc := &corev1.PersistentVolumeClaim{
				Spec: corev1.PersistentVolumeClaimSpec{
					StorageClassName: &longName,
				},
			}

			Expect(PVCMatchesStorageClass(pvc, longName)).To(BeTrue())
		})
	})

	Describe("Resource Requirements Edge Cases", func() {
		It("should handle PVC with only limits (no requests)", func() {
			pvc := &corev1.PersistentVolumeClaim{
				Spec: corev1.PersistentVolumeClaimSpec{
					Resources: corev1.VolumeResourceRequirements{
						Limits: corev1.ResourceList{
							corev1.ResourceStorage: resource.MustParse("10Gi"),
						},
						Requests: corev1.ResourceList{},
					},
				},
			}

			storageRequest := GetPVCStorageRequest(pvc)
			Expect(storageRequest.Value()).To(Equal(int64(0)))
		})

		It("should handle PVC with other resource types", func() {
			pvc := &corev1.PersistentVolumeClaim{
				Spec: corev1.PersistentVolumeClaimSpec{
					Resources: corev1.VolumeResourceRequirements{
						Requests: corev1.ResourceList{
							corev1.ResourceCPU:    resource.MustParse("100m"),
							corev1.ResourceMemory: resource.MustParse("128Mi"),
							// No storage request
						},
					},
				},
			}

			storageRequest := GetPVCStorageRequest(pvc)
			Expect(storageRequest.Value()).To(Equal(int64(0)))
		})
	})
})
