package v1alpha1

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"

	"github.com/gin-gonic/gin"
	"go.uber.org/zap"
	admissionv1 "k8s.io/api/admission/v1"
	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/resource"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/runtime/serializer"
	"k8s.io/client-go/kubernetes"
	"k8s.io/client-go/kubernetes/fake"
	"sigs.k8s.io/controller-runtime/pkg/client"
	ctrlclientfake "sigs.k8s.io/controller-runtime/pkg/client/fake"

	quotav1alpha1 "github.com/powerhome/pac-quota-controller/api/v1alpha1"
	"github.com/powerhome/pac-quota-controller/pkg/kubernetes/quota"
	"github.com/powerhome/pac-quota-controller/pkg/kubernetes/usage"
	pkglogger "github.com/powerhome/pac-quota-controller/pkg/logger"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
)

const (
	premiumSSDStorageClass          = "premium-ssd"
	fastSSDStorageClassResourceName = "fast-ssd.storageclass.storage.k8s.io/persistentvolumeclaims"
)

var _ = Describe("PersistentVolumeClaimWebhook", func() {
	var (
		ctx               context.Context
		ginEngine         *gin.Engine
		webhook           *PersistentVolumeClaimWebhook
		fakeRuntimeClient client.Client
		k8sClient         kubernetes.Interface
		crqClient         *quota.CRQClient
		testNamespace     *corev1.Namespace
		logger            *zap.Logger
	)

	BeforeEach(func() {
		gin.SetMode(gin.TestMode)
		ginEngine = gin.New()

		// Create test namespace that will be used in tests
		testNamespace = &corev1.Namespace{
			ObjectMeta: metav1.ObjectMeta{
				Name: "test-namespace",
			},
		}

		// Create webhook with fake client
		k8sClient = fake.NewSimpleClientset(testNamespace)
		scheme := runtime.NewScheme()
		logger = pkglogger.L()
		_ = quotav1alpha1.AddToScheme(scheme)
		_ = corev1.AddToScheme(scheme)
		fakeRuntimeClient = ctrlclientfake.NewClientBuilder().WithScheme(scheme).Build()
		crqClient = quota.NewCRQClient(fakeRuntimeClient, logger)
		webhook = NewPersistentVolumeClaimWebhook(k8sClient, crqClient, logger)

		// Setup route
		ginEngine.POST("/pvc", webhook.Handle)
	})

	Describe("NewPersistentVolumeClaimWebhook", func() {
		It("should create a new webhook instance", func() {
			webhook := NewPersistentVolumeClaimWebhook(k8sClient, crqClient, logger)

			Expect(webhook).NotTo(BeNil())
			Expect(webhook.client).To(Equal(k8sClient))
			Expect(webhook.logger).To(Equal(logger))
			Expect(webhook.storageCalculator).NotTo(BeNil())
		})

		It("should create webhook with nil client", func() {
			webhook := NewPersistentVolumeClaimWebhook(nil, crqClient, logger)

			Expect(webhook).NotTo(BeNil())
			Expect(webhook.client).To(BeNil())
			Expect(webhook.logger).To(Equal(logger))
		})

		It("should create webhook with nil logger", func() {
			webhook := NewPersistentVolumeClaimWebhook(k8sClient, crqClient, nil)

			Expect(webhook).NotTo(BeNil())
			Expect(webhook.client).To(Equal(k8sClient))
			Expect(webhook.logger).NotTo(BeNil())
		})

		It("should create webhook with nil CRQ client", func() {
			webhook := NewPersistentVolumeClaimWebhook(k8sClient, nil, logger)

			Expect(webhook).NotTo(BeNil())
			Expect(webhook.client).To(Equal(k8sClient))
			Expect(webhook.crqClient).To(BeNil())
		})
	})

	Describe("Handle", func() {
		It("should handle PVC creation successfully", func() {
			pvc := &corev1.PersistentVolumeClaim{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "test-pvc",
					Namespace: testNamespace.Name,
				},
				Spec: corev1.PersistentVolumeClaimSpec{
					Resources: corev1.VolumeResourceRequirements{
						Requests: corev1.ResourceList{
							corev1.ResourceStorage: resource.MustParse("1Gi"),
						},
					},
				},
			}

			admissionReview := createPVCAdmissionReview(pvc, admissionv1.Create)
			response := sendPVCWebhookRequest(ginEngine, admissionReview)

			Expect(response.Allowed).To(BeTrue())
		})

		It("should handle PVC update successfully", func() {
			pvc := &corev1.PersistentVolumeClaim{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "test-pvc",
					Namespace: testNamespace.Name,
				},
				Spec: corev1.PersistentVolumeClaimSpec{
					Resources: corev1.VolumeResourceRequirements{
						Requests: corev1.ResourceList{
							corev1.ResourceStorage: resource.MustParse("2Gi"),
						},
					},
				},
			}

			admissionReview := createPVCAdmissionReview(pvc, admissionv1.Update)
			response := sendPVCWebhookRequest(ginEngine, admissionReview)

			Expect(response.Allowed).To(BeTrue())
		})

		It("should handle PVC with no storage request", func() {
			pvc := &corev1.PersistentVolumeClaim{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "test-pvc",
					Namespace: testNamespace.Name,
				},
				Spec: corev1.PersistentVolumeClaimSpec{
					Resources: corev1.VolumeResourceRequirements{
						Requests: corev1.ResourceList{},
					},
				},
			}

			admissionReview := createPVCAdmissionReview(pvc, admissionv1.Create)
			response := sendPVCWebhookRequest(ginEngine, admissionReview)

			Expect(response.Allowed).To(BeTrue())
		})

		It("should reject request with wrong resource kind", func() {
			pvc := &corev1.PersistentVolumeClaim{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "test-pvc",
					Namespace: testNamespace.Name,
				},
			}

			admissionReview := createPVCAdmissionReview(pvc, admissionv1.Create)
			admissionReview.Request.Kind = metav1.GroupVersionKind{
				Group:   "apps",
				Version: "v1",
				Kind:    "Deployment",
			}

			response := sendPVCWebhookRequest(ginEngine, admissionReview)

			Expect(response.Allowed).To(BeFalse())
			Expect(response.Result.Message).To(ContainSubstring("Expected PersistentVolumeClaim resource"))
		})

		It("should handle unsupported operations", func() {
			pvc := &corev1.PersistentVolumeClaim{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "test-pvc",
					Namespace: testNamespace.Name,
				},
			}

			admissionReview := createPVCAdmissionReview(pvc, admissionv1.Delete)
			response := sendPVCWebhookRequest(ginEngine, admissionReview)

			Expect(response.Allowed).To(BeFalse())
			Expect(response.Result.Message).To(ContainSubstring("Operation DELETE is not supported"))
		})

		It("should handle nil admission review request", func() {
			// Create a request with nil Request field
			admissionReview := admissionv1.AdmissionReview{
				Request: nil,
				Response: &admissionv1.AdmissionResponse{
					UID: "test-uid",
				},
			}

			// Send the request directly to avoid the helper function
			body, err := json.Marshal(admissionReview)
			Expect(err).NotTo(HaveOccurred())

			req, _ := http.NewRequest("POST", "/pvc", bytes.NewBuffer(body))
			req.Header.Set("Content-Type", "application/json")
			w := httptest.NewRecorder()

			ginEngine.ServeHTTP(w, req)

			Expect(w.Code).To(Equal(http.StatusBadRequest))
		})

		It("should handle invalid JSON", func() {
			req, _ := http.NewRequest("POST", "/pvc", bytes.NewBufferString("invalid json"))
			req.Header.Set("Content-Type", "application/json")
			w := httptest.NewRecorder()

			ginEngine.ServeHTTP(w, req)

			Expect(w.Code).To(Equal(http.StatusBadRequest))
		})
	})

	Describe("validateCreate", func() {
		It("should validate PVC creation successfully", func() {
			pvc := &corev1.PersistentVolumeClaim{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "test-pvc",
					Namespace: testNamespace.Name,
				},
				Spec: corev1.PersistentVolumeClaimSpec{
					Resources: corev1.VolumeResourceRequirements{
						Requests: corev1.ResourceList{
							corev1.ResourceStorage: resource.MustParse("1Gi"),
						},
					},
				},
			}

			err := webhook.validateCreate(ctx, pvc)
			Expect(err).NotTo(HaveOccurred())
		})
	})

	Describe("validateUpdate", func() {
		It("should validate PVC update successfully", func() {
			pvc := &corev1.PersistentVolumeClaim{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "test-pvc",
					Namespace: testNamespace.Name,
				},
				Spec: corev1.PersistentVolumeClaimSpec{
					Resources: corev1.VolumeResourceRequirements{
						Requests: corev1.ResourceList{
							corev1.ResourceStorage: resource.MustParse("2Gi"),
						},
					},
				},
			}

			err := webhook.validateUpdate(ctx, pvc)
			Expect(err).NotTo(HaveOccurred())
		})
	})

	Describe("validateStorageQuota", func() {
		It("should validate storage quota successfully", func() {
			pvc := &corev1.PersistentVolumeClaim{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "test-pvc",
					Namespace: testNamespace.Name,
				},
				Spec: corev1.PersistentVolumeClaimSpec{
					Resources: corev1.VolumeResourceRequirements{
						Requests: corev1.ResourceList{
							corev1.ResourceStorage: resource.MustParse("1Gi"),
						},
					},
				},
			}

			err := webhook.validateStorageQuota(ctx, pvc)
			Expect(err).NotTo(HaveOccurred())
		})

		It("should handle PVC with no storage request", func() {
			pvc := &corev1.PersistentVolumeClaim{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "test-pvc",
					Namespace: testNamespace.Name,
				},
				Spec: corev1.PersistentVolumeClaimSpec{
					Resources: corev1.VolumeResourceRequirements{
						Requests: corev1.ResourceList{},
					},
				},
			}

			err := webhook.validateStorageQuota(ctx, pvc)
			Expect(err).NotTo(HaveOccurred())
		})
	})

	Describe("validateResourceQuota", func() {
		It("should validate storage quota successfully when within limits", func() {
			err := webhook.validateResourceQuota(ctx, testNamespace.Name, corev1.ResourceStorage, resource.MustParse("1Gi"))
			Expect(err).NotTo(HaveOccurred())
		})

		It("should handle namespace not found", func() {
			err := webhook.validateResourceQuota(ctx, "nonexistent-namespace", corev1.ResourceStorage, resource.MustParse("1Gi"))
			Expect(err).To(HaveOccurred())
			Expect(err.Error()).To(ContainSubstring("namespaces \"nonexistent-namespace\" not found"))
		})
	})

	Describe("Cross-Namespace Storage Validation", func() {
		var (
			crq         *quotav1alpha1.ClusterResourceQuota
			namespace1  *corev1.Namespace
			namespace2  *corev1.Namespace
			existingPVC *corev1.PersistentVolumeClaim
		)

		BeforeEach(func() {
			// Create test namespaces with matching labels
			namespace1 = &corev1.Namespace{
				ObjectMeta: metav1.ObjectMeta{
					Name: "storage-ns-1",
					Labels: map[string]string{
						"storage-test": "enabled",
					},
				},
			}
			namespace2 = &corev1.Namespace{
				ObjectMeta: metav1.ObjectMeta{
					Name: "storage-ns-2",
					Labels: map[string]string{
						"storage-test": "enabled",
					},
				},
			}

			// Create a ClusterResourceQuota for storage
			crq = &quotav1alpha1.ClusterResourceQuota{
				ObjectMeta: metav1.ObjectMeta{
					Name: "storage-crq",
				},
				Spec: quotav1alpha1.ClusterResourceQuotaSpec{
					NamespaceSelector: &metav1.LabelSelector{
						MatchLabels: map[string]string{
							"storage-test": "enabled",
						},
					},
					Hard: quotav1alpha1.ResourceList{
						corev1.ResourceRequestsStorage:        resource.MustParse("10Gi"),
						corev1.ResourcePersistentVolumeClaims: resource.MustParse("5"),
					},
				},
			}

			// Create an existing PVC in namespace1
			existingPVC = &corev1.PersistentVolumeClaim{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "existing-pvc",
					Namespace: "storage-ns-1",
				},
				Spec: corev1.PersistentVolumeClaimSpec{
					Resources: corev1.VolumeResourceRequirements{
						Requests: corev1.ResourceList{
							corev1.ResourceStorage: resource.MustParse("8Gi"),
						},
					},
				},
			}

			// Update clients with cross-namespace resources
			k8sClient := fake.NewSimpleClientset(testNamespace, namespace1, namespace2, existingPVC)
			scheme := runtime.NewScheme()
			_ = quotav1alpha1.AddToScheme(scheme)
			_ = corev1.AddToScheme(scheme)
			fakeRuntimeClient = ctrlclientfake.NewClientBuilder().
				WithScheme(scheme).
				WithObjects(crq, namespace1, namespace2).
				Build()
			crqClient = quota.NewCRQClient(fakeRuntimeClient, logger)
			webhook = NewPersistentVolumeClaimWebhook(k8sClient, crqClient, logger)

			// Re-setup gin engine
			ginEngine = gin.New()
			ginEngine.POST("/pvc", webhook.Handle)
		})

		AfterEach(func() {
			// Clean up cross-namespace test resources
			crq = nil
			namespace1 = nil
			namespace2 = nil
			existingPVC = nil
		})

		It("should reject PVC that would exceed cross-namespace storage quota", func() {
			// Try to create a PVC in namespace2 that would exceed the total storage quota
			newPVC := &corev1.PersistentVolumeClaim{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "new-pvc",
					Namespace: "storage-ns-2",
				},
				Spec: corev1.PersistentVolumeClaimSpec{
					Resources: corev1.VolumeResourceRequirements{
						Requests: corev1.ResourceList{
							corev1.ResourceStorage: resource.MustParse("5Gi"), // 8Gi + 5Gi = 13Gi > 10Gi limit
						},
					},
				},
			}

			admissionReview := createPVCAdmissionReview(newPVC, admissionv1.Create)
			response := sendPVCWebhookRequest(ginEngine, admissionReview)

			Expect(response.Allowed).To(BeFalse())
			Expect(response.Result.Message).To(ContainSubstring("storage-crq"))
			Expect(response.Result.Message).To(ContainSubstring("limit exceeded"))
		})

		It("should allow PVC within cross-namespace storage quota", func() {
			// Try to create a PVC in namespace2 that fits within the quota
			newPVC := &corev1.PersistentVolumeClaim{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "small-pvc",
					Namespace: "storage-ns-2",
				},
				Spec: corev1.PersistentVolumeClaimSpec{
					Resources: corev1.VolumeResourceRequirements{
						Requests: corev1.ResourceList{
							corev1.ResourceStorage: resource.MustParse("1Gi"), // 8Gi + 1Gi = 9Gi < 10Gi limit
						},
					},
				},
			}

			admissionReview := createPVCAdmissionReview(newPVC, admissionv1.Create)
			response := sendPVCWebhookRequest(ginEngine, admissionReview)

			Expect(response.Allowed).To(BeTrue())
		})

		It("should allow PVC in namespace not matching CRQ selector", func() {
			// Create a namespace that doesn't match the CRQ selector
			unmatchedNamespace := &corev1.Namespace{
				ObjectMeta: metav1.ObjectMeta{
					Name: "unmatched-storage-ns",
					Labels: map[string]string{
						"storage-test": "disabled",
					},
				},
			}

			// Update client
			k8sClient := fake.NewSimpleClientset(testNamespace, namespace1, namespace2, existingPVC, unmatchedNamespace)
			webhook = NewPersistentVolumeClaimWebhook(k8sClient, crqClient, logger)
			ginEngine = gin.New()
			ginEngine.POST("/pvc", webhook.Handle)

			// Try to create a large PVC in the unmatched namespace
			newPVC := &corev1.PersistentVolumeClaim{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "unmatched-pvc",
					Namespace: "unmatched-storage-ns",
				},
				Spec: corev1.PersistentVolumeClaimSpec{
					Resources: corev1.VolumeResourceRequirements{
						Requests: corev1.ResourceList{
							corev1.ResourceStorage: resource.MustParse("100Gi"), // Large request, but no CRQ applies
						},
					},
				},
			}

			admissionReview := createPVCAdmissionReview(newPVC, admissionv1.Create)
			response := sendPVCWebhookRequest(ginEngine, admissionReview)

			Expect(response.Allowed).To(BeTrue())
		})
	})

	Describe("getStorageRequest", func() {
		It("should extract storage request from PVC", func() {
			pvc := &corev1.PersistentVolumeClaim{
				Spec: corev1.PersistentVolumeClaimSpec{
					Resources: corev1.VolumeResourceRequirements{
						Requests: corev1.ResourceList{
							corev1.ResourceStorage: resource.MustParse("1Gi"),
						},
					},
				},
			}

			storageRequest := getStorageRequest(pvc)
			Expect(storageRequest).To(Equal(resource.MustParse("1Gi")))
		})

		It("should return empty quantity when no storage request", func() {
			pvc := &corev1.PersistentVolumeClaim{
				Spec: corev1.PersistentVolumeClaimSpec{
					Resources: corev1.VolumeResourceRequirements{
						Requests: corev1.ResourceList{},
					},
				},
			}

			storageRequest := getStorageRequest(pvc)
			Expect(storageRequest).To(Equal(resource.Quantity{}))
		})

		It("should return empty quantity when no requests", func() {
			pvc := &corev1.PersistentVolumeClaim{
				Spec: corev1.PersistentVolumeClaimSpec{
					Resources: corev1.VolumeResourceRequirements{},
				},
			}

			storageRequest := getStorageRequest(pvc)
			Expect(storageRequest).To(Equal(resource.Quantity{}))
		})

		Describe("calculateCurrentUsage function coverage", func() {
			It("should handle storage requests correctly", func() {
				// Create a PVC first
				pvc := &corev1.PersistentVolumeClaim{
					ObjectMeta: metav1.ObjectMeta{
						Name:      "test-pvc",
						Namespace: testNamespace.Name,
					},
					Spec: corev1.PersistentVolumeClaimSpec{
						Resources: corev1.VolumeResourceRequirements{
							Requests: corev1.ResourceList{
								corev1.ResourceStorage: resource.MustParse("10Gi"),
							},
						},
					},
				}
				_, err := k8sClient.CoreV1().PersistentVolumeClaims(testNamespace.Name).Create(ctx, pvc, metav1.CreateOptions{})
				Expect(err).NotTo(HaveOccurred())

				usage, err := webhook.calculateCurrentUsage(ctx, testNamespace.Name, usage.ResourceRequestsStorage)
				Expect(err).NotTo(HaveOccurred())
				Expect(usage.Value()).To(Equal(int64(10 * 1024 * 1024 * 1024))) // 10Gi in bytes
			})

			It("should handle storage with specific storage class", func() {
				storageClass := premiumSSDStorageClass
				// Create a PVC with specific storage class
				pvc := &corev1.PersistentVolumeClaim{
					ObjectMeta: metav1.ObjectMeta{
						Name:      "test-pvc-premium",
						Namespace: testNamespace.Name,
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
				_, err := k8sClient.CoreV1().PersistentVolumeClaims(testNamespace.Name).Create(ctx, pvc, metav1.CreateOptions{})
				Expect(err).NotTo(HaveOccurred())

				usage, err := webhook.calculateCurrentUsage(ctx, testNamespace.Name,
					corev1.ResourceName("premium-ssd.storageclass.storage.k8s.io/requests.storage"))
				Expect(err).NotTo(HaveOccurred())
				Expect(usage.Value()).To(Equal(int64(20 * 1024 * 1024 * 1024))) // 20Gi in bytes
			})

			It("should return error for unsupported resource types", func() {
				_, err := webhook.calculateCurrentUsage(ctx, testNamespace.Name, corev1.ResourceName("unsupported.resource"))
				Expect(err).To(HaveOccurred())
				Expect(err.Error()).To(ContainSubstring("unsupported resource type"))
			})

			It("should handle non-existent namespace", func() {
				usage, err := webhook.calculateCurrentUsage(ctx, "non-existent-namespace", usage.ResourceRequestsStorage)
				Expect(err).NotTo(HaveOccurred())
				Expect(usage.IsZero()).To(BeTrue())
			})

			It("should handle PVC count calculation", func() {
				// Create multiple PVCs to test count
				for i := 0; i < 3; i++ {
					pvc := &corev1.PersistentVolumeClaim{
						ObjectMeta: metav1.ObjectMeta{
							Name:      fmt.Sprintf("count-pvc-%d", i),
							Namespace: testNamespace.Name,
						},
						Spec: corev1.PersistentVolumeClaimSpec{
							Resources: corev1.VolumeResourceRequirements{
								Requests: corev1.ResourceList{
									corev1.ResourceStorage: resource.MustParse("5Gi"),
								},
							},
						},
					}
					_, err := k8sClient.CoreV1().PersistentVolumeClaims(testNamespace.Name).Create(ctx, pvc, metav1.CreateOptions{})
					Expect(err).NotTo(HaveOccurred())
				}

				// Test PVC count
				usage, err := webhook.calculateCurrentUsage(ctx, testNamespace.Name, corev1.ResourceName("persistentvolumeclaims"))
				Expect(err).NotTo(HaveOccurred())
				Expect(usage.Value()).To(BeNumerically("==", 3)) // Should count the PVCs
			})

			It("should handle storage class specific PVC count", func() {
				storageClass := "fast-ssd"
				// Create PVCs with specific storage class
				for i := 0; i < 2; i++ {
					pvc := &corev1.PersistentVolumeClaim{
						ObjectMeta: metav1.ObjectMeta{
							Name:      fmt.Sprintf("fast-pvc-%d", i),
							Namespace: testNamespace.Name,
						},
						Spec: corev1.PersistentVolumeClaimSpec{
							StorageClassName: &storageClass,
							Resources: corev1.VolumeResourceRequirements{
								Requests: corev1.ResourceList{
									corev1.ResourceStorage: resource.MustParse("15Gi"),
								},
							},
						},
					}
					_, err := k8sClient.CoreV1().PersistentVolumeClaims(testNamespace.Name).Create(ctx, pvc, metav1.CreateOptions{})
					Expect(err).NotTo(HaveOccurred())
				}

				// Test storage class specific PVC count
				usage, err := webhook.calculateCurrentUsage(
					ctx,
					testNamespace.Name,
					corev1.ResourceName(fastSSDStorageClassResourceName),
				)
				Expect(err).NotTo(HaveOccurred())
				Expect(usage.Value()).To(BeNumerically("==", 2)) // Should count the PVCs with specific storage class
			})

			Describe("validateStorageQuota edge cases", func() {
				var namespace *corev1.Namespace
				var crq *quotav1alpha1.ClusterResourceQuota
				var ctx context.Context

				BeforeEach(func() {
					namespace = &corev1.Namespace{
						ObjectMeta: metav1.ObjectMeta{
							Name: "storage-test-ns",
							Labels: map[string]string{
								"test-label": "storage-validation",
							},
						},
					}
					Expect(fakeRuntimeClient.Create(ctx, namespace)).To(Succeed())
					// Also create in k8sClient for storage calculator
					_, err := k8sClient.CoreV1().Namespaces().Create(ctx, namespace, metav1.CreateOptions{})
					Expect(err).NotTo(HaveOccurred())

					crq = &quotav1alpha1.ClusterResourceQuota{
						ObjectMeta: metav1.ObjectMeta{
							Name: "storage-test-crq",
						},
						Spec: quotav1alpha1.ClusterResourceQuotaSpec{
							NamespaceSelector: &metav1.LabelSelector{
								MatchLabels: map[string]string{
									"test-label": "storage-validation",
								},
							},
							Hard: quotav1alpha1.ResourceList{
								"requests.storage": resource.MustParse("100Gi"),
								"premium-ssd.storageclass.storage.k8s.io/requests.storage": resource.MustParse("50Gi"),
							},
						},
					}
					Expect(fakeRuntimeClient.Create(ctx, crq)).To(Succeed())
				})

				It("should validate storage class specific quotas", func() {
					storageClass := premiumSSDStorageClass
					pvc := &corev1.PersistentVolumeClaim{
						ObjectMeta: metav1.ObjectMeta{
							Name:      "premium-test-pvc",
							Namespace: "storage-test-ns",
						},
						Spec: corev1.PersistentVolumeClaimSpec{
							StorageClassName: &storageClass,
							Resources: corev1.VolumeResourceRequirements{
								Requests: corev1.ResourceList{
									corev1.ResourceStorage: resource.MustParse("60Gi"), // Exceeds storage class quota
								},
							},
						},
					}

					err := webhook.validateStorageQuota(ctx, pvc)
					Expect(err).To(HaveOccurred())
					Expect(err.Error()).To(ContainSubstring("premium-ssd.storageclass.storage.k8s.io/requests.storage"))
				})

				It("should validate general storage quota when storage class quota passes", func() {
					storageClass := premiumSSDStorageClass
					pvc := &corev1.PersistentVolumeClaim{
						ObjectMeta: metav1.ObjectMeta{
							Name:      "general-test-pvc",
							Namespace: "storage-test-ns",
						},
						Spec: corev1.PersistentVolumeClaimSpec{
							StorageClassName: &storageClass,
							Resources: corev1.VolumeResourceRequirements{
								Requests: corev1.ResourceList{
									corev1.ResourceStorage: resource.MustParse("30Gi"), // Within storage class quota but test general
								},
							},
						},
					}

					// Add existing PVCs to push general storage over limit
					for i := 0; i < 5; i++ {
						existingPVC := &corev1.PersistentVolumeClaim{
							ObjectMeta: metav1.ObjectMeta{
								Name:      fmt.Sprintf("existing-pvc-%d", i),
								Namespace: "storage-test-ns",
							},
							Spec: corev1.PersistentVolumeClaimSpec{
								Resources: corev1.VolumeResourceRequirements{
									Requests: corev1.ResourceList{
										corev1.ResourceStorage: resource.MustParse("20Gi"),
									},
								},
							},
						}
						_, err := k8sClient.CoreV1().PersistentVolumeClaims("storage-test-ns").Create(
							ctx, existingPVC, metav1.CreateOptions{})
						Expect(err).NotTo(HaveOccurred())
					}

					err := webhook.validateStorageQuota(ctx, pvc)
					Expect(err).To(HaveOccurred())
					Expect(err.Error()).To(ContainSubstring("requests.storage"))
				})

				It("should handle PVC without storage class", func() {
					pvc := &corev1.PersistentVolumeClaim{
						ObjectMeta: metav1.ObjectMeta{
							Name:      "no-storage-class-pvc",
							Namespace: "storage-test-ns",
						},
						Spec: corev1.PersistentVolumeClaimSpec{
							Resources: corev1.VolumeResourceRequirements{
								Requests: corev1.ResourceList{
									corev1.ResourceStorage: resource.MustParse("5Gi"),
								},
							},
						},
					}

					err := webhook.validateStorageQuota(ctx, pvc)
					Expect(err).NotTo(HaveOccurred()) // Should pass with general storage quota
				})

				It("should handle missing storage requests", func() {
					pvc := &corev1.PersistentVolumeClaim{
						ObjectMeta: metav1.ObjectMeta{
							Name:      "no-storage-requests-pvc",
							Namespace: "storage-test-ns",
						},
						Spec: corev1.PersistentVolumeClaimSpec{
							Resources: corev1.VolumeResourceRequirements{
								// No requests specified
							},
						},
					}

					err := webhook.validateStorageQuota(ctx, pvc)
					Expect(err).NotTo(HaveOccurred()) // Should pass when no storage requested
				})
			})

			Describe("validateUpdate edge cases", func() {
				var namespace *corev1.Namespace
				var crq *quotav1alpha1.ClusterResourceQuota
				var ctx context.Context

				BeforeEach(func() {
					namespace = &corev1.Namespace{
						ObjectMeta: metav1.ObjectMeta{
							Name: "update-test-ns",
							Labels: map[string]string{
								"test-label": "update-validation",
							},
						},
					}
					Expect(fakeRuntimeClient.Create(ctx, namespace)).To(Succeed())
					// Also create in k8sClient for storage calculator
					_, err := k8sClient.CoreV1().Namespaces().Create(ctx, namespace, metav1.CreateOptions{})
					Expect(err).NotTo(HaveOccurred())

					crq = &quotav1alpha1.ClusterResourceQuota{
						ObjectMeta: metav1.ObjectMeta{
							Name: "update-test-crq",
						},
						Spec: quotav1alpha1.ClusterResourceQuotaSpec{
							NamespaceSelector: &metav1.LabelSelector{
								MatchLabels: map[string]string{
									"test-label": "update-validation",
								},
							},
							Hard: quotav1alpha1.ResourceList{
								"requests.storage": resource.MustParse("50Gi"),
							},
						},
					}
					Expect(fakeRuntimeClient.Create(ctx, crq)).To(Succeed())
				})

				It("should allow updates that don't increase storage", func() {
					// Create original PVC
					pvc := &corev1.PersistentVolumeClaim{
						ObjectMeta: metav1.ObjectMeta{
							Name:      "update-pvc",
							Namespace: "update-test-ns",
						},
						Spec: corev1.PersistentVolumeClaimSpec{
							Resources: corev1.VolumeResourceRequirements{
								Requests: corev1.ResourceList{
									corev1.ResourceStorage: resource.MustParse("10Gi"),
								},
							},
						},
					}
					Expect(fakeRuntimeClient.Create(ctx, pvc)).To(Succeed())

					// Simulate update with same storage
					updatedPVC := pvc.DeepCopy()
					// Just change labels, not storage
					updatedPVC.Labels = map[string]string{"updated": "true"}

					err := webhook.validateUpdate(ctx, updatedPVC)
					Expect(err).NotTo(HaveOccurred())
				})

				It("should validate updates that increase storage", func() {
					// Create original PVC
					pvc := &corev1.PersistentVolumeClaim{
						ObjectMeta: metav1.ObjectMeta{
							Name:      "expand-pvc",
							Namespace: "update-test-ns",
						},
						Spec: corev1.PersistentVolumeClaimSpec{
							Resources: corev1.VolumeResourceRequirements{
								Requests: corev1.ResourceList{
									corev1.ResourceStorage: resource.MustParse("10Gi"),
								},
							},
						},
					}
					_, err := k8sClient.CoreV1().PersistentVolumeClaims("update-test-ns").Create(ctx, pvc, metav1.CreateOptions{})
					Expect(err).NotTo(HaveOccurred())

					// Fill up quota with another PVC
					otherPVC := &corev1.PersistentVolumeClaim{
						ObjectMeta: metav1.ObjectMeta{
							Name:      "other-pvc",
							Namespace: "update-test-ns",
						},
						Spec: corev1.PersistentVolumeClaimSpec{
							Resources: corev1.VolumeResourceRequirements{
								Requests: corev1.ResourceList{
									corev1.ResourceStorage: resource.MustParse("35Gi"), // Total would be 45Gi
								},
							},
						},
					}
					_, err = k8sClient.CoreV1().PersistentVolumeClaims("update-test-ns").Create(ctx, otherPVC, metav1.CreateOptions{})
					Expect(err).NotTo(HaveOccurred())

					// Try to expand beyond quota
					updatedPVC := pvc.DeepCopy()
					// Would make total 55Gi > 50Gi
					updatedPVC.Spec.Resources.Requests[corev1.ResourceStorage] = resource.MustParse("20Gi")

					err = webhook.validateUpdate(ctx, updatedPVC)
					Expect(err).To(HaveOccurred())
					Expect(err.Error()).To(ContainSubstring("requests.storage"))
				})

				It("should handle update with nil PVC", func() {
					newPVC := &corev1.PersistentVolumeClaim{
						ObjectMeta: metav1.ObjectMeta{
							Name:      "nil-test-pvc",
							Namespace: "update-test-ns",
						},
						Spec: corev1.PersistentVolumeClaimSpec{
							Resources: corev1.VolumeResourceRequirements{
								Requests: corev1.ResourceList{
									corev1.ResourceStorage: resource.MustParse("5Gi"),
								},
							},
						},
					}

					err := webhook.validateUpdate(ctx, newPVC)
					Expect(err).NotTo(HaveOccurred()) // Should validate as normal
				})
			})
		})
	})
})

