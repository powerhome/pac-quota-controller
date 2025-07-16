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

package v1alpha1

import (
	"context"
	"fmt"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"

	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/resource"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/client/fake"

	quotav1alpha1 "github.com/powerhome/pac-quota-controller/api/v1alpha1"
	"github.com/powerhome/pac-quota-controller/pkg/kubernetes/storage"
)

var _ = Describe("PVC Webhook Quota Validation", func() {
	var (
		ctx       context.Context
		validator *PersistentVolumeClaimCustomValidator
		defaulter *PersistentVolumeClaimCustomDefaulter
		k8sClient client.Client
		scheme    *runtime.Scheme
		testNS    *corev1.Namespace
		testCRQ   *quotav1alpha1.ClusterResourceQuota
	)

	BeforeEach(func() {
		ctx = context.Background()
		scheme = runtime.NewScheme()
		Expect(corev1.AddToScheme(scheme)).To(Succeed())
		Expect(quotav1alpha1.AddToScheme(scheme)).To(Succeed())

		// Create test namespace
		testNS = &corev1.Namespace{
			ObjectMeta: metav1.ObjectMeta{
				Name: "test-namespace",
				Labels: map[string]string{
					"quota-test": "pvc-webhook",
				},
			},
		}

		// Create test ClusterResourceQuota
		testCRQ = &quotav1alpha1.ClusterResourceQuota{
			ObjectMeta: metav1.ObjectMeta{
				Name: "test-crq",
			},
			Spec: quotav1alpha1.ClusterResourceQuotaSpec{
				Hard: quotav1alpha1.ResourceList{
					corev1.ResourceRequestsStorage:        resource.MustParse("5Gi"),
					corev1.ResourcePersistentVolumeClaims: resource.MustParse("3"),
				},
				NamespaceSelector: &metav1.LabelSelector{
					MatchLabels: map[string]string{
						"quota-test": "pvc-webhook",
					},
				},
			},
		}

		k8sClient = fake.NewClientBuilder().
			WithScheme(scheme).
			WithObjects(testNS, testCRQ).
			Build()

		storageCalculator := &storage.StorageResourceCalculator{
			Client: k8sClient,
		}

		validator = &PersistentVolumeClaimCustomValidator{
			Client:            k8sClient,
			StorageCalculator: storageCalculator,
		}

		defaulter = &PersistentVolumeClaimCustomDefaulter{}
	})

	Describe("ValidateCreate", func() {
		It("should allow PVC creation within storage limits", func() {
			pvc := &corev1.PersistentVolumeClaim{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "test-pvc",
					Namespace: "test-namespace",
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

			warnings, err := validator.ValidateCreate(ctx, pvc)
			Expect(err).NotTo(HaveOccurred())
			Expect(warnings).To(BeEmpty())
		})

		It("should block PVC creation when storage quota would be exceeded", func() {
			// First create existing PVCs that use most of the quota
			existingPVC := &corev1.PersistentVolumeClaim{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "existing-pvc",
					Namespace: "test-namespace",
				},
				Spec: corev1.PersistentVolumeClaimSpec{
					Resources: corev1.VolumeResourceRequirements{
						Requests: corev1.ResourceList{
							corev1.ResourceStorage: resource.MustParse("4Gi"),
						},
					},
				},
			}
			Expect(k8sClient.Create(ctx, existingPVC)).To(Succeed())

			// Try to create another PVC that would exceed the quota
			newPVC := &corev1.PersistentVolumeClaim{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "new-pvc",
					Namespace: "test-namespace",
				},
				Spec: corev1.PersistentVolumeClaimSpec{
					Resources: corev1.VolumeResourceRequirements{
						Requests: corev1.ResourceList{
							corev1.ResourceStorage: resource.MustParse("2Gi"), // 4Gi + 2Gi > 5Gi limit
						},
					},
				},
			}

			warnings, err := validator.ValidateCreate(ctx, newPVC)
			Expect(err).To(HaveOccurred())
			Expect(err.Error()).To(ContainSubstring("ClusterResourceQuota"))
			Expect(err.Error()).To(ContainSubstring("storage limit"))
			Expect(warnings).To(BeNil())
		})

		It("should block PVC creation when PVC count quota would be exceeded", func() {
			// Create 3 PVCs to reach the limit
			for i := 0; i < 3; i++ {
				pvc := &corev1.PersistentVolumeClaim{
					ObjectMeta: metav1.ObjectMeta{
						Name:      fmt.Sprintf("existing-pvc-%d", i),
						Namespace: "test-namespace",
					},
					Spec: corev1.PersistentVolumeClaimSpec{
						Resources: corev1.VolumeResourceRequirements{
							Requests: corev1.ResourceList{
								corev1.ResourceStorage: resource.MustParse("500Mi"),
							},
						},
					},
				}
				Expect(k8sClient.Create(ctx, pvc)).To(Succeed())
			}

			// Try to create another PVC
			newPVC := &corev1.PersistentVolumeClaim{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "new-pvc",
					Namespace: "test-namespace",
				},
				Spec: corev1.PersistentVolumeClaimSpec{
					Resources: corev1.VolumeResourceRequirements{
						Requests: corev1.ResourceList{
							corev1.ResourceStorage: resource.MustParse("100Mi"),
						},
					},
				},
			}

			warnings, err := validator.ValidateCreate(ctx, newPVC)
			Expect(err).To(HaveOccurred())
			Expect(err.Error()).To(ContainSubstring("ClusterResourceQuota"))
			Expect(err.Error()).To(ContainSubstring("PVC count limit"))
			Expect(warnings).To(BeNil())
		})

		It("should allow PVC creation in namespaces not matching the selector", func() {
			// Create a namespace without matching labels
			nonQuotaNS := &corev1.Namespace{
				ObjectMeta: metav1.ObjectMeta{
					Name: "non-quota-namespace",
					Labels: map[string]string{
						"app": "test", // Different label
					},
				},
			}
			Expect(k8sClient.Create(ctx, nonQuotaNS)).To(Succeed())

			pvc := &corev1.PersistentVolumeClaim{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "test-pvc",
					Namespace: "non-quota-namespace",
				},
				Spec: corev1.PersistentVolumeClaimSpec{
					Resources: corev1.VolumeResourceRequirements{
						Requests: corev1.ResourceList{
							corev1.ResourceStorage: resource.MustParse("10Gi"), // Much larger than quota
						},
					},
				},
			}

			warnings, err := validator.ValidateCreate(ctx, pvc)
			Expect(err).NotTo(HaveOccurred())
			Expect(warnings).To(BeEmpty())
		})

		It("should handle PVC without storage request", func() {
			pvc := &corev1.PersistentVolumeClaim{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "test-pvc-no-storage",
					Namespace: "test-namespace",
				},
				Spec: corev1.PersistentVolumeClaimSpec{
					AccessModes: []corev1.PersistentVolumeAccessMode{
						corev1.ReadWriteOnce,
					},
					// No Resources specified
				},
			}

			warnings, err := validator.ValidateCreate(ctx, pvc)
			Expect(err).NotTo(HaveOccurred())
			Expect(warnings).To(BeEmpty())
		})

		It("should allow PVC creation with storage class specific quotas within limits", func() {
			// Create a ClusterResourceQuota with storage class specific limits
			storageClassCRQ := &quotav1alpha1.ClusterResourceQuota{
				ObjectMeta: metav1.ObjectMeta{
					Name: "storage-class-crq",
				},
				Spec: quotav1alpha1.ClusterResourceQuotaSpec{
					Hard: quotav1alpha1.ResourceList{
						// Storage class specific quotas
						corev1.ResourceName("fast-ssd.storageclass.storage.k8s.io/requests.storage"):       resource.MustParse("5Gi"),
						corev1.ResourceName("fast-ssd.storageclass.storage.k8s.io/persistentvolumeclaims"): resource.MustParse("3"),
					},
					NamespaceSelector: &metav1.LabelSelector{
						MatchLabels: map[string]string{
							"quota-test": "pvc-webhook",
						},
					},
				},
			}
			Expect(k8sClient.Create(ctx, storageClassCRQ)).To(Succeed())

			// Create PVC with fast-ssd storage class within limits
			fastSSDStorageClass := "fast-ssd"
			pvc := &corev1.PersistentVolumeClaim{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "fast-ssd-pvc",
					Namespace: "test-namespace",
				},
				Spec: corev1.PersistentVolumeClaimSpec{
					AccessModes: []corev1.PersistentVolumeAccessMode{
						corev1.ReadWriteOnce,
					},
					StorageClassName: &fastSSDStorageClass,
					Resources: corev1.VolumeResourceRequirements{
						Requests: corev1.ResourceList{
							corev1.ResourceStorage: resource.MustParse("2Gi"), // Within 5Gi limit
						},
					},
				},
			}

			warnings, err := validator.ValidateCreate(ctx, pvc)
			Expect(err).NotTo(HaveOccurred())
			Expect(warnings).To(BeEmpty())
		})

		It("should block PVC creation when storage class specific storage quota would be exceeded", func() {
			// Create a ClusterResourceQuota with storage class specific limits
			storageClassCRQ := &quotav1alpha1.ClusterResourceQuota{
				ObjectMeta: metav1.ObjectMeta{
					Name: "storage-class-storage-crq",
				},
				Spec: quotav1alpha1.ClusterResourceQuotaSpec{
					Hard: quotav1alpha1.ResourceList{
						corev1.ResourceName("premium.storageclass.storage.k8s.io/requests.storage"): resource.MustParse("3Gi"),
					},
					NamespaceSelector: &metav1.LabelSelector{
						MatchLabels: map[string]string{
							"quota-test": "pvc-webhook",
						},
					},
				},
			}
			Expect(k8sClient.Create(ctx, storageClassCRQ)).To(Succeed())

			// First create existing PVC with premium storage class that uses most of the quota
			premiumStorageClass := "premium"
			existingPVC := &corev1.PersistentVolumeClaim{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "existing-premium-pvc",
					Namespace: "test-namespace",
				},
				Spec: corev1.PersistentVolumeClaimSpec{
					StorageClassName: &premiumStorageClass,
					Resources: corev1.VolumeResourceRequirements{
						Requests: corev1.ResourceList{
							corev1.ResourceStorage: resource.MustParse("2Gi"),
						},
					},
				},
			}
			Expect(k8sClient.Create(ctx, existingPVC)).To(Succeed())

			// Try to create another PVC that would exceed the storage class quota
			newPVC := &corev1.PersistentVolumeClaim{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "new-premium-pvc",
					Namespace: "test-namespace",
				},
				Spec: corev1.PersistentVolumeClaimSpec{
					StorageClassName: &premiumStorageClass,
					Resources: corev1.VolumeResourceRequirements{
						Requests: corev1.ResourceList{
							corev1.ResourceStorage: resource.MustParse("2Gi"), // 2Gi + 2Gi > 3Gi limit
						},
					},
				},
			}

			warnings, err := validator.ValidateCreate(ctx, newPVC)
			Expect(err).To(HaveOccurred())
			Expect(err.Error()).To(ContainSubstring("ClusterResourceQuota"))
			Expect(err.Error()).To(ContainSubstring("storage class premium storage limit"))
			Expect(warnings).To(BeNil())
		})

		It("should block PVC creation when storage class specific PVC count quota would be exceeded", func() {
			// Create a ClusterResourceQuota with storage class specific PVC count limits
			storageClassCRQ := &quotav1alpha1.ClusterResourceQuota{
				ObjectMeta: metav1.ObjectMeta{
					Name: "storage-class-count-crq",
				},
				Spec: quotav1alpha1.ClusterResourceQuotaSpec{
					Hard: quotav1alpha1.ResourceList{
						corev1.ResourceName("gold.storageclass.storage.k8s.io/persistentvolumeclaims"): resource.MustParse("2"),
					},
					NamespaceSelector: &metav1.LabelSelector{
						MatchLabels: map[string]string{
							"quota-test": "pvc-webhook",
						},
					},
				},
			}
			Expect(k8sClient.Create(ctx, storageClassCRQ)).To(Succeed())

			// Create 2 PVCs with gold storage class to reach the limit
			goldStorageClass := "gold"
			for i := 0; i < 2; i++ {
				pvc := &corev1.PersistentVolumeClaim{
					ObjectMeta: metav1.ObjectMeta{
						Name:      fmt.Sprintf("existing-gold-pvc-%d", i),
						Namespace: "test-namespace",
					},
					Spec: corev1.PersistentVolumeClaimSpec{
						StorageClassName: &goldStorageClass,
						Resources: corev1.VolumeResourceRequirements{
							Requests: corev1.ResourceList{
								corev1.ResourceStorage: resource.MustParse("1Gi"),
							},
						},
					},
				}
				Expect(k8sClient.Create(ctx, pvc)).To(Succeed())
			}

			// Try to create another PVC with gold storage class
			newPVC := &corev1.PersistentVolumeClaim{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "new-gold-pvc",
					Namespace: "test-namespace",
				},
				Spec: corev1.PersistentVolumeClaimSpec{
					StorageClassName: &goldStorageClass,
					Resources: corev1.VolumeResourceRequirements{
						Requests: corev1.ResourceList{
							corev1.ResourceStorage: resource.MustParse("1Gi"),
						},
					},
				},
			}

			warnings, err := validator.ValidateCreate(ctx, newPVC)
			Expect(err).To(HaveOccurred())
			Expect(err.Error()).To(ContainSubstring("ClusterResourceQuota"))
			Expect(err.Error()).To(ContainSubstring("storage class gold PVC count limit"))
			Expect(warnings).To(BeNil())
		})

		It("should allow PVC creation with different storage class when storage class specific quota exists", func() {
			// Create a ClusterResourceQuota with storage class specific limits for fast-ssd only
			storageClassCRQ := &quotav1alpha1.ClusterResourceQuota{
				ObjectMeta: metav1.ObjectMeta{
					Name: "fast-ssd-only-crq",
				},
				Spec: quotav1alpha1.ClusterResourceQuotaSpec{
					Hard: quotav1alpha1.ResourceList{
						// No general storage limit, only storage class specific
						corev1.ResourceName("fast-ssd.storageclass.storage.k8s.io/requests.storage"): resource.MustParse("1Gi"), // Very small limit for fast-ssd
					},
					NamespaceSelector: &metav1.LabelSelector{
						MatchLabels: map[string]string{
							"quota-test": "different-storage-class", // Different selector to avoid conflict with existing CRQ
						},
					},
				},
			}
			Expect(k8sClient.Create(ctx, storageClassCRQ)).To(Succeed())

			// Create namespace with different label
			differentNS := &corev1.Namespace{
				ObjectMeta: metav1.ObjectMeta{
					Name: "different-storage-namespace",
					Labels: map[string]string{
						"quota-test": "different-storage-class",
					},
				},
			}
			Expect(k8sClient.Create(ctx, differentNS)).To(Succeed())

			// Create PVC with different storage class (slow-hdd) - should be allowed
			slowHDDStorageClass := "slow-hdd"
			pvc := &corev1.PersistentVolumeClaim{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "slow-hdd-pvc",
					Namespace: "different-storage-namespace", // Use different namespace
				},
				Spec: corev1.PersistentVolumeClaimSpec{
					StorageClassName: &slowHDDStorageClass,
					Resources: corev1.VolumeResourceRequirements{
						Requests: corev1.ResourceList{
							corev1.ResourceStorage: resource.MustParse("5Gi"), // Reasonable size
						},
					},
				},
			}

			warnings, err := validator.ValidateCreate(ctx, pvc)
			Expect(err).NotTo(HaveOccurred())
			Expect(warnings).To(BeEmpty())
		})

		It("should reject non-PVC objects", func() {
			pod := &corev1.Pod{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "test-pod",
					Namespace: "test-namespace",
				},
			}

			warnings, err := validator.ValidateCreate(ctx, pod)
			Expect(err).To(HaveOccurred())
			Expect(err.Error()).To(ContainSubstring("expected a PersistentVolumeClaim object"))
			Expect(warnings).To(BeNil())
		})
	})

	Describe("ValidateUpdate", func() {
		It("should validate storage quota on PVC updates", func() {
			oldPVC := &corev1.PersistentVolumeClaim{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "test-pvc",
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

			// Updated PVC with larger storage request
			newPVC := oldPVC.DeepCopy()
			newPVC.Spec.Resources.Requests[corev1.ResourceStorage] = resource.MustParse("6Gi") // Exceeds 5Gi limit

			warnings, err := validator.ValidateUpdate(ctx, oldPVC, newPVC)
			Expect(err).To(HaveOccurred())
			Expect(err.Error()).To(ContainSubstring("ClusterResourceQuota"))
			Expect(warnings).To(BeNil())
		})
	})

	Describe("ValidateDelete", func() {
		It("should allow PVC deletion", func() {
			pvc := &corev1.PersistentVolumeClaim{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "test-pvc",
					Namespace: "test-namespace",
				},
			}

			warnings, err := validator.ValidateDelete(ctx, pvc)
			Expect(err).NotTo(HaveOccurred())
			Expect(warnings).To(BeEmpty())
		})
	})

	Describe("Default", func() {
		It("should not modify PVC objects", func() {
			pvc := &corev1.PersistentVolumeClaim{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "test-pvc",
					Namespace: "test-namespace",
				},
			}

			originalPVC := pvc.DeepCopy()
			err := defaulter.Default(ctx, pvc)
			Expect(err).NotTo(HaveOccurred())
			Expect(pvc).To(Equal(originalPVC))
		})
	})

	Describe("Helper Functions", func() {
		It("getStorageRequest should extract storage request correctly", func() {
			pvc := &corev1.PersistentVolumeClaim{
				Spec: corev1.PersistentVolumeClaimSpec{
					Resources: corev1.VolumeResourceRequirements{
						Requests: corev1.ResourceList{
							corev1.ResourceStorage: resource.MustParse("2Gi"),
						},
					},
				},
			}

			storageRequest := getStorageRequest(pvc)
			expected := resource.MustParse("2Gi")
			Expect(storageRequest.Equal(expected)).To(BeTrue())
		})

		It("getStorageRequest should return zero for PVC without storage request", func() {
			pvc := &corev1.PersistentVolumeClaim{
				Spec: corev1.PersistentVolumeClaimSpec{
					// No Resources specified
				},
			}

			storageRequest := getStorageRequest(pvc)
			Expect(storageRequest.IsZero()).To(BeTrue())
		})

		It("getPVCStorageClass should extract storage class correctly", func() {
			storageClass := "premium-ssd"
			pvc := &corev1.PersistentVolumeClaim{
				Spec: corev1.PersistentVolumeClaimSpec{
					StorageClassName: &storageClass,
				},
			}

			extractedClass := getPVCStorageClass(pvc)
			Expect(extractedClass).To(Equal("premium-ssd"))
		})

		It("getPVCStorageClass should return empty string for PVC without storage class", func() {
			pvc := &corev1.PersistentVolumeClaim{
				Spec: corev1.PersistentVolumeClaimSpec{
					// No StorageClassName specified
				},
			}

			extractedClass := getPVCStorageClass(pvc)
			Expect(extractedClass).To(Equal(""))
		})
	})
})
