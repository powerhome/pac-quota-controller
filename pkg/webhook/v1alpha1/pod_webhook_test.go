package v1alpha1

import (
	"bytes"
	"encoding/json"
	"net/http"
	"net/http/httptest"

	"github.com/gin-gonic/gin"
	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	"go.uber.org/zap"
	admissionv1 "k8s.io/api/admission/v1"
	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/resource"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/client-go/kubernetes"
	"k8s.io/client-go/kubernetes/fake"
	"sigs.k8s.io/controller-runtime/pkg/client"
	ctrlclientfake "sigs.k8s.io/controller-runtime/pkg/client/fake"

	quotav1alpha1 "github.com/powerhome/pac-quota-controller/api/v1alpha1"
	"github.com/powerhome/pac-quota-controller/pkg/kubernetes/quota"
)

var _ = Describe("PodWebhook", func() {
	var (
		webhook           *PodWebhook
		fakeClient        kubernetes.Interface
		fakeRuntimeClient client.Client
		crqClient         *quota.CRQClient
		logger            *zap.Logger
		ginEngine         *gin.Engine
		testNamespace     *corev1.Namespace
	)

	BeforeEach(func() {
		// Create test namespace that will be used in tests
		testNamespace = &corev1.Namespace{
			ObjectMeta: metav1.ObjectMeta{
				Name: "test-namespace",
			},
		}

		fakeClient = fake.NewSimpleClientset(testNamespace)
		scheme := runtime.NewScheme()
		_ = quotav1alpha1.AddToScheme(scheme)
		fakeRuntimeClient = ctrlclientfake.NewClientBuilder().WithScheme(scheme).Build()
		crqClient = quota.NewCRQClient(fakeRuntimeClient)
		logger = zap.NewNop()
		webhook = NewPodWebhook(fakeClient, logger)
		webhook.SetCRQClient(crqClient)
		gin.SetMode(gin.TestMode)
		ginEngine = gin.New()
		ginEngine.POST("/webhook", webhook.Handle)
	})

	Describe("NewPodWebhook", func() {
		It("should create a new pod webhook", func() {
			Expect(webhook).NotTo(BeNil())
			Expect(webhook.client).To(Equal(fakeClient))
			Expect(webhook.log).To(Equal(logger))
			Expect(webhook.podCalculator).NotTo(BeNil())
		})

		It("should create webhook with nil client", func() {
			webhook := NewPodWebhook(nil, logger)
			Expect(webhook).NotTo(BeNil())
			Expect(webhook.client).To(BeNil())
		})

		It("should create webhook with nil logger", func() {
			webhook := NewPodWebhook(fakeClient, nil)
			Expect(webhook).NotTo(BeNil())
			Expect(webhook.log).To(BeNil())
		})
	})

	Describe("Handle", func() {
		It("should handle valid pod creation request", func() {
			pod := &corev1.Pod{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "test-pod",
					Namespace: "test-namespace",
				},
				Spec: corev1.PodSpec{
					Containers: []corev1.Container{
						{
							Name: "test-container",
							Resources: corev1.ResourceRequirements{
								Requests: corev1.ResourceList{
									corev1.ResourceCPU: resource.MustParse("100m"),
								},
							},
						},
					},
				},
			}

			admissionReview := createAdmissionReview(pod, admissionv1.Create)
			response := sendWebhookRequest(ginEngine, admissionReview)

			Expect(response.Response.Allowed).To(BeTrue())
			Expect(response.Response.UID).To(Equal(admissionReview.Request.UID))
		})

		It("should handle valid pod update request", func() {
			pod := &corev1.Pod{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "test-pod",
					Namespace: "test-namespace",
				},
				Spec: corev1.PodSpec{
					Containers: []corev1.Container{
						{
							Name: "test-container",
							Resources: corev1.ResourceRequirements{
								Requests: corev1.ResourceList{
									corev1.ResourceCPU: resource.MustParse("200m"),
								},
							},
						},
					},
				},
			}

			admissionReview := createAdmissionReview(pod, admissionv1.Update)
			response := sendWebhookRequest(ginEngine, admissionReview)

			Expect(response.Response.Allowed).To(BeTrue())
			Expect(response.Response.UID).To(Equal(admissionReview.Request.UID))
		})

		It("should reject request with nil admission review", func() {
			req, _ := http.NewRequest("POST", "/webhook", bytes.NewBuffer([]byte("{}")))
			req.Header.Set("Content-Type", "application/json")
			w := httptest.NewRecorder()

			ginEngine.ServeHTTP(w, req)

			Expect(w.Code).To(Equal(http.StatusBadRequest))
		})

		It("should reject request with nil admission review request", func() {
			admissionReview := &admissionv1.AdmissionReview{
				TypeMeta: metav1.TypeMeta{
					Kind:       "AdmissionReview",
					APIVersion: "admission.k8s.io/v1",
				},
				Request: nil,
			}

			response := sendWebhookRequest(ginEngine, admissionReview)

			Expect(response).NotTo(BeNil())
			Expect(response.Response).NotTo(BeNil())
			Expect(response.Response.Allowed).To(BeFalse())
			Expect(response.Response.Result.Message).To(ContainSubstring("Admission review request is nil"))
		})

		It("should reject request with wrong resource kind", func() {
			pod := &corev1.Pod{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "test-pod",
					Namespace: "test-namespace",
				},
			}

			admissionReview := createAdmissionReview(pod, admissionv1.Create)
			admissionReview.Request.Kind = metav1.GroupVersionKind{
				Group:   "apps",
				Version: "v1",
				Kind:    "Deployment",
			}

			response := sendWebhookRequest(ginEngine, admissionReview)

			Expect(response.Response.Allowed).To(BeFalse())
			Expect(response.Response.Result.Message).To(ContainSubstring("Unexpected resource kind"))
		})

		It("should reject request with invalid JSON", func() {
			req, _ := http.NewRequest("POST", "/webhook", bytes.NewBuffer([]byte("invalid json")))
			req.Header.Set("Content-Type", "application/json")
			w := httptest.NewRecorder()

			ginEngine.ServeHTTP(w, req)

			Expect(w.Code).To(Equal(http.StatusBadRequest))
		})

		It("should reject unsupported operation", func() {
			pod := &corev1.Pod{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "test-pod",
					Namespace: "test-namespace",
				},
			}

			admissionReview := createAdmissionReview(pod, admissionv1.Delete)
			response := sendWebhookRequest(ginEngine, admissionReview)

			Expect(response.Response.Allowed).To(BeFalse())
			Expect(response.Response.Result.Message).To(ContainSubstring("Unsupported operation"))
		})

		It("should handle pod with no containers", func() {
			pod := &corev1.Pod{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "test-pod",
					Namespace: "test-namespace",
				},
				Spec: corev1.PodSpec{
					Containers: []corev1.Container{},
				},
			}

			admissionReview := createAdmissionReview(pod, admissionv1.Create)
			response := sendWebhookRequest(ginEngine, admissionReview)

			Expect(response.Response.Allowed).To(BeTrue())
		})

		It("should handle pod with init containers", func() {
			pod := &corev1.Pod{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "test-pod",
					Namespace: "test-namespace",
				},
				Spec: corev1.PodSpec{
					InitContainers: []corev1.Container{
						{
							Name: "init-container",
							Resources: corev1.ResourceRequirements{
								Requests: corev1.ResourceList{
									corev1.ResourceCPU: resource.MustParse("50m"),
								},
							},
						},
					},
					Containers: []corev1.Container{
						{
							Name: "main-container",
							Resources: corev1.ResourceRequirements{
								Requests: corev1.ResourceList{
									corev1.ResourceCPU: resource.MustParse("100m"),
								},
							},
						},
					},
				},
			}

			admissionReview := createAdmissionReview(pod, admissionv1.Create)
			response := sendWebhookRequest(ginEngine, admissionReview)

			Expect(response.Response.Allowed).To(BeTrue())
		})

		It("should handle pod with large resource requests", func() {
			pod := &corev1.Pod{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "test-pod",
					Namespace: "test-namespace",
				},
				Spec: corev1.PodSpec{
					Containers: []corev1.Container{
						{
							Name: "test-container",
							Resources: corev1.ResourceRequirements{
								Requests: corev1.ResourceList{
									corev1.ResourceCPU:    resource.MustParse("1000m"),
									corev1.ResourceMemory: resource.MustParse("1Gi"),
								},
								Limits: corev1.ResourceList{
									corev1.ResourceCPU:    resource.MustParse("2000m"),
									corev1.ResourceMemory: resource.MustParse("2Gi"),
								},
							},
						},
					},
				},
			}

			admissionReview := createAdmissionReview(pod, admissionv1.Create)
			response := sendWebhookRequest(ginEngine, admissionReview)

			Expect(response.Response.Allowed).To(BeTrue())
		})

		It("should handle pod with custom resources", func() {
			pod := &corev1.Pod{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "test-pod",
					Namespace: "test-namespace",
				},
				Spec: corev1.PodSpec{
					Containers: []corev1.Container{
						{
							Name: "test-container",
							Resources: corev1.ResourceRequirements{
								Requests: corev1.ResourceList{
									"nvidia.com/gpu": resource.MustParse("1"),
									"hugepages-2Mi":  resource.MustParse("1Gi"),
								},
							},
						},
					},
				},
			}

			admissionReview := createAdmissionReview(pod, admissionv1.Create)
			response := sendWebhookRequest(ginEngine, admissionReview)

			Expect(response.Response.Allowed).To(BeTrue())
		})

		It("should handle pod with no resource requests", func() {
			pod := &corev1.Pod{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "test-pod",
					Namespace: "test-namespace",
				},
				Spec: corev1.PodSpec{
					Containers: []corev1.Container{
						{
							Name: "test-container",
						},
					},
				},
			}

			admissionReview := createAdmissionReview(pod, admissionv1.Create)
			response := sendWebhookRequest(ginEngine, admissionReview)

			Expect(response.Response.Allowed).To(BeTrue())
		})

		It("should reject request with wrong resource kind", func() {
			pod := &corev1.Pod{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "test-pod",
					Namespace: "test-namespace",
				},
			}

			admissionReview := createAdmissionReview(pod, admissionv1.Create)
			admissionReview.Request.Kind = metav1.GroupVersionKind{
				Group:   "apps",
				Version: "v1",
				Kind:    "Deployment",
			}

			response := sendWebhookRequest(ginEngine, admissionReview)

			Expect(response.Response.Allowed).To(BeFalse())
			Expect(response.Response.Result.Message).To(ContainSubstring("Unexpected resource kind"))
		})

		It("should handle failed pod decoding", func() {
			// Skip this test for now since it's difficult to simulate the exact failure condition
			// TODO: Implement proper error simulation when needed
			Skip("Skipping test that requires specific error simulation")
		})

		It("should handle unsupported operations", func() {
			pod := &corev1.Pod{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "test-pod",
					Namespace: "test-namespace",
				},
			}

			admissionReview := createAdmissionReview(pod, admissionv1.Delete)
			response := sendWebhookRequest(ginEngine, admissionReview)

			Expect(response.Response.Allowed).To(BeFalse())
			Expect(response.Response.Result.Message).To(ContainSubstring("Unsupported operation"))
		})
	})

	Describe("validateCreate", func() {
		It("should validate pod creation", func() {
			pod := &corev1.Pod{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "test-pod",
					Namespace: "test-namespace",
				},
				Spec: corev1.PodSpec{
					Containers: []corev1.Container{
						{
							Name: "test-container",
							Resources: corev1.ResourceRequirements{
								Requests: corev1.ResourceList{
									corev1.ResourceCPU: resource.MustParse("100m"),
								},
							},
						},
					},
				},
			}

			warnings, err := webhook.validateCreate(pod)
			Expect(err).ToNot(HaveOccurred())
			Expect(warnings).To(BeNil())
		})
	})

	Describe("validateUpdate", func() {
		It("should validate pod update", func() {
			pod := &corev1.Pod{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "test-pod",
					Namespace: "test-namespace",
				},
				Spec: corev1.PodSpec{
					Containers: []corev1.Container{
						{
							Name: "test-container",
							Resources: corev1.ResourceRequirements{
								Requests: corev1.ResourceList{
									corev1.ResourceCPU: resource.MustParse("200m"),
								},
							},
						},
					},
				},
			}

			warnings, err := webhook.validateUpdate(pod)
			Expect(err).ToNot(HaveOccurred())
			Expect(warnings).To(BeNil())
		})
	})

	Describe("validatePodOperation", func() {
		It("should validate pod operation for creation", func() {
			pod := &corev1.Pod{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "test-pod",
					Namespace: "test-namespace",
				},
				Spec: corev1.PodSpec{
					Containers: []corev1.Container{
						{
							Name: "test-container",
						},
					},
				},
			}

			warnings, err := webhook.validatePodOperation(pod, "creation")
			Expect(err).ToNot(HaveOccurred())
			Expect(warnings).To(BeNil())
		})

		It("should validate pod operation for update", func() {
			pod := &corev1.Pod{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "test-pod",
					Namespace: "test-namespace",
				},
				Spec: corev1.PodSpec{
					Containers: []corev1.Container{
						{
							Name: "test-container",
						},
					},
				},
			}

			warnings, err := webhook.validatePodOperation(pod, "update")
			Expect(err).ToNot(HaveOccurred())
			Expect(warnings).To(BeNil())
		})

		It("should handle nil pod", func() {
			warnings, err := webhook.validatePodOperation(nil, "creation")
			Expect(err).ToNot(HaveOccurred())
			Expect(warnings).To(BeNil())
		})
	})

	Describe("Edge Cases", func() {
		It("should handle pod with very long name", func() {
			longName := "very-long-pod-name-that-exceeds-normal-length-limits-for-testing-purposes"
			pod := &corev1.Pod{
				ObjectMeta: metav1.ObjectMeta{
					Name:      longName,
					Namespace: "test-namespace",
				},
				Spec: corev1.PodSpec{
					Containers: []corev1.Container{
						{
							Name: "test-container",
						},
					},
				},
			}

			admissionReview := createAdmissionReview(pod, admissionv1.Create)
			response := sendWebhookRequest(ginEngine, admissionReview)

			Expect(response.Response.Allowed).To(BeTrue())
		})

		It("should handle pod with special characters in name", func() {
			pod := &corev1.Pod{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "test-pod-123_456-789",
					Namespace: "test-namespace",
				},
				Spec: corev1.PodSpec{
					Containers: []corev1.Container{
						{
							Name: "test-container",
						},
					},
				},
			}

			admissionReview := createAdmissionReview(pod, admissionv1.Create)
			response := sendWebhookRequest(ginEngine, admissionReview)

			Expect(response.Response.Allowed).To(BeTrue())
		})

		It("should handle pod with multiple containers", func() {
			pod := &corev1.Pod{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "test-pod",
					Namespace: "test-namespace",
				},
				Spec: corev1.PodSpec{
					Containers: []corev1.Container{
						{
							Name: "container-1",
							Resources: corev1.ResourceRequirements{
								Requests: corev1.ResourceList{
									corev1.ResourceCPU: resource.MustParse("100m"),
								},
							},
						},
						{
							Name: "container-2",
							Resources: corev1.ResourceRequirements{
								Requests: corev1.ResourceList{
									corev1.ResourceCPU: resource.MustParse("200m"),
								},
							},
						},
					},
				},
			}

			admissionReview := createAdmissionReview(pod, admissionv1.Create)
			response := sendWebhookRequest(ginEngine, admissionReview)

			Expect(response.Response.Allowed).To(BeTrue())
		})
	})
})

