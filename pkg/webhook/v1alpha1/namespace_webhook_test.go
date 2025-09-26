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
	"k8s.io/client-go/kubernetes"
	"k8s.io/client-go/kubernetes/fake"
	"sigs.k8s.io/controller-runtime/pkg/client"
	ctrlclientfake "sigs.k8s.io/controller-runtime/pkg/client/fake"

	quotav1alpha1 "github.com/powerhome/pac-quota-controller/api/v1alpha1"
	"github.com/powerhome/pac-quota-controller/pkg/kubernetes/quota"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
)

var _ = Describe("NamespaceWebhook", func() {
	var (
		ctx               context.Context
		webhook           *NamespaceWebhook
		fakeClient        kubernetes.Interface
		fakeRuntimeClient client.Client
		crqClient         *quota.CRQClient
		logger            *zap.Logger
	)

	BeforeEach(func() {
		ctx = context.Background() // Entry point context for all tests
		fakeClient = fake.NewSimpleClientset()
		scheme := runtime.NewScheme()
		_ = quotav1alpha1.AddToScheme(scheme)
		_ = corev1.AddToScheme(scheme)
		fakeRuntimeClient = ctrlclientfake.NewClientBuilder().WithScheme(scheme).Build()
		crqClient = quota.NewCRQClient(fakeRuntimeClient)
		logger, _ = zap.NewDevelopment()
		webhook = NewNamespaceWebhook(fakeClient, crqClient, logger)
	})

	BeforeEach(func() {
		// No per-test setup needed; all setup is done in BeforeAll
	})

	Describe("NewNamespaceWebhook", func() {
		It("should create a new namespace webhook", func() {
			Expect(webhook).NotTo(BeNil())
			Expect(webhook.client).To(Equal(fakeClient))
		})

		It("should create webhook with nil client", func() {
			webhook := NewNamespaceWebhook(nil, crqClient, logger)
			Expect(webhook).NotTo(BeNil())
			Expect(webhook.client).To(BeNil())
		})

		It("should create webhook with nil logger", func() {
			webhook := NewNamespaceWebhook(fakeClient, crqClient, nil)
			Expect(webhook).NotTo(BeNil())
			Expect(webhook.log).To(BeNil())
		})

		It("should create webhook with nil CRQ client", func() {
			webhook := NewNamespaceWebhook(fakeClient, nil, logger)
			Expect(webhook).NotTo(BeNil())
			Expect(webhook.crqClient).To(BeNil())
		})
	})

	Describe("validateCreate", func() {
		It("should validate namespace creation", func() {
			namespace := &corev1.Namespace{
				ObjectMeta: metav1.ObjectMeta{
					Name: "test-namespace",
				},
			}

			err := webhook.validateCreate(ctx, namespace)
			Expect(err).ToNot(HaveOccurred())
		})
	})

	Describe("validateUpdate", func() {
		It("should validate namespace update", func() {
			namespace := &corev1.Namespace{
				ObjectMeta: metav1.ObjectMeta{
					Name: "test-namespace",
				},
			}

			err := webhook.validateUpdate(ctx, namespace)
			Expect(err).ToNot(HaveOccurred())
		})
	})

	Describe("Handle", func() {
		It("should handle create operation", func() {
			// Create admission review
			admissionReview := admissionv1.AdmissionReview{
				Request: &admissionv1.AdmissionRequest{
					UID: "test-uid",
					Kind: metav1.GroupVersionKind{
						Group:   "",
						Version: "v1",
						Kind:    "Namespace",
					},
					Operation: admissionv1.Create,
					Object: runtime.RawExtension{
						Raw: []byte(`{"apiVersion":"v1","kind":"Namespace","metadata":{"name":"test-namespace"}}`),
					},
				},
			}

			// Create gin context
			gin.SetMode(gin.TestMode)
			w := httptest.NewRecorder()
			c, _ := gin.CreateTestContext(w)

			// Set up request body
			body, _ := json.Marshal(admissionReview)
			c.Request = httptest.NewRequest("POST", "/", bytes.NewBuffer(body))
			c.Request.Header.Set("Content-Type", "application/json")

			// Call Handle
			webhook.Handle(c)

			// Verify response
			Expect(w.Code).To(Equal(http.StatusOK))
		})

		It("should handle nil request", func() {
			// Create admission review with nil request
			admissionReview := admissionv1.AdmissionReview{
				Request: nil,
			}

			// Create gin context
			gin.SetMode(gin.TestMode)
			w := httptest.NewRecorder()
			c, _ := gin.CreateTestContext(w)

			// Set up request body
			body, _ := json.Marshal(admissionReview)
			c.Request = httptest.NewRequest("POST", "/", bytes.NewBuffer(body))
			c.Request.Header.Set("Content-Type", "application/json")

			// Call Handle
			webhook.Handle(c)

			// Verify response
			Expect(w.Code).To(Equal(http.StatusOK))
		})

		It("should handle invalid JSON", func() {
			// Create gin context with invalid JSON
			gin.SetMode(gin.TestMode)
			w := httptest.NewRecorder()
			c, _ := gin.CreateTestContext(w)

			// Set up request body with invalid JSON
			c.Request = httptest.NewRequest("POST", "/", bytes.NewBuffer([]byte("invalid json")))
			c.Request.Header.Set("Content-Type", "application/json")

			// Call Handle
			webhook.Handle(c)

			// Verify response - webhook returns 400 for invalid JSON
			Expect(w.Code).To(Equal(http.StatusBadRequest))
		})

		It("should handle update operation", func() {
			// Create admission review for update
			admissionReview := admissionv1.AdmissionReview{
				Request: &admissionv1.AdmissionRequest{
					UID: "test-uid",
					Kind: metav1.GroupVersionKind{
						Group:   "",
						Version: "v1",
						Kind:    "Namespace",
					},
					Operation: admissionv1.Update,
					Object: runtime.RawExtension{
						Raw: createNamespaceJSON("test-namespace", map[string]string{"team": "updated"}),
					},
				},
			}

			// Create gin context
			gin.SetMode(gin.TestMode)
			w := httptest.NewRecorder()
			c, _ := gin.CreateTestContext(w)

			// Set up request body
			body, _ := json.Marshal(admissionReview)
			c.Request = httptest.NewRequest("POST", "/", bytes.NewBuffer(body))
			c.Request.Header.Set("Content-Type", "application/json")

			// Call Handle
			webhook.Handle(c)

			// Verify response
			Expect(w.Code).To(Equal(http.StatusOK))
		})

		It("should handle delete operation", func() {
			// Create admission review for delete
			admissionReview := admissionv1.AdmissionReview{
				Request: &admissionv1.AdmissionRequest{
					UID: "test-uid",
					Kind: metav1.GroupVersionKind{
						Group:   "",
						Version: "v1",
						Kind:    "Namespace",
					},
					Operation: admissionv1.Delete,
					Object: runtime.RawExtension{
						Raw: []byte(`{"apiVersion":"v1","kind":"Namespace","metadata":{"name":"test-namespace"}}`),
					},
				},
			}

			// Create gin context
			gin.SetMode(gin.TestMode)
			w := httptest.NewRecorder()
			c, _ := gin.CreateTestContext(w)

			// Set up request body
			body, _ := json.Marshal(admissionReview)
			c.Request = httptest.NewRequest("POST", "/", bytes.NewBuffer(body))
			c.Request.Header.Set("Content-Type", "application/json")

			// Call Handle
			webhook.Handle(c)

			// Verify response
			Expect(w.Code).To(Equal(http.StatusOK))
		})

		Describe("validateNamespaceAgainstCRQs edge cases", func() {
			var ctx context.Context

			It("should handle namespace with no CRQ client", func() {
				// Create webhook without CRQ client
				webhookNoCRQ := NewNamespaceWebhook(fakeClient, nil, zap.NewNop())

				namespace := &corev1.Namespace{
					ObjectMeta: metav1.ObjectMeta{
						Name: "test-no-crq",
						Labels: map[string]string{
							"test": "value",
						},
					},
				}

				err := webhookNoCRQ.validateNamespaceAgainstCRQs(ctx, namespace)
				Expect(err).NotTo(HaveOccurred()) // Should pass when no CRQ client
			})

			It("should validate namespace against multiple CRQs", func() {
				// Create multiple CRQs that would conflict
				crq1 := &quotav1alpha1.ClusterResourceQuota{
					ObjectMeta: metav1.ObjectMeta{
						Name: "test-crq-1",
					},
					Spec: quotav1alpha1.ClusterResourceQuotaSpec{
						NamespaceSelector: &metav1.LabelSelector{
							MatchLabels: map[string]string{
								"environment": "production",
							},
						},
						Hard: quotav1alpha1.ResourceList{
							"requests.cpu": resource.MustParse("10"),
						},
					},
				}
				Expect(fakeRuntimeClient.Create(ctx, crq1)).To(Succeed())

				crq2 := &quotav1alpha1.ClusterResourceQuota{
					ObjectMeta: metav1.ObjectMeta{
						Name: "test-crq-2",
					},
					Spec: quotav1alpha1.ClusterResourceQuotaSpec{
						NamespaceSelector: &metav1.LabelSelector{
							MatchLabels: map[string]string{
								"team": "backend",
							},
						},
						Hard: quotav1alpha1.ResourceList{
							"requests.memory": resource.MustParse("20Gi"),
						},
					},
				}
				Expect(fakeRuntimeClient.Create(ctx, crq2)).To(Succeed())

				namespace := &corev1.Namespace{
					ObjectMeta: metav1.ObjectMeta{
						Name: "multi-crq-test",
						Labels: map[string]string{
							"environment": "production",
							"team":        "backend",
						},
					},
				}

				err := webhook.validateNamespaceAgainstCRQs(ctx, namespace)
				Expect(err).To(HaveOccurred()) // Should fail when multiple CRQs select the same namespace
				Expect(err.Error()).To(ContainSubstring("multiple ClusterResourceQuotas select namespace"))
			})

			It("should handle namespace with no matching CRQs", func() {
				namespace := &corev1.Namespace{
					ObjectMeta: metav1.ObjectMeta{
						Name: "no-match-test",
						Labels: map[string]string{
							"unmatched": "label",
						},
					},
				}

				err := webhook.validateNamespaceAgainstCRQs(ctx, namespace)
				Expect(err).NotTo(HaveOccurred()) // Should pass when no CRQs match
			})

			It("should handle namespace with empty labels", func() {
				namespace := &corev1.Namespace{
					ObjectMeta: metav1.ObjectMeta{
						Name: "no-labels-test",
						// No labels
					},
				}

				err := webhook.validateNamespaceAgainstCRQs(ctx, namespace)
				Expect(err).NotTo(HaveOccurred()) // Should pass when namespace has no labels
			})

			It("should handle conflicting CRQ scenarios", func() {
				// Create a CRQ that already has usage
				existingNs := &corev1.Namespace{
					ObjectMeta: metav1.ObjectMeta{
						Name: "existing-ns",
						Labels: map[string]string{
							"quota-test": "true",
						},
					},
				}
				Expect(fakeRuntimeClient.Create(ctx, existingNs)).To(Succeed())

				// Create pods in existing namespace to use up quota
				for i := 0; i < 3; i++ {
					pod := &corev1.Pod{
						ObjectMeta: metav1.ObjectMeta{
							Name:      fmt.Sprintf("existing-pod-%d", i),
							Namespace: "existing-ns",
						},
						Spec: corev1.PodSpec{
							Containers: []corev1.Container{
								{
									Name:  "test-container",
									Image: "nginx:latest",
									Resources: corev1.ResourceRequirements{
										Requests: corev1.ResourceList{
											corev1.ResourceCPU: resource.MustParse("1"),
										},
									},
								},
							},
						},
					}
					Expect(fakeRuntimeClient.Create(ctx, pod)).To(Succeed())
				}

				crq := &quotav1alpha1.ClusterResourceQuota{
					ObjectMeta: metav1.ObjectMeta{
						Name: "conflict-crq",
					},
					Spec: quotav1alpha1.ClusterResourceQuotaSpec{
						NamespaceSelector: &metav1.LabelSelector{
							MatchLabels: map[string]string{
								"quota-test": "true",
							},
						},
						Hard: quotav1alpha1.ResourceList{
							"requests.cpu": resource.MustParse("5"), // Limited quota
						},
					},
				}
				Expect(fakeRuntimeClient.Create(ctx, crq)).To(Succeed())

				// Try to create new namespace with same labels
				newNamespace := &corev1.Namespace{
					ObjectMeta: metav1.ObjectMeta{
						Name: "new-conflict-ns",
						Labels: map[string]string{
							"quota-test": "true",
						},
					},
				}

				err := webhook.validateNamespaceAgainstCRQs(ctx, newNamespace)
				// This should pass because namespace validation doesn't check current usage
				Expect(err).NotTo(HaveOccurred())
			})
		})
	})
})

// Helper function to create namespace JSON
func createNamespaceJSON(name string, labels map[string]string) []byte {
	labelsJSON := ""
	if len(labels) > 0 {
		labelsJSON = fmt.Sprintf(`,"labels":%s`, fmt.Sprintf(`{"team":"%s"}`, labels["team"]))
	}
	return []byte(fmt.Sprintf(`{"apiVersion":"v1","kind":"Namespace","metadata":{"name":"%s"%s}}`, name, labelsJSON))
}
