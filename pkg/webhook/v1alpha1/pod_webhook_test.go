package v1alpha1

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
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
		ctx               context.Context
		webhook           *PodWebhook
		fakeClient        kubernetes.Interface
		fakeRuntimeClient client.Client
		crqClient         *quota.CRQClient
		logger            *zap.Logger
		ginEngine         *gin.Engine
		testNamespace     *corev1.Namespace
	)

	BeforeEach(func() {
		testNamespace = &corev1.Namespace{
			ObjectMeta: metav1.ObjectMeta{
				Name: "test-namespace",
			},
		}
		fakeClient = fake.NewSimpleClientset(testNamespace)
		scheme := runtime.NewScheme()
		_ = quotav1alpha1.AddToScheme(scheme)
		_ = corev1.AddToScheme(scheme)
		fakeRuntimeClient = ctrlclientfake.NewClientBuilder().WithScheme(scheme).Build()
		crqClient = quota.NewCRQClient(fakeRuntimeClient)
		logger = zap.NewNop()
		webhook = NewPodWebhook(fakeClient, crqClient, logger)
		gin.SetMode(gin.TestMode)
		ginEngine = gin.New()
		ginEngine.POST("/webhook", webhook.Handle)
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

			admissionReview := createPodAdmissionReview(pod, admissionv1.Create)
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

			admissionReview := createPodAdmissionReview(pod, admissionv1.Update)
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

			admissionReviewJSON, err := json.Marshal(admissionReview)
			Expect(err).NotTo(HaveOccurred())
			req, _ := http.NewRequest("POST", "/webhook", bytes.NewBuffer(admissionReviewJSON))
			req.Header.Set("Content-Type", "application/json")
			w := httptest.NewRecorder()

			ginEngine.ServeHTTP(w, req)

			Expect(w.Code).To(Equal(http.StatusBadRequest))
		})

		It("should reject request with wrong resource kind", func() {
			pod := &corev1.Pod{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "test-pod",
					Namespace: "test-namespace",
				},
			}

			admissionReview := createPodAdmissionReview(pod, admissionv1.Create)
			admissionReview.Request.Kind = metav1.GroupVersionKind{
				Group:   "apps",
				Version: "v1",
				Kind:    "Deployment",
			}

			response := sendWebhookRequest(ginEngine, admissionReview)

			Expect(response.Response.Allowed).To(BeFalse())
			Expect(response.Response.Result.Message).To(ContainSubstring("Expected Pod resource"))
		})

		It("should reject request with invalid JSON", func() {
			req, _ := http.NewRequest("POST", "/webhook", bytes.NewBuffer([]byte("invalid json")))
			req.Header.Set("Content-Type", "application/json")
			w := httptest.NewRecorder()

			ginEngine.ServeHTTP(w, req)

			Expect(w.Code).To(Equal(http.StatusBadRequest))
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

			admissionReview := createPodAdmissionReview(pod, admissionv1.Create)
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

			admissionReview := createPodAdmissionReview(pod, admissionv1.Create)
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

			admissionReview := createPodAdmissionReview(pod, admissionv1.Create)
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

			admissionReview := createPodAdmissionReview(pod, admissionv1.Create)
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

			admissionReview := createPodAdmissionReview(pod, admissionv1.Create)
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

			admissionReview := createPodAdmissionReview(pod, admissionv1.Create)
			admissionReview.Request.Kind = metav1.GroupVersionKind{
				Group:   "apps",
				Version: "v1",
				Kind:    "Deployment",
			}

			response := sendWebhookRequest(ginEngine, admissionReview)

			Expect(response.Response.Allowed).To(BeFalse())
			Expect(response.Response.Result.Message).To(ContainSubstring("Expected Pod resource"))
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

			warnings, err := webhook.validateCreate(ctx, pod)
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

			warnings, err := webhook.validateUpdate(ctx, pod)
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

			warnings, err := webhook.validatePodOperation(ctx, pod, "creation")
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

			warnings, err := webhook.validatePodOperation(ctx, pod, "update")
			Expect(err).ToNot(HaveOccurred())
			Expect(warnings).To(BeNil())
		})

		It("should handle nil pod", func() {
			warnings, err := webhook.validatePodOperation(ctx, nil, "creation")
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

			admissionReview := createPodAdmissionReview(pod, admissionv1.Create)
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

			admissionReview := createPodAdmissionReview(pod, admissionv1.Create)
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

			admissionReview := createPodAdmissionReview(pod, admissionv1.Create)
			response := sendWebhookRequest(ginEngine, admissionReview)

			Expect(response.Response.Allowed).To(BeTrue())
		})
	})

	Describe("Cross-Namespace Quota Validation", func() {
		var (
			crq         *quotav1alpha1.ClusterResourceQuota
			namespace1  *corev1.Namespace
			namespace2  *corev1.Namespace
			namespace3  *corev1.Namespace // For non-matching namespace tests
			existingPod *corev1.Pod
		)

		BeforeEach(func() {
			// Create test namespaces with matching labels
			namespace1 = &corev1.Namespace{
				ObjectMeta: metav1.ObjectMeta{
					Name: "test-ns-1",
					Labels: map[string]string{
						"environment": "test",
						"team":        "platform",
					},
				},
			}
			namespace2 = &corev1.Namespace{
				ObjectMeta: metav1.ObjectMeta{
					Name: "test-ns-2",
					Labels: map[string]string{
						"environment": "test",
						"team":        "platform",
					},
				},
			}

			// Create a namespace that doesn't match the CRQ selector
			namespace3 = &corev1.Namespace{
				ObjectMeta: metav1.ObjectMeta{
					Name: "test-ns-3",
					Labels: map[string]string{
						"environment": "production",
						"team":        "backend",
					},
				},
			}

			// Create a ClusterResourceQuota that selects both test namespaces
			crq = &quotav1alpha1.ClusterResourceQuota{
				ObjectMeta: metav1.ObjectMeta{
					Name: "test-crq",
				},
				Spec: quotav1alpha1.ClusterResourceQuotaSpec{
					NamespaceSelector: &metav1.LabelSelector{
						MatchLabels: map[string]string{
							"environment": "test",
						},
					},
					Hard: quotav1alpha1.ResourceList{
						corev1.ResourceRequestsCPU:    resource.MustParse("300m"),
						corev1.ResourceRequestsMemory: resource.MustParse("300Mi"),
					},
				},
			}

			// Create an existing pod in namespace1 that consumes resources
			existingPod = &corev1.Pod{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "existing-pod",
					Namespace: "test-ns-1",
				},
				Spec: corev1.PodSpec{
					Containers: []corev1.Container{
						{
							Name: "existing-container",
							Resources: corev1.ResourceRequirements{
								Requests: corev1.ResourceList{
									corev1.ResourceCPU:    resource.MustParse("200m"),
									corev1.ResourceMemory: resource.MustParse("200Mi"),
								},
							},
						},
					},
				},
			}

			// Update the fake clients with the new resources
			fakeClient = fake.NewSimpleClientset(testNamespace, namespace1, namespace2, namespace3, existingPod)
			scheme := runtime.NewScheme()
			_ = quotav1alpha1.AddToScheme(scheme)
			_ = corev1.AddToScheme(scheme)
			fakeRuntimeClient = ctrlclientfake.NewClientBuilder().
				WithScheme(scheme).
				WithObjects(crq, namespace1, namespace2, namespace3).
				Build()
			crqClient = quota.NewCRQClient(fakeRuntimeClient)

			// Recreate webhook with updated clients
			webhook = NewPodWebhook(fakeClient, crqClient, logger)

			// Re-setup gin engine
			ginEngine = gin.New()
			ginEngine.POST("/webhook", webhook.Handle)
		})

		AfterEach(func() {
			// Clean up cross-namespace test resources
			crq = nil
			namespace1 = nil
			namespace2 = nil
			namespace3 = nil
			existingPod = nil
		})

		It("should reject pod that would exceed cross-namespace quota limits", func() {
			// Try to create a new pod in namespace2 that would exceed the total quota
			newPod := &corev1.Pod{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "new-pod",
					Namespace: "test-ns-2",
				},
				Spec: corev1.PodSpec{
					Containers: []corev1.Container{
						{
							Name: "new-container",
							Resources: corev1.ResourceRequirements{
								Requests: corev1.ResourceList{
									corev1.ResourceCPU: resource.MustParse("150m"), // 200m + 150m = 350m > 300m limit
								},
							},
						},
					},
				},
			}

			admissionReview := createPodAdmissionReview(newPod, admissionv1.Create)
			response := sendWebhookRequest(ginEngine, admissionReview)

			Expect(response.Response.Allowed).To(BeFalse())
			Expect(response.Response.Result.Message).To(ContainSubstring("CPU requests validation failed"))
			Expect(response.Response.Result.Message).To(ContainSubstring("test-crq"))
			Expect(response.Response.Result.Message).To(ContainSubstring("limit exceeded"))
		})

		It("should allow pod that fits within cross-namespace quota limits", func() {
			// Try to create a new pod in namespace2 that fits within the total quota
			newPod := &corev1.Pod{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "new-pod",
					Namespace: "test-ns-2",
				},
				Spec: corev1.PodSpec{
					Containers: []corev1.Container{
						{
							Name: "new-container",
							Resources: corev1.ResourceRequirements{
								Requests: corev1.ResourceList{
									corev1.ResourceCPU:    resource.MustParse("50m"),  // 200m + 50m = 250m < 300m limit
									corev1.ResourceMemory: resource.MustParse("50Mi"), // 200Mi + 50Mi = 250Mi < 300Mi limit
								},
							},
						},
					},
				},
			}

			admissionReview := createPodAdmissionReview(newPod, admissionv1.Create)
			response := sendWebhookRequest(ginEngine, admissionReview)

			Expect(response.Response.Allowed).To(BeTrue())
		})

		It("should allow pod in namespace not matching CRQ selector", func() {
			// Try to create a pod in namespace3 (doesn't match CRQ selector) with high resource requests
			newPod := &corev1.Pod{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "unmatched-pod",
					Namespace: "test-ns-3",
				},
				Spec: corev1.PodSpec{
					Containers: []corev1.Container{
						{
							Name: "container",
							Resources: corev1.ResourceRequirements{
								Requests: corev1.ResourceList{
									corev1.ResourceCPU: resource.MustParse("1000m"), // High request, but no CRQ applies
								},
							},
						},
					},
				},
			}

			admissionReview := createPodAdmissionReview(newPod, admissionv1.Create)
			response := sendWebhookRequest(ginEngine, admissionReview)

			Expect(response.Response.Allowed).To(BeTrue())
		})

		It("should handle complex label selectors", func() {
			// Create a test namespace that matches the complex selector
			complexNamespace := &corev1.Namespace{
				ObjectMeta: metav1.ObjectMeta{
					Name: "complex-ns",
					Labels: map[string]string{
						"team":        "platform",
						"environment": "staging", // Not production, not test
					},
				},
			}

			// Create a CRQ with complex label selector (MatchExpressions)
			complexCRQ := &quotav1alpha1.ClusterResourceQuota{
				ObjectMeta: metav1.ObjectMeta{
					Name: "complex-crq",
				},
				Spec: quotav1alpha1.ClusterResourceQuotaSpec{
					NamespaceSelector: &metav1.LabelSelector{
						MatchExpressions: []metav1.LabelSelectorRequirement{
							{
								Key:      "team",
								Operator: metav1.LabelSelectorOpIn,
								Values:   []string{"platform", "backend"},
							},
							{
								Key:      "environment",
								Operator: metav1.LabelSelectorOpNotIn,
								Values:   []string{"production"},
							},
						},
					},
					Hard: quotav1alpha1.ResourceList{
						corev1.ResourceRequestsCPU: resource.MustParse("500m"),
					},
				},
			}

			// Update clients with new resources
			fakeClient = fake.NewSimpleClientset(
				testNamespace, namespace1, namespace2, namespace3, existingPod, complexNamespace)
			err := fakeRuntimeClient.Create(ctx, complexCRQ)
			Expect(err).NotTo(HaveOccurred())
			err = fakeRuntimeClient.Create(ctx, complexNamespace)
			Expect(err).NotTo(HaveOccurred())

			// Recreate webhook with updated client
			webhook = NewPodWebhook(fakeClient, crqClient, logger)
			ginEngine = gin.New()
			ginEngine.POST("/webhook", webhook.Handle)

			// Try to create a pod in the complex namespace (should match complex selector)
			newPod := &corev1.Pod{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "complex-pod",
					Namespace: "complex-ns",
				},
				Spec: corev1.PodSpec{
					Containers: []corev1.Container{
						{
							Name: "container",
							Resources: corev1.ResourceRequirements{
								Requests: corev1.ResourceList{
									corev1.ResourceCPU: resource.MustParse("100m"),
								},
							},
						},
					},
				},
			}

			admissionReview := createPodAdmissionReview(newPod, admissionv1.Create)
			response := sendWebhookRequest(ginEngine, admissionReview)

			Expect(response.Response.Allowed).To(BeTrue())
		})

		It("should handle multiple containers across namespaces", func() {
			// Create a pod with multiple containers that would exceed limits
			multiContainerPod := &corev1.Pod{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "multi-container-pod",
					Namespace: "test-ns-2",
				},
				Spec: corev1.PodSpec{
					Containers: []corev1.Container{
						{
							Name: "container-1",
							Resources: corev1.ResourceRequirements{
								Requests: corev1.ResourceList{
									corev1.ResourceCPU: resource.MustParse("80m"),
								},
							},
						},
						{
							Name: "container-2",
							Resources: corev1.ResourceRequirements{
								Requests: corev1.ResourceList{
									corev1.ResourceCPU: resource.MustParse("80m"),
								},
							},
						},
					},
				},
			}

			admissionReview := createPodAdmissionReview(multiContainerPod, admissionv1.Create)
			response := sendWebhookRequest(ginEngine, admissionReview)

			// 200m (existing) + 80m + 80m = 360m > 300m limit
			Expect(response.Response.Allowed).To(BeFalse())
			Expect(response.Response.Result.Message).To(ContainSubstring("test-crq"))
			Expect(response.Response.Result.Message).To(ContainSubstring("limit exceeded"))
		})

		It("should handle init containers in cross-namespace scenario", func() {
			// Create a pod with init containers that would exceed limits
			initContainerPod := &corev1.Pod{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "init-container-pod",
					Namespace: "test-ns-2",
				},
				Spec: corev1.PodSpec{
					InitContainers: []corev1.Container{
						{
							Name: "init-container",
							Resources: corev1.ResourceRequirements{
								Requests: corev1.ResourceList{
									corev1.ResourceCPU: resource.MustParse("150m"),
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

			admissionReview := createPodAdmissionReview(initContainerPod, admissionv1.Create)
			response := sendWebhookRequest(ginEngine, admissionReview)

			// Should use max(init: 150m, main: 100m) = 150m
			// 200m (existing) + 150m = 350m > 300m limit
			Expect(response.Response.Allowed).To(BeFalse())
			Expect(response.Response.Result.Message).To(ContainSubstring("test-crq"))
		})

		It("should validate memory resources across namespaces", func() {
			// Create a pod that would exceed memory limits
			memoryPod := &corev1.Pod{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "memory-pod",
					Namespace: "test-ns-2",
				},
				Spec: corev1.PodSpec{
					Containers: []corev1.Container{
						{
							Name: "memory-container",
							Resources: corev1.ResourceRequirements{
								Requests: corev1.ResourceList{
									corev1.ResourceMemory: resource.MustParse("150Mi"),
								},
							},
						},
					},
				},
			}

			admissionReview := createPodAdmissionReview(memoryPod, admissionv1.Create)
			response := sendWebhookRequest(ginEngine, admissionReview)

			// 200Mi (existing) + 150Mi = 350Mi > 300Mi limit
			Expect(response.Response.Allowed).To(BeFalse())
			Expect(response.Response.Result.Message).To(ContainSubstring("memory requests validation failed"))
			Expect(response.Response.Result.Message).To(ContainSubstring("test-crq"))
		})

		It("should handle CRQ with no matching namespaces", func() {
			// Create a namespace with unique labels for this test
			isolatedNamespace := &corev1.Namespace{
				ObjectMeta: metav1.ObjectMeta{
					Name: "isolated-ns",
					Labels: map[string]string{
						"isolated": "true",
					},
				},
			}

			// Create a CRQ that doesn't match any namespaces
			noMatchCRQ := &quotav1alpha1.ClusterResourceQuota{
				ObjectMeta: metav1.ObjectMeta{
					Name: "no-match-crq",
				},
				Spec: quotav1alpha1.ClusterResourceQuotaSpec{
					NamespaceSelector: &metav1.LabelSelector{
						MatchLabels: map[string]string{
							"nonexistent": "label",
						},
					},
					Hard: quotav1alpha1.ResourceList{
						corev1.ResourceRequestsCPU: resource.MustParse("100m"),
					},
				},
			}

			// Update clients with new resources
			fakeClient = fake.NewSimpleClientset(
				testNamespace, namespace1, namespace2, namespace3, existingPod, isolatedNamespace)
			err := fakeRuntimeClient.Create(ctx, noMatchCRQ)
			Expect(err).NotTo(HaveOccurred())
			err = fakeRuntimeClient.Create(ctx, isolatedNamespace)
			Expect(err).NotTo(HaveOccurred())

			// Recreate webhook with updated client
			webhook = NewPodWebhook(fakeClient, crqClient, logger)
			ginEngine = gin.New()
			ginEngine.POST("/webhook", webhook.Handle)

			// Create a pod in the isolated namespace - should be allowed since CRQ doesn't apply
			newPod := &corev1.Pod{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "no-match-pod",
					Namespace: "isolated-ns",
				},
				Spec: corev1.PodSpec{
					Containers: []corev1.Container{
						{
							Name: "container",
							Resources: corev1.ResourceRequirements{
								Requests: corev1.ResourceList{
									corev1.ResourceCPU: resource.MustParse("500m"),
								},
							},
						},
					},
				},
			}

			admissionReview := createPodAdmissionReview(newPod, admissionv1.Create)
			response := sendWebhookRequest(ginEngine, admissionReview)

			Expect(response.Response.Allowed).To(BeTrue())
		})

		Describe("calculateCurrentUsage function coverage", func() {
			It("should handle CPU requests correctly", func() {
				// Create a pod with CPU requests
				pod := &corev1.Pod{
					ObjectMeta: metav1.ObjectMeta{
						Name:      "cpu-test-pod",
						Namespace: "test-namespace",
					},
					Spec: corev1.PodSpec{
						Containers: []corev1.Container{
							{
								Name:  "test-container",
								Image: "nginx:latest",
								Resources: corev1.ResourceRequirements{
									Requests: corev1.ResourceList{
										corev1.ResourceCPU: resource.MustParse("500m"),
									},
								},
							},
						},
					},
				}
				_, err := fakeClient.CoreV1().Pods("test-namespace").Create(ctx, pod, metav1.CreateOptions{})
				Expect(err).NotTo(HaveOccurred())

				usage, err := webhook.calculateCurrentUsage(ctx, "test-namespace", corev1.ResourceName("requests.cpu"))
				Expect(err).NotTo(HaveOccurred())
				Expect(usage.MilliValue()).To(Equal(int64(500))) // 500m CPU
			})

			It("should handle memory requests correctly", func() {
				// Create a pod with memory requests
				pod := &corev1.Pod{
					ObjectMeta: metav1.ObjectMeta{
						Name:      "memory-test-pod",
						Namespace: "test-namespace",
					},
					Spec: corev1.PodSpec{
						Containers: []corev1.Container{
							{
								Name:  "test-container",
								Image: "nginx:latest",
								Resources: corev1.ResourceRequirements{
									Requests: corev1.ResourceList{
										corev1.ResourceMemory: resource.MustParse("256Mi"),
									},
								},
							},
						},
					},
				}
				_, err := fakeClient.CoreV1().Pods("test-namespace").Create(ctx, pod, metav1.CreateOptions{})
				Expect(err).NotTo(HaveOccurred())

				usage, err := webhook.calculateCurrentUsage(ctx, "test-namespace", corev1.ResourceName("requests.memory"))
				Expect(err).NotTo(HaveOccurred())
				Expect(usage.Value()).To(Equal(int64(256 * 1024 * 1024))) // 256Mi in bytes
			})

			It("should handle CPU limits correctly", func() {
				// Create a pod with CPU limits
				pod := &corev1.Pod{
					ObjectMeta: metav1.ObjectMeta{
						Name:      "cpu-limit-test-pod",
						Namespace: "test-namespace",
					},
					Spec: corev1.PodSpec{
						Containers: []corev1.Container{
							{
								Name:  "test-container",
								Image: "nginx:latest",
								Resources: corev1.ResourceRequirements{
									Limits: corev1.ResourceList{
										corev1.ResourceCPU: resource.MustParse("1"),
									},
								},
							},
						},
					},
				}
				_, err := fakeClient.CoreV1().Pods("test-namespace").Create(ctx, pod, metav1.CreateOptions{})
				Expect(err).NotTo(HaveOccurred())

				usage, err := webhook.calculateCurrentUsage(ctx, "test-namespace", corev1.ResourceName("limits.cpu"))
				Expect(err).NotTo(HaveOccurred())
				Expect(usage.MilliValue()).To(Equal(int64(1000))) // 1 CPU = 1000m
			})

			It("should handle memory limits correctly", func() {
				// Create a pod with memory limits
				pod := &corev1.Pod{
					ObjectMeta: metav1.ObjectMeta{
						Name:      "memory-limit-test-pod",
						Namespace: "test-namespace",
					},
					Spec: corev1.PodSpec{
						Containers: []corev1.Container{
							{
								Name:  "test-container",
								Image: "nginx:latest",
								Resources: corev1.ResourceRequirements{
									Limits: corev1.ResourceList{
										corev1.ResourceMemory: resource.MustParse("512Mi"),
									},
								},
							},
						},
					},
				}
				_, err := fakeClient.CoreV1().Pods("test-namespace").Create(ctx, pod, metav1.CreateOptions{})
				Expect(err).NotTo(HaveOccurred())

				usage, err := webhook.calculateCurrentUsage(ctx, "test-namespace", corev1.ResourceName("limits.memory"))
				Expect(err).NotTo(HaveOccurred())
				Expect(usage.Value()).To(Equal(int64(512 * 1024 * 1024))) // 512Mi in bytes
			})

			It("should handle pods in terminal states (Succeeded)", func() {
				testTerminalPodState(ctx, corev1.PodSucceeded, "succeeded-pod", "terminal-test-namespace",
					&fakeClient, crqClient, logger)
			})

			It("should handle pods in terminal states (Failed)", func() {
				testTerminalPodState(ctx, corev1.PodFailed, "failed-pod", "failed-test-namespace",
					&fakeClient, crqClient, logger)
			})

			It("should handle pods with both init and regular containers", func() {
				// Create a fresh namespace for this test to ensure clean state
				testNs := &corev1.Namespace{
					ObjectMeta: metav1.ObjectMeta{
						Name: "init-container-test-namespace",
					},
				}
				fakeClient = fake.NewSimpleClientset(testNs)
				webhook = NewPodWebhook(fakeClient, crqClient, logger)

				// Create a pod with both init and regular containers
				pod := &corev1.Pod{
					ObjectMeta: metav1.ObjectMeta{
						Name:      "init-and-regular-pod",
						Namespace: "init-container-test-namespace",
					},
					Spec: corev1.PodSpec{
						InitContainers: []corev1.Container{
							{
								Name:  "init-container",
								Image: "busybox:latest",
								Resources: corev1.ResourceRequirements{
									Requests: corev1.ResourceList{
										corev1.ResourceCPU: resource.MustParse("50m"),
									},
								},
							},
						},
						Containers: []corev1.Container{
							{
								Name:  "main-container",
								Image: "nginx:latest",
								Resources: corev1.ResourceRequirements{
									Requests: corev1.ResourceList{
										corev1.ResourceCPU: resource.MustParse("100m"),
									},
								},
							},
						},
					},
				}
				_, err := fakeClient.CoreV1().Pods("init-container-test-namespace").Create(ctx, pod, metav1.CreateOptions{})
				Expect(err).NotTo(HaveOccurred())

				usage, err := webhook.calculateCurrentUsage(
					ctx,
					"init-container-test-namespace",
					corev1.ResourceName("requests.cpu"),
				)
				Expect(err).NotTo(HaveOccurred())
				// Should use Max(init: 50m, main: 100m) = 100m
				Expect(usage.MilliValue()).To(Equal(int64(100)))
			})

			It("should return error for unsupported resource types", func() {
				_, err := webhook.calculateCurrentUsage(ctx, "test-namespace", corev1.ResourceName("unsupported.resource"))
				Expect(err).To(HaveOccurred())
				Expect(err.Error()).To(ContainSubstring("unsupported resource type"))
			})

			It("should handle pod count calculation", func() {
				// Create a fresh namespace for this test to ensure clean state
				testNs := &corev1.Namespace{
					ObjectMeta: metav1.ObjectMeta{
						Name: "pod-count-namespace",
					},
				}
				fakeClient = fake.NewSimpleClientset(testNs)
				webhook = NewPodWebhook(fakeClient, crqClient, logger)

				// Create multiple pods to test count
				for i := 0; i < 3; i++ {
					pod := &corev1.Pod{
						ObjectMeta: metav1.ObjectMeta{
							Name:      fmt.Sprintf("count-pod-%d", i),
							Namespace: "pod-count-namespace",
						},
						Spec: corev1.PodSpec{
							Containers: []corev1.Container{
								{
									Name:  "test-container",
									Image: "nginx:latest",
								},
							},
						},
					}
					_, err := fakeClient.CoreV1().Pods("pod-count-namespace").Create(ctx, pod, metav1.CreateOptions{})
					Expect(err).NotTo(HaveOccurred())
				}

				// Test pod count
				usage, err := webhook.calculateCurrentUsage(ctx, "pod-count-namespace", corev1.ResourceName("pods"))
				Expect(err).NotTo(HaveOccurred())
				Expect(usage.Value()).To(Equal(int64(3))) // Should count 3 pods
			})

			It("should handle non-existent namespace", func() {
				usage, err := webhook.calculateCurrentUsage(ctx, "non-existent-namespace", corev1.ResourceName("requests.cpu"))
				Expect(err).NotTo(HaveOccurred())
				Expect(usage.IsZero()).To(BeTrue())
			})

			It("should handle namespace with no pods", func() {
				// Create empty namespace
				emptyNs := &corev1.Namespace{
					ObjectMeta: metav1.ObjectMeta{
						Name: "empty-namespace",
					},
				}
				Expect(fakeRuntimeClient.Create(ctx, emptyNs)).To(Succeed())

				usage, err := webhook.calculateCurrentUsage(ctx, "empty-namespace", corev1.ResourceName("requests.cpu"))
				Expect(err).NotTo(HaveOccurred())
				Expect(usage.IsZero()).To(BeTrue())
			})

			It("should handle zero resource requests/limits", func() {
				// Create a fresh namespace for this test to ensure clean state
				testNs := &corev1.Namespace{
					ObjectMeta: metav1.ObjectMeta{
						Name: "zero-resource-namespace",
					},
				}
				fakeClient = fake.NewSimpleClientset(testNs)
				webhook = NewPodWebhook(fakeClient, crqClient, logger)

				// Create a pod with zero resource requests (should contribute 0 to usage)
				pod := &corev1.Pod{
					ObjectMeta: metav1.ObjectMeta{
						Name:      "zero-resource-pod",
						Namespace: "zero-resource-namespace",
					},
					Spec: corev1.PodSpec{
						Containers: []corev1.Container{
							{
								Name:  "test-container",
								Image: "nginx:latest",
								Resources: corev1.ResourceRequirements{
									Requests: corev1.ResourceList{
										corev1.ResourceCPU: resource.MustParse("0"),
									},
								},
							},
						},
					},
				}
				_, err := fakeClient.CoreV1().Pods("zero-resource-namespace").Create(ctx, pod, metav1.CreateOptions{})
				Expect(err).NotTo(HaveOccurred())

				usage, err := webhook.calculateCurrentUsage(ctx, "zero-resource-namespace", corev1.ResourceName("requests.cpu"))
				Expect(err).NotTo(HaveOccurred())
				// Should be 0 since the pod requests 0 CPU
				Expect(usage.MilliValue()).To(Equal(int64(0)))
			})

			It("should handle mixed resource types in same pod", func() {
				// Create a fresh namespace for this test to ensure clean state
				testNs := &corev1.Namespace{
					ObjectMeta: metav1.ObjectMeta{
						Name: "mixed-resource-namespace",
					},
				}
				fakeClient = fake.NewSimpleClientset(testNs)
				webhook = NewPodWebhook(fakeClient, crqClient, logger)

				// Create a pod with mixed resource types to test calculator behavior
				pod := &corev1.Pod{
					ObjectMeta: metav1.ObjectMeta{
						Name:      "mixed-resource-pod",
						Namespace: "mixed-resource-namespace",
					},
					Spec: corev1.PodSpec{
						Containers: []corev1.Container{
							{
								Name:  "test-container",
								Image: "nginx:latest",
								Resources: corev1.ResourceRequirements{
									Requests: corev1.ResourceList{
										corev1.ResourceCPU:    resource.MustParse("100m"),
										corev1.ResourceMemory: resource.MustParse("128Mi"),
									},
									Limits: corev1.ResourceList{
										corev1.ResourceCPU:    resource.MustParse("200m"),
										corev1.ResourceMemory: resource.MustParse("256Mi"),
									},
								},
							},
						},
					},
				}
				_, err := fakeClient.CoreV1().Pods("mixed-resource-namespace").Create(ctx, pod, metav1.CreateOptions{})
				Expect(err).NotTo(HaveOccurred())

				// Test CPU requests
				cpuUsage, err := webhook.calculateCurrentUsage(ctx, "mixed-resource-namespace", corev1.ResourceName("requests.cpu"))
				Expect(err).NotTo(HaveOccurred())
				Expect(cpuUsage.MilliValue()).To(Equal(int64(100))) // Just this pod: 100m

				// Test memory limits
				memoryUsage, err := webhook.calculateCurrentUsage(
					ctx,
					"mixed-resource-namespace",
					corev1.ResourceName("limits.memory"),
				)
				Expect(err).NotTo(HaveOccurred())
				expectedMemory := int64(256 * 1024 * 1024) // 256Mi
				Expect(memoryUsage.Value()).To(Equal(expectedMemory))
			})

			It("should handle multiple containers with different resource patterns", func() {
				// Create a fresh namespace for this test to ensure clean state
				testNs := &corev1.Namespace{
					ObjectMeta: metav1.ObjectMeta{
						Name: "multi-pattern-namespace",
					},
				}
				fakeClient = fake.NewSimpleClientset(testNs)
				webhook = NewPodWebhook(fakeClient, crqClient, logger)

				// Create a pod with multiple containers having different resource configurations
				pod := &corev1.Pod{
					ObjectMeta: metav1.ObjectMeta{
						Name:      "multi-pattern-pod",
						Namespace: "multi-pattern-namespace",
					},
					Spec: corev1.PodSpec{
						Containers: []corev1.Container{
							{
								Name:  "cpu-only-container",
								Image: "nginx:latest",
								Resources: corev1.ResourceRequirements{
									Requests: corev1.ResourceList{
										corev1.ResourceCPU: resource.MustParse("50m"),
									},
								},
							},
							{
								Name:  "memory-only-container",
								Image: "redis:latest",
								Resources: corev1.ResourceRequirements{
									Requests: corev1.ResourceList{
										corev1.ResourceMemory: resource.MustParse("64Mi"),
									},
								},
							},
							{
								Name:  "limits-only-container",
								Image: "postgres:latest",
								Resources: corev1.ResourceRequirements{
									Limits: corev1.ResourceList{
										corev1.ResourceCPU: resource.MustParse("200m"),
									},
								},
							},
						},
					},
				}
				_, err := fakeClient.CoreV1().Pods("multi-pattern-namespace").Create(ctx, pod, metav1.CreateOptions{})
				Expect(err).NotTo(HaveOccurred())

				// Test CPU requests (should only count cpu-only-container)
				cpuUsage, err := webhook.calculateCurrentUsage(ctx, "multi-pattern-namespace", corev1.ResourceName("requests.cpu"))
				Expect(err).NotTo(HaveOccurred())
				Expect(cpuUsage.MilliValue()).To(Equal(int64(50))) // Just this pod: 50m

				// Test memory requests (should only count memory-only-container)
				memoryUsage, err := webhook.calculateCurrentUsage(
					ctx,
					"multi-pattern-namespace",
					corev1.ResourceName("requests.memory"),
				)
				Expect(err).NotTo(HaveOccurred())
				expectedMemory := int64(64 * 1024 * 1024) // 64Mi
				Expect(memoryUsage.Value()).To(Equal(expectedMemory))

				// Test CPU limits (should only count limits-only-container)
				cpuLimitsUsage, err := webhook.calculateCurrentUsage(
					ctx,
					"multi-pattern-namespace",
					corev1.ResourceName("limits.cpu"),
				)
				Expect(err).NotTo(HaveOccurred())
				Expect(cpuLimitsUsage.MilliValue()).To(Equal(int64(200))) // 200m
			})
		})
	})
})

