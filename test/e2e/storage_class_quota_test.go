package e2e

import (
	"fmt"
	"time"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"

	corev1 "k8s.io/api/core/v1"
	storagev1 "k8s.io/api/storage/v1"
	"k8s.io/apimachinery/pkg/api/resource"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"

	quotav1alpha1 "github.com/powerhome/pac-quota-controller/api/v1alpha1"
	testutils "github.com/powerhome/pac-quota-controller/test/utils"
)

var _ = Describe("Storage Class Quota E2E Tests", func() {
	var (
		testNamespace      string
		storageClassFast   string
		storageClassSlow   string
		storageClassCustom string
		crqName            string
	)

	createTestStorageClasses := func() {
		storageClasses := []*storagev1.StorageClass{
			{
				ObjectMeta:  metav1.ObjectMeta{Name: storageClassFast},
				Provisioner: "kubernetes.io/no-provisioner",
				Parameters:  map[string]string{"type": "fast-ssd"},
			},
			{
				ObjectMeta:  metav1.ObjectMeta{Name: storageClassSlow},
				Provisioner: "kubernetes.io/no-provisioner",
				Parameters:  map[string]string{"type": "slow-hdd"},
			},
			{
				ObjectMeta:  metav1.ObjectMeta{Name: storageClassCustom},
				Provisioner: "kubernetes.io/no-provisioner",
				Parameters:  map[string]string{"type": "custom-nvme"},
			},
		}

		for _, sc := range storageClasses {
			_ = k8sClient.Create(ctx, sc) // Ignore errors if already exists
		}
	}

	cleanupTestStorageClasses := func() {
		storageClassNames := []string{storageClassFast, storageClassSlow, storageClassCustom}
		for _, name := range storageClassNames {
			sc := &storagev1.StorageClass{ObjectMeta: metav1.ObjectMeta{Name: name}}
			_ = k8sClient.Delete(ctx, sc) // Ignore errors
		}
	}

	BeforeEach(func() {
		testNamespace = fmt.Sprintf("storage-class-test-%s", testutils.GenerateTestSuffix())
		storageClassFast = fmt.Sprintf("fast-ssd-e2e-%s", testutils.GenerateTestSuffix())
		storageClassSlow = fmt.Sprintf("slow-hdd-e2e-%s", testutils.GenerateTestSuffix())
		storageClassCustom = fmt.Sprintf("custom-nvme-e2e-%s", testutils.GenerateTestSuffix())
		crqName = fmt.Sprintf("storage-class-crq-%s", testutils.GenerateTestSuffix())

		// Create test namespace
		ns := &corev1.Namespace{
			ObjectMeta: metav1.ObjectMeta{
				Name: testNamespace,
				Labels: map[string]string{
					"storage-class-test": "enabled",
					"e2e-test":           "storage-quota",
				},
			},
		}
		Expect(k8sClient.Create(ctx, ns)).To(Succeed())

		// Create test storage classes
		createTestStorageClasses()
	})

	AfterEach(func() {
		// Clean up namespace
		ns := &corev1.Namespace{ObjectMeta: metav1.ObjectMeta{Name: testNamespace}}
		_ = k8sClient.Delete(ctx, ns)

		// Clean up CRQ
		crq := &quotav1alpha1.ClusterResourceQuota{ObjectMeta: metav1.ObjectMeta{Name: crqName}}
		_ = k8sClient.Delete(ctx, crq)

		// Clean up storage classes
		cleanupTestStorageClasses()
	})

	Describe("Storage Class Specific Quotas", func() {
		It("should enforce storage class specific storage quotas", func() {
			// Create ClusterResourceQuota with storage class specific limits
			crq := &quotav1alpha1.ClusterResourceQuota{
				ObjectMeta: metav1.ObjectMeta{
					Name: crqName,
				},
				Spec: quotav1alpha1.ClusterResourceQuotaSpec{
					NamespaceSelector: &metav1.LabelSelector{
						MatchLabels: map[string]string{
							"storage-class-test": "enabled",
						},
					},
					Hard: quotav1alpha1.ResourceList{
						// General quotas
						corev1.ResourceRequestsStorage:        resource.MustParse("20Gi"),
						corev1.ResourcePersistentVolumeClaims: resource.MustParse("10"), // Storage class specific quotas
						corev1.ResourceName(fmt.Sprintf(
							"%s.storageclass.storage.k8s.io/requests.storage", storageClassFast,
						)): resource.MustParse("5Gi"),
						corev1.ResourceName(fmt.Sprintf(
							"%s.storageclass.storage.k8s.io/persistentvolumeclaims", storageClassFast,
						)): resource.MustParse("3"),
						corev1.ResourceName(fmt.Sprintf(
							"%s.storageclass.storage.k8s.io/requests.storage", storageClassSlow,
						)): resource.MustParse("10Gi"),
						corev1.ResourceName(fmt.Sprintf(
							"%s.storageclass.storage.k8s.io/persistentvolumeclaims", storageClassSlow,
						)): resource.MustParse("2"),
					},
				},
			}
			Expect(k8sClient.Create(ctx, crq)).To(Succeed())

			By("Creating first fast SSD PVC within limits")
			pvc1 := createTestPVC("fast-pvc-1", testNamespace, storageClassFast, "2Gi")
			Expect(k8sClient.Create(ctx, pvc1)).To(Succeed(), "Should allow first fast SSD PVC within limits")

			By("Creating second fast SSD PVC within limits")
			pvc2 := createTestPVC("fast-pvc-2", testNamespace, storageClassFast, "2Gi")
			Expect(k8sClient.Create(ctx, pvc2)).To(Succeed(), "Should allow second fast SSD PVC within limits")

			By("Trying to create fast SSD PVC that exceeds storage quota (should fail)")
			pvc3 := createTestPVC("fast-pvc-3", testNamespace, storageClassFast, "2Gi") // Total would be 6Gi > 5Gi limit
			err := k8sClient.Create(ctx, pvc3)
			Expect(err).To(HaveOccurred(), "Should block PVC that exceeds fast SSD storage quota")
			Expect(err.Error()).To(
				ContainSubstring("ClusterResourceQuota storage class '"+storageClassFast+"' storage validation failed"),
				"Error should mention storage class storage limit",
			)

			By("Creating third fast SSD PVC within both storage and count limits")
			pvc4 := createTestPVC("fast-pvc-4", testNamespace, storageClassFast, "500Mi") // Within storage but count would be 3
			Expect(k8sClient.Create(ctx, pvc4)).To(Succeed(), "Should allow third fast SSD PVC within both limits")

			By("Trying to create fast SSD PVC that exceeds count quota (should fail)")
			pvc5 := createTestPVC("fast-pvc-5", testNamespace, storageClassFast, "100Mi") // Count would be 4 > 3 limit
			err = k8sClient.Create(ctx, pvc5)
			Expect(err).To(HaveOccurred(), "Should block PVC that exceeds fast SSD count quota")
			Expect(err.Error()).To(
				ContainSubstring("ClusterResourceQuota storage class '"+storageClassFast+"' PVC count validation failed"),
				"Error should mention storage class PVC count limit",
			)

			By("Creating slow HDD PVC with different quota")
			slowPVC1 := createTestPVC("slow-pvc-1", testNamespace, storageClassSlow, "8Gi")
			Expect(k8sClient.Create(ctx, slowPVC1)).To(Succeed(), "Should allow slow HDD PVC with different quota")

			By("Creating second slow HDD PVC within limits")
			slowPVC2 := createTestPVC("slow-pvc-2", testNamespace, storageClassSlow, "1Gi")
			Expect(k8sClient.Create(ctx, slowPVC2)).To(Succeed(), "Should allow second slow HDD PVC within limits")

			By("Trying to create slow HDD PVC that exceeds count quota (should fail)")
			slowPVC3 := createTestPVC("slow-pvc-3", testNamespace, storageClassSlow, "500Mi") // Count would be 3 > 2 limit
			err = k8sClient.Create(ctx, slowPVC3)
			Expect(err).To(HaveOccurred(), "Should block PVC that exceeds slow HDD count quota")
			Expect(err.Error()).To(
				ContainSubstring("ClusterResourceQuota storage class '"+storageClassSlow+"' PVC count validation failed"),
				"Error should mention storage class PVC count limit",
			)

			By("Creating PVC without storage class (should only count against general quotas)")
			defaultPVC := createTestPVC("default-pvc", testNamespace, "", "3Gi")
			Expect(k8sClient.Create(ctx, defaultPVC)).To(Succeed(), "Should allow PVC without storage class")

			By("Creating PVC with storage class not covered by quota (should only count against general quotas)")
			customPVC := createTestPVC("custom-pvc", testNamespace, storageClassCustom, "2Gi")
			Expect(k8sClient.Create(ctx, customPVC)).To(Succeed(), "Should allow PVC with unquoted storage class")

			By("Verifying ClusterResourceQuota status reflects usage")
			Eventually(func() bool {
				updatedCRQ := &quotav1alpha1.ClusterResourceQuota{}
				err := k8sClient.Get(ctx, types.NamespacedName{Name: crqName}, updatedCRQ)
				if err != nil {
					return false
				}
				// Check if status is being updated by the controller
				return len(updatedCRQ.Status.Namespaces) > 0
			}, time.Minute, time.Second*5).Should(BeTrue(), "ClusterResourceQuota status should be updated")
		})

		It("should handle multiple storage classes with different quota configurations", func() {
			// Create ClusterResourceQuota with mixed quota types
			crq := &quotav1alpha1.ClusterResourceQuota{
				ObjectMeta: metav1.ObjectMeta{
					Name: crqName,
				},
				Spec: quotav1alpha1.ClusterResourceQuotaSpec{
					NamespaceSelector: &metav1.LabelSelector{
						MatchLabels: map[string]string{
							"storage-class-test": "enabled",
						},
					}, Hard: quotav1alpha1.ResourceList{
						// Only storage quota for fast SSD (no count limit)
						corev1.ResourceName(fmt.Sprintf(
							"%s.storageclass.storage.k8s.io/requests.storage", storageClassFast,
						)): resource.MustParse("3Gi"),

						// Only count quota for slow HDD (no storage limit)
						corev1.ResourceName(fmt.Sprintf(
							"%s.storageclass.storage.k8s.io/persistentvolumeclaims", storageClassSlow,
						)): resource.MustParse("2"),

						// Both quotas for custom storage class
						corev1.ResourceName(fmt.Sprintf(
							"%s.storageclass.storage.k8s.io/requests.storage", storageClassCustom,
						)): resource.MustParse("2Gi"),
						corev1.ResourceName(fmt.Sprintf(
							"%s.storageclass.storage.k8s.io/persistentvolumeclaims", storageClassCustom,
						)): resource.MustParse("1"),
					},
				},
			}
			Expect(k8sClient.Create(ctx, crq)).To(Succeed())

			By("Creating fast SSD PVC #1 (only storage quota applies)")
			fastPVC1 := createTestPVC("fast-mixed-1", testNamespace, storageClassFast, "1Gi")
			Expect(k8sClient.Create(ctx, fastPVC1)).To(Succeed(), "Should allow fast SSD PVC #1 within storage quota")

			By("Creating fast SSD PVC #2 (only storage quota applies)")
			fastPVC2 := createTestPVC("fast-mixed-2", testNamespace, storageClassFast, "1Gi")
			Expect(k8sClient.Create(ctx, fastPVC2)).To(Succeed(), "Should allow fast SSD PVC #2 within storage quota")

			By("Creating fast SSD PVC #3 (only storage quota applies)")
			fastPVC3 := createTestPVC("fast-mixed-3", testNamespace, storageClassFast, "100Mi")
			Expect(k8sClient.Create(ctx, fastPVC3)).To(Succeed(), "Should allow fast SSD PVC #3 within storage quota")

			By("Trying to create fast SSD PVC #4 that exceeds storage quota (should fail)")
			fastPVC4 := createTestPVC("fast-mixed-4", testNamespace, storageClassFast, "1Gi")
			err := k8sClient.Create(ctx, fastPVC4)
			Expect(err).To(HaveOccurred(), "Should block when fast SSD storage quota exceeded")
			Expect(err.Error()).To(
				ContainSubstring("ClusterResourceQuota storage class '"+storageClassFast+"' storage validation failed"),
				"Error should mention storage class storage limit",
			)

			By("Creating slow HDD PVC #1 (only count quota applies)")
			slowPVC1 := createTestPVC("slow-mixed-1", testNamespace, storageClassSlow, "100Gi")
			Expect(k8sClient.Create(ctx, slowPVC1)).To(Succeed(), "Should allow slow HDD PVC #1 within count quota")

			By("Creating slow HDD PVC #2 (only count quota applies)")
			slowPVC2 := createTestPVC("slow-mixed-2", testNamespace, storageClassSlow, "200Gi")
			Expect(k8sClient.Create(ctx, slowPVC2)).To(Succeed(), "Should allow slow HDD PVC #2 within count quota")

			By("Trying to create slow HDD PVC #3 that exceeds count quota (should fail)")
			slowPVC3 := createTestPVC("slow-mixed-3", testNamespace, storageClassSlow, "1Mi")
			err = k8sClient.Create(ctx, slowPVC3)
			Expect(err).To(HaveOccurred(), "Should block when slow HDD count quota exceeded")
			Expect(err.Error()).To(
				ContainSubstring("ClusterResourceQuota storage class '"+storageClassSlow+"' PVC count validation failed"),
				"Error should mention storage class PVC count limit",
			)

			By("Creating custom storage class PVC #1 (both quotas apply)")
			customPVC1 := createTestPVC("custom-mixed-1", testNamespace, storageClassCustom, "1Gi")
			Expect(k8sClient.Create(ctx, customPVC1)).To(Succeed(), "Should allow custom storage class PVC #1 within quotas")

			By("Trying to create custom storage class PVC #2 that exceeds count quota (should fail)")
			customPVC2 := createTestPVC("custom-mixed-2", testNamespace, storageClassCustom, "500Mi")
			err = k8sClient.Create(ctx, customPVC2)
			Expect(err).To(HaveOccurred(), "Should block when custom storage class count quota exceeded")
			Expect(err.Error()).To(
				ContainSubstring("ClusterResourceQuota storage class '"+storageClassCustom+"' PVC count validation failed"),
				"Error should mention storage class PVC count limit",
			)
		})
	})
})

func createTestPVC(name, namespace, storageClass, size string) *corev1.PersistentVolumeClaim {
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
					corev1.ResourceStorage: resource.MustParse(size),
				},
			},
		},
	}

	if storageClass != "" {
		pvc.Spec.StorageClassName = &storageClass
	}

	return pvc
}
