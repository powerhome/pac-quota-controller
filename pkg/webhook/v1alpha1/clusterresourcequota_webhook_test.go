package v1alpha1

import (
	"bytes"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"

	"github.com/gin-gonic/gin"
	"go.uber.org/zap"
	admissionv1 "k8s.io/api/admission/v1"
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

var _ = Describe("ClusterResourceQuotaWebhook", func() {
	var (
		webhook           *ClusterResourceQuotaWebhook
		fakeClient        kubernetes.Interface
		fakeRuntimeClient client.Client
		crqClient         *quota.CRQClient
		logger            *zap.Logger
	)

	BeforeEach(func() {
		fakeClient = fake.NewSimpleClientset()
		scheme := runtime.NewScheme()
		_ = quotav1alpha1.AddToScheme(scheme)
		fakeRuntimeClient = ctrlclientfake.NewClientBuilder().WithScheme(scheme).Build()
		crqClient = quota.NewCRQClient(fakeRuntimeClient)
		logger, _ = zap.NewDevelopment()
		webhook = NewClusterResourceQuotaWebhook(fakeClient, crqClient, logger)
	})

	Describe("NewClusterResourceQuotaWebhook", func() {
		It("should create a new cluster resource quota webhook", func() {
			Expect(webhook).NotTo(BeNil())
			Expect(webhook.client).To(Equal(fakeClient))
		})

		It("should create webhook with nil client", func() {
			webhook := NewClusterResourceQuotaWebhook(nil, crqClient, logger)
			Expect(webhook).NotTo(BeNil())
			Expect(webhook.client).To(BeNil())
		})

		It("should create webhook with nil logger", func() {
			webhook := NewClusterResourceQuotaWebhook(fakeClient, crqClient, nil)
			Expect(webhook).NotTo(BeNil())
			Expect(webhook.log).To(BeNil())
		})

		It("should create webhook with nil CRQ client", func() {
			webhook := NewClusterResourceQuotaWebhook(fakeClient, nil, logger)
			Expect(webhook).NotTo(BeNil())
			Expect(webhook.crqClient).To(BeNil())
		})
	})

	Describe("validateCreate", func() {
		It("should validate cluster resource quota creation", func() {
			crq := &quotav1alpha1.ClusterResourceQuota{
				ObjectMeta: metav1.ObjectMeta{
					Name: "test-crq",
				},
				Spec: quotav1alpha1.ClusterResourceQuotaSpec{
					Hard: quotav1alpha1.ResourceList{
						"cpu":    resource.MustParse("4"),
						"memory": resource.MustParse("8Gi"),
					},
					NamespaceSelector: &metav1.LabelSelector{
						MatchLabels: map[string]string{
							"environment": "production",
						},
					},
				},
			}

			err := webhook.validateCreate(crq)
			Expect(err).ToNot(HaveOccurred())
		})
	})

	Describe("validateUpdate", func() {
		It("should validate cluster resource quota update", func() {
			crq := &quotav1alpha1.ClusterResourceQuota{
				ObjectMeta: metav1.ObjectMeta{
					Name: "test-crq",
				},
				Spec: quotav1alpha1.ClusterResourceQuotaSpec{
					Hard: quotav1alpha1.ResourceList{
						"cpu":    resource.MustParse("4"),
						"memory": resource.MustParse("8Gi"),
					},
					NamespaceSelector: &metav1.LabelSelector{
						MatchLabels: map[string]string{
							"environment": "production",
						},
					},
				},
			}

			err := webhook.validateUpdate(crq)
			Expect(err).ToNot(HaveOccurred())
		})
	})

	Describe("validateDelete", func() {
		It("should validate cluster resource quota deletion", func() {
			err := webhook.validateDelete()
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
						Group:   "quota.powerapp.cloud",
						Version: "v1alpha1",
						Kind:    "ClusterResourceQuota",
					},
					Operation: admissionv1.Create,
					Object: runtime.RawExtension{
						Raw: createCRQJSON("test-crq", "4", "8Gi"),
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

		It("should handle wrong resource kind", func() {
			// Create admission review with wrong resource kind
			admissionReview := admissionv1.AdmissionReview{
				Request: &admissionv1.AdmissionRequest{
					UID: "test-uid",
					Kind: metav1.GroupVersionKind{
						Group:   "v1",
						Version: "v1",
						Kind:    "Pod", // Wrong kind
					},
					Operation: admissionv1.Create,
					Object: runtime.RawExtension{
						Raw: []byte(`{"apiVersion":"v1","kind":"Pod","metadata":{"name":"test-pod"}}`),
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

		It("should handle update operation", func() {
			// Create admission review for update
			admissionReview := admissionv1.AdmissionReview{
				Request: &admissionv1.AdmissionRequest{
					UID: "test-uid",
					Kind: metav1.GroupVersionKind{
						Group:   "quota.powerapp.cloud",
						Version: "v1alpha1",
						Kind:    "ClusterResourceQuota",
					},
					Operation: admissionv1.Update,
					Object: runtime.RawExtension{
						Raw: createCRQJSON("test-crq", "8", "16Gi"),
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
						Group:   "quota.powerapp.cloud",
						Version: "v1alpha1",
						Kind:    "ClusterResourceQuota",
					},
					Operation: admissionv1.Delete,
					Object: runtime.RawExtension{
						Raw: createCRQDeleteJSON("test-crq"),
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
	})
})

// Helper function to create CRQ JSON
func createCRQJSON(name, cpu, memory string) []byte {
	jsonTemplate := `{"apiVersion":"quota.powerapp.cloud/v1alpha1","kind":"ClusterResourceQuota",` +
		`"metadata":{"name":"%s"},"spec":{"hard":{"cpu":"%s","memory":"%s"},` +
		`"namespaceSelector":{"matchLabels":{"environment":"production"}}}}`
	return []byte(fmt.Sprintf(jsonTemplate, name, cpu, memory))
}

// Helper function to create CRQ delete JSON
func createCRQDeleteJSON(name string) []byte {
	jsonTemplate := `{"apiVersion":"quota.powerapp.cloud/v1alpha1","kind":"ClusterResourceQuota","metadata":{"name":"%s"}}`
	return []byte(fmt.Sprintf(jsonTemplate, name))
}
