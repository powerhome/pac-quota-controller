package v1alpha1

import (
	"bytes"
	"encoding/json"
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
	"k8s.io/client-go/kubernetes/fake"
	"sigs.k8s.io/controller-runtime/pkg/client"
	ctrlclientfake "sigs.k8s.io/controller-runtime/pkg/client/fake"

	quotav1alpha1 "github.com/powerhome/pac-quota-controller/api/v1alpha1"
	"github.com/powerhome/pac-quota-controller/pkg/kubernetes/quota"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
)

var _ = Describe("PersistentVolumeClaimWebhook", func() {
	var (
		ginEngine         *gin.Engine
		webhook           *PersistentVolumeClaimWebhook
		fakeRuntimeClient client.Client
		crqClient         *quota.CRQClient
		testNamespace     *corev1.Namespace
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
		k8sClient := fake.NewSimpleClientset(testNamespace)
		scheme := runtime.NewScheme()
		_ = quotav1alpha1.AddToScheme(scheme)
		fakeRuntimeClient = ctrlclientfake.NewClientBuilder().WithScheme(scheme).Build()
		crqClient = quota.NewCRQClient(fakeRuntimeClient)
		webhook = NewPersistentVolumeClaimWebhook(k8sClient, zap.NewNop())
		webhook.SetCRQClient(crqClient)

		// Setup route
		ginEngine.POST("/pvc", webhook.Handle)
	})

	Describe("NewPersistentVolumeClaimWebhook", func() {
		It("should create a new webhook instance", func() {
			k8sClient := fake.NewSimpleClientset()
			logger := zap.NewNop()
			webhook := NewPersistentVolumeClaimWebhook(k8sClient, logger)

			Expect(webhook).NotTo(BeNil())
			Expect(webhook.client).To(Equal(k8sClient))
			Expect(webhook.log).To(Equal(logger))
			Expect(webhook.storageCalculator).NotTo(BeNil())
		})
	})

	Describe("Handle", func() {
		It("should handle PVC creation successfully", func() {
			pvc := &corev1.PersistentVolumeClaim{
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

			admissionReview := createPVCAdmissionReview(pvc, admissionv1.Create)
			response := sendPVCWebhookRequest(ginEngine, admissionReview)

			Expect(response.Allowed).To(BeTrue())
		})

		It("should handle PVC update successfully", func() {
			pvc := &corev1.PersistentVolumeClaim{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "test-pvc",
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

			admissionReview := createPVCAdmissionReview(pvc, admissionv1.Update)
			response := sendPVCWebhookRequest(ginEngine, admissionReview)

			Expect(response.Allowed).To(BeTrue())
		})

		It("should handle PVC with no storage request", func() {
			pvc := &corev1.PersistentVolumeClaim{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "test-pvc",
					Namespace: "test-namespace",
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
					Namespace: "test-namespace",
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
					Namespace: "test-namespace",
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

			Expect(w.Code).To(Equal(http.StatusOK))

			var response admissionv1.AdmissionReview
			err = json.Unmarshal(w.Body.Bytes(), &response)
			Expect(err).NotTo(HaveOccurred())

			Expect(response.Response.Allowed).To(BeFalse())
			Expect(response.Response.Result.Message).To(ContainSubstring("Missing admission request"))
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

			err := webhook.validateCreate(pvc)
			Expect(err).NotTo(HaveOccurred())
		})
	})

	Describe("validateUpdate", func() {
		It("should validate PVC update successfully", func() {
			pvc := &corev1.PersistentVolumeClaim{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "test-pvc",
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

			err := webhook.validateUpdate(pvc)
			Expect(err).NotTo(HaveOccurred())
		})
	})

	Describe("validateStorageQuota", func() {
		It("should validate storage quota successfully", func() {
			pvc := &corev1.PersistentVolumeClaim{
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

			err := webhook.validateStorageQuota(pvc)
			Expect(err).NotTo(HaveOccurred())
		})

		It("should handle PVC with no storage request", func() {
			pvc := &corev1.PersistentVolumeClaim{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "test-pvc",
					Namespace: "test-namespace",
				},
				Spec: corev1.PersistentVolumeClaimSpec{
					Resources: corev1.VolumeResourceRequirements{
						Requests: corev1.ResourceList{},
					},
				},
			}

			err := webhook.validateStorageQuota(pvc)
			Expect(err).NotTo(HaveOccurred())
		})
	})

	Describe("validateResourceQuota", func() {
		It("should skip CRQ validation for now", func() {
			err := webhook.validateResourceQuota("test-namespace", corev1.ResourceStorage, resource.MustParse("1Gi"))
			Expect(err).NotTo(HaveOccurred())
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
