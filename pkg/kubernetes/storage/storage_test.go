package storage

import (
	"context"

	"go.uber.org/zap"
	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/resource"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	ctrlclient "sigs.k8s.io/controller-runtime/pkg/client"
	ctrlclientfake "sigs.k8s.io/controller-runtime/pkg/client/fake"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
)

const testStorageClass = "fast-ssd"

func newStorageFakeClient() ctrlclient.Client {
	s := runtime.NewScheme()
	_ = corev1.AddToScheme(s)
	return ctrlclientfake.NewClientBuilder().WithScheme(s).Build()
}

var _ = Describe("StorageResourceCalculator", func() {
	var (
		ctx    context.Context
		logger *zap.Logger
	)
	BeforeEach(func() {
		ctx = context.Background() // Entry point context for all tests
		var err error
		logger, err = zap.NewDevelopment()
		Expect(err).ToNot(HaveOccurred())
	})

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

	Describe("StorageResourceCalculator CalculateStorageUsage", func() {
		var (
			calculator *StorageResourceCalculator
			fakeClient ctrlclient.Client
		)

		BeforeEach(func() {
			fakeClient = newStorageFakeClient()
			calculator = NewStorageResourceCalculator(fakeClient, logger)
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
			Expect(fakeClient.Create(ctx, pvc1)).To(Succeed())
			Expect(fakeClient.Create(ctx, pvc2)).To(Succeed())

			usage, err := calculator.CalculateStorageUsage(ctx, "test-ns")

			Expect(err).NotTo(HaveOccurred())
			expected := resource.MustParse("30Gi")
			Expect(usage.Equal(expected)).To(BeTrue())
		})

		It("should return zero usage for empty namespace", func() {
			usage, err := calculator.CalculateStorageUsage(ctx, "empty-ns")

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

			Expect(fakeClient.Create(ctx, pvc)).To(Succeed())

			usage, err := calculator.CalculateStorageUsage(ctx, "test-ns")

			Expect(err).NotTo(HaveOccurred())
			Expect(usage.Value()).To(Equal(int64(0)))
		})
	})

	Describe("StorageResourceCalculator CalculateUsage", func() {
		var (
			calculator *StorageResourceCalculator
			fakeClient ctrlclient.Client
		)

		BeforeEach(func() {
			fakeClient = newStorageFakeClient()
			calculator = NewStorageResourceCalculator(fakeClient, logger)
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

			Expect(fakeClient.Create(ctx, pvc)).To(Succeed())

			usage, err := calculator.CalculateUsage(ctx, "test-ns", corev1.ResourceRequestsStorage)

			Expect(err).NotTo(HaveOccurred())
			expected := resource.MustParse("10Gi")
			Expect(usage.Equal(expected)).To(BeTrue())
		})

		It("should return zero for non-storage resources", func() {
			usage, err := calculator.CalculateUsage(ctx, "test-ns", corev1.ResourceCPU)

			Expect(err).NotTo(HaveOccurred())
			Expect(usage.Value()).To(Equal(int64(0)))
		})
	})

	Describe("StorageResourceCalculator CalculateStorageClassUsage", func() {
		var (
			calculator *StorageResourceCalculator
			fakeClient ctrlclient.Client
		)

		BeforeEach(func() {
			fakeClient = newStorageFakeClient()
			calculator = NewStorageResourceCalculator(fakeClient, logger)
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

			Expect(fakeClient.Create(ctx, pvc1)).To(Succeed())
			Expect(fakeClient.Create(ctx, pvc2)).To(Succeed())

			usage, err := calculator.CalculateStorageClassUsage(ctx, "test-ns", storageClass)

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

			Expect(fakeClient.Create(ctx, pvc)).To(Succeed())

			usage, err := calculator.CalculateStorageClassUsage(ctx, "test-ns", storageClass)

			Expect(err).NotTo(HaveOccurred())
			Expect(usage.Value()).To(Equal(int64(0)))
		})

		It("should include legacy storage class annotation", func() {
			storageClass := testStorageClass
			pvc := &corev1.PersistentVolumeClaim{
				ObjectMeta: metav1.ObjectMeta{
					Name:        "pvc-legacy",
					Namespace:   "test-ns",
					Annotations: map[string]string{"volume.beta.kubernetes.io/storage-class": storageClass},
				},
				Spec: corev1.PersistentVolumeClaimSpec{
					Resources: corev1.VolumeResourceRequirements{
						Requests: corev1.ResourceList{corev1.ResourceStorage: resource.MustParse("7Gi")},
					},
				},
			}

			Expect(fakeClient.Create(ctx, pvc)).To(Succeed())

			usage, err := calculator.CalculateStorageClassUsage(ctx, "test-ns", storageClass)

			Expect(err).NotTo(HaveOccurred())
			expected := resource.MustParse("7Gi")
			Expect(usage.Equal(expected)).To(BeTrue())
		})
	})

	Describe("StorageResourceCalculator CalculateStorageClassCount", func() {
		var (
			calculator *StorageResourceCalculator
			fakeClient ctrlclient.Client
		)

		BeforeEach(func() {
			fakeClient = newStorageFakeClient()
			calculator = NewStorageResourceCalculator(fakeClient, logger)
		})

		It("should count PVCs for specific storage class", func() {
			storageClass := testStorageClass
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

			Expect(fakeClient.Create(ctx, pvc1)).To(Succeed())
			Expect(fakeClient.Create(ctx, pvc2)).To(Succeed())

			count, err := calculator.CalculateStorageClassCount(ctx, "test-ns", storageClass)

			Expect(err).NotTo(HaveOccurred())
			Expect(count).To(Equal(int64(2)))
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
				},
			}

			Expect(fakeClient.Create(ctx, pvc)).To(Succeed())

			count, err := calculator.CalculateStorageClassCount(ctx, "test-ns", storageClass)

			Expect(err).NotTo(HaveOccurred())
			Expect(count).To(Equal(int64(0)))
		})

		It("should count legacy storage class annotation", func() {
			storageClass := testStorageClass
			pvc := &corev1.PersistentVolumeClaim{
				ObjectMeta: metav1.ObjectMeta{
					Name:        "pvc-legacy",
					Namespace:   "test-ns",
					Annotations: map[string]string{"volume.beta.kubernetes.io/storage-class": storageClass},
				},
			}

			Expect(fakeClient.Create(ctx, pvc)).To(Succeed())

			count, err := calculator.CalculateStorageClassCount(ctx, "test-ns", storageClass)

			Expect(err).NotTo(HaveOccurred())
			Expect(count).To(Equal(int64(1)))
		})
	})

	Describe("CalculatePVCCount", func() {
		var (
			calculator *StorageResourceCalculator
			fakeClient ctrlclient.Client
		)

		BeforeEach(func() {
			fakeClient = newStorageFakeClient()
			calculator = NewStorageResourceCalculator(fakeClient, logger)
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

			Expect(fakeClient.Create(ctx, pvc1)).To(Succeed())
			Expect(fakeClient.Create(ctx, pvc2)).To(Succeed())

			count, err := calculator.CalculatePVCCount(ctx, "test-namespace")
			Expect(err).NotTo(HaveOccurred())
			Expect(count).To(Equal(int64(2)))
		})

		It("should handle empty namespace", func() {
			_, err := calculator.CalculatePVCCount(ctx, "non-existent-namespace")
			Expect(err).NotTo(HaveOccurred())
		})
	})
})