// Helper functions for testing

func createPodAdmissionReview(pod *corev1.Pod, operation admissionv1.Operation) *admissionv1.AdmissionReview {
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
			Namespace: pod.Namespace,
			Object: runtime.RawExtension{
				Raw: raw,
			},
		},
	}
}

// testTerminalPodState is a helper function to test pods in terminal states (Succeeded/Failed)
func testTerminalPodState(ctx context.Context, phase corev1.PodPhase, podName, namespaceName string,
	fakeClient *kubernetes.Interface, crqClient *quota.CRQClient, logger *zap.Logger) {
	// Create a fresh namespace for this test to ensure clean state
	testNs := &corev1.Namespace{
		ObjectMeta: metav1.ObjectMeta{
			Name: namespaceName,
		},
	}
	*fakeClient = fake.NewSimpleClientset(testNs)
	webhook := NewPodWebhook(*fakeClient, crqClient, logger)

	// Create a pod in the specified terminal state (should not count towards usage)
	cpuRequest := "100m"
	if phase == corev1.PodFailed {
		cpuRequest = "200m"
	}

	pod := &corev1.Pod{
		ObjectMeta: metav1.ObjectMeta{
			Name:      podName,
			Namespace: namespaceName,
		},
		Spec: corev1.PodSpec{
			Containers: []corev1.Container{
				{
					Name:  "test-container",
					Image: "nginx:latest",
					Resources: corev1.ResourceRequirements{
						Requests: corev1.ResourceList{
							corev1.ResourceCPU: resource.MustParse(cpuRequest),
						},
					},
				},
			},
		},
		Status: corev1.PodStatus{
			Phase: phase,
		},
	}
	_, err := (*fakeClient).CoreV1().Pods(namespaceName).Create(ctx, pod, metav1.CreateOptions{})
	Expect(err).NotTo(HaveOccurred())

	usage, err := webhook.calculateCurrentUsage(ctx, namespaceName, "requests.cpu")
	Expect(err).NotTo(HaveOccurred())
	// Should not include the terminal pod's resources
	Expect(usage.Value()).To(Equal(int64(0)))
}