// Helper functions
func createPVCAdmissionReview(pvc *corev1.PersistentVolumeClaim,
	operation admissionv1.Operation) admissionv1.AdmissionReview {
	// Encode the PVC to raw bytes
	scheme := runtime.NewScheme()
	err := corev1.AddToScheme(scheme)
	Expect(err).NotTo(HaveOccurred())
	codec := serializer.NewCodecFactory(scheme).LegacyCodec(corev1.SchemeGroupVersion)

	pvcBytes, err := runtime.Encode(codec, pvc)
	Expect(err).NotTo(HaveOccurred())

	return admissionv1.AdmissionReview{
		Request: &admissionv1.AdmissionRequest{
			UID: "test-uid",
			Kind: metav1.GroupVersionKind{
				Group:   "",
				Version: "v1",
				Kind:    "PersistentVolumeClaim",
			},
			Resource: metav1.GroupVersionResource{
				Group:    "",
				Version:  "v1",
				Resource: "persistentvolumeclaims",
			},
			Operation: operation,
			Namespace: pvc.Namespace,
			Object: runtime.RawExtension{
				Raw: pvcBytes,
			},
		},
		Response: &admissionv1.AdmissionResponse{
			UID: "test-uid",
		},
	}
}

func sendPVCWebhookRequest(ginEngine *gin.Engine,
	admissionReview admissionv1.AdmissionReview) *admissionv1.AdmissionResponse {
	body, err := json.Marshal(admissionReview)
	Expect(err).NotTo(HaveOccurred())

	req, _ := http.NewRequest("POST", "/pvc", bytes.NewBuffer(body))
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()

	ginEngine.ServeHTTP(w, req)

	Expect(w.Code).To(Equal(http.StatusOK))

	var response admissionv1.AdmissionReview
	err = json.Unmarshal(w.Body.Bytes(), &response)
	Expect(err).NotTo(HaveOccurred())

	return response.Response
}
