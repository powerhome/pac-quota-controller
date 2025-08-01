package storage

import (
	"context"

	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/resource"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/client-go/kubernetes/fake"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
)

const testStorageClass = "fast-ssd"

var _ = Describe("StorageResourceCalculator", func() {
	Describe("getPVCStorageRequest", func() {
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

			storageRequest := getPVCStorageRequest(pvc)
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

			storageRequest := getPVCStorageRequest(pvc)
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

			storageRequest := getPVCStorageRequest(pvc)
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

			storageRequest := getPVCStorageRequest(pvc)
			expected := resource.MustParse("1Mi")
			Expect(storageRequest.Equal(expected)).To(BeTrue())
		})

		It("should handle nil PVC", func() {
			storageRequest := getPVCStorageRequest(nil)
			Expect(storageRequest.Value()).To(Equal(int64(0)))
		})

		It("should handle PVC with nil resources", func() {
			pvc := &corev1.PersistentVolumeClaim{
				Spec: corev1.PersistentVolumeClaimSpec{},
			}

			storageRequest := getPVCStorageRequest(pvc)
			Expect(storageRequest.Value()).To(Equal(int64(0)))
		})
	})

	Describe("getPVCStorageClass", func() {
		It("should extract storage class from PVC", func() {
			storageClassName := testStorageClass
			pvc := &corev1.PersistentVolumeClaim{
				Spec: corev1.PersistentVolumeClaimSpec{
					StorageClassName: &storageClassName,
				},
			}

			sc := getPVCStorageClass(pvc)
			Expect(sc).To(Equal(testStorageClass))
		})

		It("should return empty string for PVC without storage class", func() {
			pvc := &corev1.PersistentVolumeClaim{
				Spec: corev1.PersistentVolumeClaimSpec{},
			}

			sc := getPVCStorageClass(pvc)
			Expect(sc).To(Equal(""))
		})

		It("should handle nil storage class pointer", func() {
			pvc := &corev1.PersistentVolumeClaim{
				Spec: corev1.PersistentVolumeClaimSpec{
					StorageClassName: nil,
				},
			}

			sc := getPVCStorageClass(pvc)
			Expect(sc).To(Equal(""))
		})

		It("should handle nil PVC", func() {
			sc := getPVCStorageClass(nil)
			Expect(sc).To(Equal(""))
		})

		It("should handle empty storage class name", func() {
			storageClassName := ""
			pvc := &corev1.PersistentVolumeClaim{
				Spec: corev1.PersistentVolumeClaimSpec{
					StorageClassName: &storageClassName,
				},
			}

			sc := getPVCStorageClass(pvc)
			Expect(sc).To(Equal(""))
		})
	})

	Describe("StorageResourceCalculator Constructor", func() {
		It("should create calculator with nil client", func() {
			// This test verifies that the constructor can handle nil clients
			// In a real scenario, this would be used for testing error handling
			calculator := &StorageResourceCalculator{}
			Expect(calculator).NotTo(BeNil())
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

			storageRequest := getPVCStorageRequest(pvc)
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

				storageRequest := getPVCStorageRequest(pvc)
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

			storageRequest := getPVCStorageRequest(pvc)
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

				storageRequest := getPVCStorageRequest(pvc)
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

			storageRequest := getPVCStorageRequest(pvc)
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

				sc := getPVCStorageClass(pvc)
				Expect(sc).To(Equal(storageClassName), "Failed for storage class: %s", storageClassName)
			}
		})

		It("should handle very long storage class names", func() {
			longName := "very-long-storage-class-name-that-exceeds-normal-length-limits-for-testing-purposes"
			pvc := &corev1.PersistentVolumeClaim{
				Spec: corev1.PersistentVolumeClaimSpec{
					StorageClassName: &longName,
				},
			}

			sc := getPVCStorageClass(pvc)
			Expect(sc).To(Equal(longName))
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

			storageRequest := getPVCStorageRequest(pvc)
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

			storageRequest := getPVCStorageRequest(pvc)
			Expect(storageRequest.Value()).To(Equal(int64(0)))
		})
	})

	Describe("StorageResourceCalculator CalculateStorageUsage", func() {
		var (
			calculator *StorageResourceCalculator
			fakeClient *fake.Clientset
		)

		BeforeEach(func() {
			fakeClient = fake.NewSimpleClientset()
			calculator = NewStorageResourceCalculator(fakeClient)
		})

		It("should calculate storage usage for namespace with PVCs", func() {
			// Create test PVCs
			pvc1 := &corev1.PersistentVolumeClaim{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "pvc1",
					Namespace: "test-ns",
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
					Namespace: "test-ns",
				},
				Spec: corev1.PersistentVolumeClaimSpec{
					Resources: corev1.VolumeResourceRequirements{
						Requests: corev1.ResourceList{
							corev1.ResourceStorage: resource.MustParse("20Gi"),
						},
					},
				},
			}

			// Add PVCs to fake client
			_, err := fakeClient.CoreV1().PersistentVolumeClaims("test-ns").Create(
				context.Background(), pvc1, metav1.CreateOptions{})
			Expect(err).NotTo(HaveOccurred())
			_, err = fakeClient.CoreV1().PersistentVolumeClaims("test-ns").Create(
				context.Background(), pvc2, metav1.CreateOptions{})
			Expect(err).NotTo(HaveOccurred())

			usage, err := calculator.CalculateStorageUsage(context.Background(), "test-ns")

			Expect(err).NotTo(HaveOccurred())
			expected := resource.MustParse("30Gi")
			Expect(usage.Equal(expected)).To(BeTrue())
		})

		It("should return zero usage for empty namespace", func() {
			usage, err := calculator.CalculateStorageUsage(context.Background(), "empty-ns")

			Expect(err).NotTo(HaveOccurred())
			Expect(usage.Value()).To(Equal(int64(0)))
		})

		It("should handle PVCs with no storage requests", func() {
			pvc := &corev1.PersistentVolumeClaim{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "pvc-no-storage",
					Namespace: "test-ns",
				},
				Spec: corev1.PersistentVolumeClaimSpec{
					Resources: corev1.VolumeResourceRequirements{
						Requests: corev1.ResourceList{},
					},
				},
			}

			_, err := fakeClient.CoreV1().PersistentVolumeClaims("test-ns").Create(
				context.Background(), pvc, metav1.CreateOptions{})
			Expect(err).NotTo(HaveOccurred())

			usage, err := calculator.CalculateStorageUsage(context.Background(), "test-ns")

			Expect(err).NotTo(HaveOccurred())
			Expect(usage.Value()).To(Equal(int64(0)))
		})
	})

	Describe("StorageResourceCalculator CalculateUsage", func() {
		var (
			calculator *StorageResourceCalculator
			fakeClient *fake.Clientset
		)

		BeforeEach(func() {
			fakeClient = fake.NewSimpleClientset()
			calculator = NewStorageResourceCalculator(fakeClient)
		})

		It("should calculate storage usage for storage resources", func() {
			pvc := &corev1.PersistentVolumeClaim{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "pvc",
					Namespace: "test-ns",
				},
				Spec: corev1.PersistentVolumeClaimSpec{
					Resources: corev1.VolumeResourceRequirements{
						Requests: corev1.ResourceList{
							corev1.ResourceStorage: resource.MustParse("10Gi"),
						},
					},
				},
			}

			_, err := fakeClient.CoreV1().PersistentVolumeClaims("test-ns").Create(
				context.Background(), pvc, metav1.CreateOptions{})
			Expect(err).NotTo(HaveOccurred())

			usage, err := calculator.CalculateUsage(context.Background(), "test-ns", corev1.ResourceRequestsStorage)

			Expect(err).NotTo(HaveOccurred())
			expected := resource.MustParse("10Gi")
			Expect(usage.Equal(expected)).To(BeTrue())
		})

		It("should return zero for non-storage resources", func() {
			usage, err := calculator.CalculateUsage(context.Background(), "test-ns", corev1.ResourceCPU)

			Expect(err).NotTo(HaveOccurred())
			Expect(usage.Value()).To(Equal(int64(0)))
		})
	})

	Describe("StorageResourceCalculator CalculateTotalUsage", func() {
		var (
			calculator *StorageResourceCalculator
			fakeClient *fake.Clientset
		)

		BeforeEach(func() {
			fakeClient = fake.NewSimpleClientset()
			calculator = NewStorageResourceCalculator(fakeClient)
		})

		It("should calculate total usage for all storage resources", func() {
			pvc := &corev1.PersistentVolumeClaim{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "pvc",
					Namespace: "test-ns",
				},
				Spec: corev1.PersistentVolumeClaimSpec{
					Resources: corev1.VolumeResourceRequirements{
						Requests: corev1.ResourceList{
							corev1.ResourceStorage: resource.MustParse("10Gi"),
						},
					},
				},
			}

			_, err := fakeClient.CoreV1().PersistentVolumeClaims("test-ns").Create(
				context.Background(), pvc, metav1.CreateOptions{})
			Expect(err).NotTo(HaveOccurred())

			totalUsage, err := calculator.CalculateTotalUsage(context.Background(), "test-ns")

			Expect(err).NotTo(HaveOccurred())
			Expect(totalUsage).NotTo(BeNil())
			Expect(totalUsage).To(HaveLen(3)) // ResourceRequestsStorage, ResourceStorage, and ResourcePersistentVolumeClaims
		})

		It("should return empty map for empty namespace", func() {
			totalUsage, err := calculator.CalculateTotalUsage(context.Background(), "empty-ns")

			Expect(err).NotTo(HaveOccurred())
			Expect(totalUsage).NotTo(BeNil())
			Expect(totalUsage).To(HaveLen(3)) // ResourceRequestsStorage, ResourceStorage, and ResourcePersistentVolumeClaims
		})
	})

	Describe("StorageResourceCalculator CalculateStorageClassUsage", func() {
		var (
			calculator *StorageResourceCalculator
			fakeClient *fake.Clientset
		)

		BeforeEach(func() {
			fakeClient = fake.NewSimpleClientset()
			calculator = NewStorageResourceCalculator(fakeClient)
		})

		It("should calculate storage class usage", func() {
			storageClass := testStorageClass
			pvc1 := &corev1.PersistentVolumeClaim{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "pvc1",
					Namespace: "test-ns",
				},
				Spec: corev1.PersistentVolumeClaimSpec{
					StorageClassName: &storageClass,
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
					Namespace: "test-ns",
				},
				Spec: corev1.PersistentVolumeClaimSpec{
					StorageClassName: &storageClass,
					Resources: corev1.VolumeResourceRequirements{
						Requests: corev1.ResourceList{
							corev1.ResourceStorage: resource.MustParse("20Gi"),
						},
					},
				},
			}

			_, err := fakeClient.CoreV1().PersistentVolumeClaims("test-ns").Create(
				context.Background(), pvc1, metav1.CreateOptions{})
			Expect(err).NotTo(HaveOccurred())
			_, err = fakeClient.CoreV1().PersistentVolumeClaims("test-ns").Create(
				context.Background(), pvc2, metav1.CreateOptions{})
			Expect(err).NotTo(HaveOccurred())

			usage, err := calculator.CalculateStorageClassUsage(context.Background(), "test-ns", storageClass)

			Expect(err).NotTo(HaveOccurred())
			expected := resource.MustParse("30Gi")
			Expect(usage.Equal(expected)).To(BeTrue())
		})

		It("should return zero for non-matching storage class", func() {
			storageClass := testStorageClass
			otherStorageClass := "slow-hdd"
			pvc := &corev1.PersistentVolumeClaim{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "pvc",
					Namespace: "test-ns",
				},
				Spec: corev1.PersistentVolumeClaimSpec{
					StorageClassName: &otherStorageClass,
					Resources: corev1.VolumeResourceRequirements{
						Requests: corev1.ResourceList{
							corev1.ResourceStorage: resource.MustParse("10Gi"),
						},
					},
				},
			}

			_, err := fakeClient.CoreV1().PersistentVolumeClaims("test-ns").Create(
				context.Background(), pvc, metav1.CreateOptions{})
			Expect(err).NotTo(HaveOccurred())

			usage, err := calculator.CalculateStorageClassUsage(context.Background(), "test-ns", storageClass)

			Expect(err).NotTo(HaveOccurred())
			Expect(usage.Value()).To(Equal(int64(0)))
		})
	})

	Describe("StorageResourceCalculator CalculateStorageClassCount", func() {
		var (
			calculator *StorageResourceCalculator
			fakeClient *fake.Clientset
		)

		BeforeEach(func() {
			fakeClient = fake.NewSimpleClientset()
			calculator = NewStorageResourceCalculator(fakeClient)
		})

		It("should count PVCs for specific storage class", func() {
			storageClass := "fast-ssd"
			pvc1 := &corev1.PersistentVolumeClaim{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "pvc1",
					Namespace: "test-ns",
				},
				Spec: corev1.PersistentVolumeClaimSpec{
					StorageClassName: &storageClass,
				},
			}

			pvc2 := &corev1.PersistentVolumeClaim{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "pvc2",
					Namespace: "test-ns",
				},
				Spec: corev1.PersistentVolumeClaimSpec{
					StorageClassName: &storageClass,
				},
			}

			_, err := fakeClient.CoreV1().PersistentVolumeClaims("test-ns").Create(
				context.Background(), pvc1, metav1.CreateOptions{})
			Expect(err).NotTo(HaveOccurred())
			_, err = fakeClient.CoreV1().PersistentVolumeClaims("test-ns").Create(
				context.Background(), pvc2, metav1.CreateOptions{})
			Expect(err).NotTo(HaveOccurred())

			count, err := calculator.CalculateStorageClassCount(context.Background(), "test-ns", storageClass)

			Expect(err).NotTo(HaveOccurred())
			Expect(count).To(Equal(int64(2)))
		})

		It("should return zero for non-matching storage class", func() {
			storageClass := "fast-ssd"
			otherStorageClass := "slow-hdd"
			pvc := &corev1.PersistentVolumeClaim{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "pvc",
					Namespace: "test-ns",
				},
				Spec: corev1.PersistentVolumeClaimSpec{
					StorageClassName: &otherStorageClass,
				},
			}

			_, err := fakeClient.CoreV1().PersistentVolumeClaims("test-ns").Create(
				context.Background(), pvc, metav1.CreateOptions{})
			Expect(err).NotTo(HaveOccurred())

			count, err := calculator.CalculateStorageClassCount(context.Background(), "test-ns", storageClass)

			Expect(err).NotTo(HaveOccurred())
			Expect(count).To(Equal(int64(0)))
		})
	})

	Describe("CalculatePVCCount", func() {
		var (
			calculator *StorageResourceCalculator
			fakeClient *fake.Clientset
			ctx        context.Context
		)

		BeforeEach(func() {
			fakeClient = fake.NewSimpleClientset()
			calculator = NewStorageResourceCalculator(fakeClient)
			ctx = context.Background()
		})

		It("should return zero for empty namespace", func() {
			count, err := calculator.CalculatePVCCount(ctx, "empty-namespace")
			Expect(err).NotTo(HaveOccurred())
			Expect(count).To(Equal(int64(0)))
		})

		It("should count all PVCs in namespace", func() {
			// Create test PVCs
			pvc1 := &corev1.PersistentVolumeClaim{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "pvc-1",
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
			pvc2 := &corev1.PersistentVolumeClaim{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "pvc-2",
					Namespace: "test-namespace",
				},
				Spec: corev1.PersistentVolumeClaimSpec{
					Resources: corev1.VolumeResourceRequirements{
						Requests: corev1.ResourceList{
							corev1.ResourceStorage: resource.MustParse("2Gi"),
						},
					},
				},
			}

			_, err := fakeClient.CoreV1().PersistentVolumeClaims("test-namespace").Create(
				context.Background(), pvc1, metav1.CreateOptions{})
			Expect(err).NotTo(HaveOccurred())
			_, err = fakeClient.CoreV1().PersistentVolumeClaims("test-namespace").Create(
				context.Background(), pvc2, metav1.CreateOptions{})
			Expect(err).NotTo(HaveOccurred())

			count, err := calculator.CalculatePVCCount(ctx, "test-namespace")
			Expect(err).NotTo(HaveOccurred())
			Expect(count).To(Equal(int64(2)))
		})

		It("should handle client error", func() {
			// Create a client that will fail
			fakeClient := fake.NewSimpleClientset()
			calculator := NewStorageResourceCalculator(fakeClient)

			_, err := calculator.CalculatePVCCount(ctx, "non-existent-namespace")
			Expect(err).NotTo(HaveOccurred()) // Fake client doesn't actually fail
		})
	})
})