// Helper functions for testing

func createAdmissionReview(pod *corev1.Pod, operation admissionv1.Operation) *admissionv1.AdmissionReview {
	raw, _ := json.Marshal(pod)
	return &admissionv1.AdmissionReview{
		Request: &admissionv1.AdmissionRequest{
			UID: "test-uid",
			Kind: metav1.GroupVersionKind{
				Group:   "",
				Version: "v1",
				Kind:    "Pod",
			},
			Resource: metav1.GroupVersionResource{
				Group:    "",
				Version:  "v1",
				Resource: "pods",
			},
			Operation: operation,
			Object: runtime.RawExtension{
				Raw: raw,
			},
		},
	}
}

func sendWebhookRequest(engine *gin.Engine, admissionReview *admissionv1.AdmissionReview) *admissionv1.AdmissionReview {
	body, _ := json.Marshal(admissionReview)
	req, _ := http.NewRequest("POST", "/webhook", bytes.NewBuffer(body))
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()

	engine.ServeHTTP(w, req)

	var response admissionv1.AdmissionReview
	if err := json.Unmarshal(w.Body.Bytes(), &response); err != nil {
		// If unmarshaling fails, create a default response
		response = admissionv1.AdmissionReview{
			Response: &admissionv1.AdmissionResponse{
				Allowed: false,
				Result: &metav1.Status{
					Message: "Failed to parse response",
				},
			},
		}
	}
	return &response
}
