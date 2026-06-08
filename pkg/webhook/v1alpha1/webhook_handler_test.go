package v1alpha1

import (
	"bytes"
	"context"
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

	quotav1alpha1 "github.com/powerhome/pac-quota-controller/api/v1alpha1"
)

// postReview runs the engine and returns the parsed AdmissionReview and HTTP code.
func postReview(engine *gin.Engine, body []byte) (int, *admissionv1.AdmissionReview) {
	req := httptest.NewRequest(http.MethodPost, "/webhook", bytes.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()
	engine.ServeHTTP(w, req)
	var resp admissionv1.AdmissionReview
	_ = json.Unmarshal(w.Body.Bytes(), &resp)
	return w.Code, &resp
}

var _ = Describe("runWebhook", func() {
	var (
		engine *gin.Engine
		logger *zap.Logger
	)

	BeforeEach(func() {
		gin.SetMode(gin.TestMode)
		engine = gin.New()
		logger = zap.NewNop()
	})

	It("returns 400 on malformed JSON", func() {
		engine.POST("/webhook", func(c *gin.Context) {
			runWebhook(c, logger, webhookConfig{name: "t"},
				func(context.Context, *admissionv1.AdmissionRequest) ([]string, error) { return nil, nil })
		})
		code, _ := postReview(engine, []byte("{not-json"))
		Expect(code).To(Equal(http.StatusBadRequest))
	})

	It("returns 400 on missing request", func() {
		engine.POST("/webhook", func(c *gin.Context) {
			runWebhook(c, logger, webhookConfig{name: "t"},
				func(context.Context, *admissionv1.AdmissionRequest) ([]string, error) { return nil, nil })
		})
		body, _ := json.Marshal(admissionv1.AdmissionReview{})
		code, _ := postReview(engine, body)
		Expect(code).To(Equal(http.StatusBadRequest))
	})

	It("denies when namespace is required but empty", func() {
		engine.POST("/webhook", func(c *gin.Context) {
			runWebhook(c, logger, webhookConfig{name: "t", requireNamespace: true},
				func(context.Context, *admissionv1.AdmissionRequest) ([]string, error) { return nil, nil })
		})
		body, _ := json.Marshal(admissionv1.AdmissionReview{
			Request: &admissionv1.AdmissionRequest{UID: "1", Operation: admissionv1.Create},
		})
		code, resp := postReview(engine, body)
		Expect(code).To(Equal(http.StatusOK))
		Expect(resp.Response.Allowed).To(BeFalse())
		Expect(resp.Response.Result.Message).To(ContainSubstring("Namespace is required"))
	})

	It("denies when GVK does not match", func() {
		expected := metav1.GroupVersionKind{Group: "", Version: "v1", Kind: "Pod"}
		engine.POST("/webhook", func(c *gin.Context) {
			runWebhook(c, logger, webhookConfig{name: "t", expectedGVK: &expected},
				func(context.Context, *admissionv1.AdmissionRequest) ([]string, error) { return nil, nil })
		})
		body, _ := json.Marshal(admissionv1.AdmissionReview{
			Request: &admissionv1.AdmissionRequest{
				UID: "1", Operation: admissionv1.Create,
				Kind: metav1.GroupVersionKind{Kind: "Service"},
			},
		})
		code, resp := postReview(engine, body)
		Expect(code).To(Equal(http.StatusOK))
		Expect(resp.Response.Allowed).To(BeFalse())
		Expect(resp.Response.Result.Message).To(ContainSubstring("Expected Pod"))
	})

	It("admits when the validate callback returns nil", func() {
		engine.POST("/webhook", func(c *gin.Context) {
			runWebhook(c, logger, webhookConfig{name: "t", requireNamespace: true},
				func(context.Context, *admissionv1.AdmissionRequest) ([]string, error) { return nil, nil })
		})
		body, _ := json.Marshal(admissionv1.AdmissionReview{
			Request: &admissionv1.AdmissionRequest{
				UID: "1", Operation: admissionv1.Create, Namespace: "ns",
			},
		})
		code, resp := postReview(engine, body)
		Expect(code).To(Equal(http.StatusOK))
		Expect(resp.Response.Allowed).To(BeTrue())
	})

	It("denies with 403 by default on validate error", func() {
		engine.POST("/webhook", func(c *gin.Context) {
			runWebhook(c, logger, webhookConfig{name: "t", requireNamespace: true},
				func(context.Context, *admissionv1.AdmissionRequest) ([]string, error) {
					return nil, newStatusErrorf(http.StatusForbidden, "no")
				})
		})
		body, _ := json.Marshal(admissionv1.AdmissionReview{
			Request: &admissionv1.AdmissionRequest{
				UID: "1", Operation: admissionv1.Create, Namespace: "ns",
			},
		})
		code, resp := postReview(engine, body)
		Expect(code).To(Equal(http.StatusOK))
		Expect(resp.Response.Allowed).To(BeFalse())
		Expect(resp.Response.Result.Code).To(Equal(int32(http.StatusForbidden)))
	})
})

var _ = Describe("decodeAdmissionObject", func() {
	It("returns a 400-coded statusError on bad bytes", func() {
		var pod corev1.Pod
		err := decodeAdmissionObject([]byte("not-json"), &pod, "Pod")
		Expect(err).To(HaveOccurred())
		se, ok := err.(*statusError)
		Expect(ok).To(BeTrue())
		Expect(se.code).To(Equal(http.StatusBadRequest))
	})
})

var _ = Describe("unsupportedOperationError", func() {
	It("returns 400 with a clear message", func() {
		err := unsupportedOperationError(admissionv1.Delete, "Pod")
		Expect(err).To(HaveOccurred())
		Expect(err.Error()).To(ContainSubstring("Operation DELETE is not supported"))
	})
})

var _ = Describe("validateAgainstCRQ (status-read path)", func() {
	var (
		ctx     context.Context
		logger  *zap.Logger
		nsLabel = map[string]string{"team": "alpha"}
	)
	const nsName = "vh-ns"

	BeforeEach(func() {
		ctx = context.Background()
		logger = zap.NewNop()
	})

	It("admits when crqClient is nil", func() {
		err := validateAgainstCRQ(ctx, nil, logger, nsName, corev1.ResourceCPU, quantity("1"))
		Expect(err).NotTo(HaveOccurred())
	})

	It("admits (fail-open) when namespace lookup fails", func() {
		// CRQ client exists but namespace is absent: Get returns NotFound.
		client := newTestCRQClient()
		err := validateAgainstCRQ(ctx, client, logger, "missing", corev1.ResourceCPU, quantity("1"))
		Expect(err).NotTo(HaveOccurred())
	})

	It("admits (fail-open) when CRQ list errors out", func() {
		ns := makeNamespace(nsName, nsLabel)
		client := newTestCRQClientWithListError(ns)
		err := validateAgainstCRQ(ctx, client, logger, nsName, corev1.ResourceCPU, quantity("1"))
		Expect(err).NotTo(HaveOccurred())
	})

	It("admits when no CRQ matches the namespace", func() {
		ns := makeNamespace(nsName, nsLabel)
		client := newTestCRQClient(ns)
		err := validateAgainstCRQ(ctx, client, logger, nsName, corev1.ResourceCPU, quantity("1"))
		Expect(err).NotTo(HaveOccurred())
	})

	It("admits when the CRQ has no hard limit for the resource", func() {
		ns := makeNamespace(nsName, nsLabel)
		crq := makeCRQ("crq", nsLabel,
			quotav1alpha1.ResourceList{corev1.ResourceMemory: quantity("1Gi")},
			quotav1alpha1.ResourceList{corev1.ResourceMemory: quantity("0")},
		)
		client := newTestCRQClient(ns, crq)
		err := validateAgainstCRQ(ctx, client, logger, nsName, corev1.ResourceCPU, quantity("1"))
		Expect(err).NotTo(HaveOccurred())
	})

	It("admits (fail-open) when the status has no usage value for the resource", func() {
		ns := makeNamespace(nsName, nsLabel)
		crq := makeCRQ("crq", nsLabel,
			quotav1alpha1.ResourceList{corev1.ResourceCPU: quantity("2")},
			nil,
		)
		client := newTestCRQClient(ns, crq)
		err := validateAgainstCRQ(ctx, client, logger, nsName, corev1.ResourceCPU, quantity("1"))
		Expect(err).NotTo(HaveOccurred())
	})

	It("denies when status.used + requested would exceed the hard limit", func() {
		ns := makeNamespace(nsName, nsLabel)
		crq := makeCRQ("crq-cpu", nsLabel,
			quotav1alpha1.ResourceList{corev1.ResourceCPU: quantity("2")},
			quotav1alpha1.ResourceList{corev1.ResourceCPU: quantity("2")},
		)
		client := newTestCRQClient(ns, crq)
		err := validateAgainstCRQ(ctx, client, logger, nsName, corev1.ResourceCPU, quantity("1"))
		Expect(err).To(HaveOccurred())
		Expect(err.Error()).To(ContainSubstring("ClusterResourceQuota 'crq-cpu' cpu limit exceeded"))
	})

	It("admits when status.used + requested stays within the hard limit", func() {
		ns := makeNamespace(nsName, nsLabel)
		crq := makeCRQ("crq-cpu", nsLabel,
			quotav1alpha1.ResourceList{corev1.ResourceCPU: quantity("5")},
			quotav1alpha1.ResourceList{corev1.ResourceCPU: quantity("2")},
		)
		client := newTestCRQClient(ns, crq)
		err := validateAgainstCRQ(ctx, client, logger, nsName, corev1.ResourceCPU, quantity("3"))
		Expect(err).NotTo(HaveOccurred())
	})

	It("admits when total exactly equals the hard limit", func() {
		ns := makeNamespace(nsName, nsLabel)
		crq := makeCRQ("crq-cpu", nsLabel,
			quotav1alpha1.ResourceList{corev1.ResourceCPU: quantity("5")},
			quotav1alpha1.ResourceList{corev1.ResourceCPU: quantity("4")},
		)
		client := newTestCRQClient(ns, crq)
		err := validateAgainstCRQ(ctx, client, logger, nsName, corev1.ResourceCPU, quantity("1"))
		Expect(err).NotTo(HaveOccurred())
	})
})

var _ = Describe("resolveCRQForNamespace", func() {
	var (
		ctx     context.Context
		logger  *zap.Logger
		nsLabel = map[string]string{"team": "alpha"}
	)
	const nsName = "vh-ns"

	BeforeEach(func() {
		ctx = context.Background()
		logger = zap.NewNop()
	})

	It("returns nil when client is nil", func() {
		crq := resolveCRQForNamespace(ctx, nil, logger, nsName)
		Expect(crq).To(BeNil())
	})

	It("returns nil (fail-open) when namespace cannot be fetched", func() {
		client := newTestCRQClient()
		crq := resolveCRQForNamespace(ctx, client, logger, "missing")
		Expect(crq).To(BeNil())
	})

	It("returns the matching CRQ when found", func() {
		ns := makeNamespace(nsName, nsLabel)
		want := makeCRQ("crq", nsLabel,
			quotav1alpha1.ResourceList{corev1.ResourceCPU: quantity("2")},
			quotav1alpha1.ResourceList{corev1.ResourceCPU: quantity("1")},
		)
		client := newTestCRQClient(ns, want)
		got := resolveCRQForNamespace(ctx, client, logger, nsName)
		Expect(got).NotTo(BeNil())
		Expect(got.Name).To(Equal("crq"))
	})
})

var _ = Describe("validateCRQStatusUsage", func() {
	logger := zap.NewNop()

	It("returns nil when the resource is not in spec.hard", func() {
		crq := makeCRQ("c", nil,
			quotav1alpha1.ResourceList{corev1.ResourceMemory: quantity("1Gi")},
			quotav1alpha1.ResourceList{corev1.ResourceMemory: quantity("0")},
		)
		Expect(validateCRQStatusUsage(crq, corev1.ResourceCPU, quantity("1"), logger, "")).To(Succeed())
	})

	It("returns nil (fail-open) when status is missing the resource", func() {
		crq := makeCRQ("c", nil,
			quotav1alpha1.ResourceList{corev1.ResourceCPU: quantity("2")},
			quotav1alpha1.ResourceList{corev1.ResourceMemory: quantity("0")},
		)
		Expect(validateCRQStatusUsage(crq, corev1.ResourceCPU, quantity("1"), logger, "")).To(Succeed())
	})

	It("returns an error when over the hard limit", func() {
		crq := makeCRQ("c", nil,
			quotav1alpha1.ResourceList{corev1.ResourceCPU: quantity("2")},
			quotav1alpha1.ResourceList{corev1.ResourceCPU: quantity("2")},
		)
		err := validateCRQStatusUsage(crq, corev1.ResourceCPU, quantity("1"), logger, "")
		Expect(err).To(HaveOccurred())
		Expect(err.Error()).To(ContainSubstring("limit exceeded"))
	})

	It("returns nil when zero usage + requested exactly equals limit", func() {
		crq := makeCRQ("c", nil,
			quotav1alpha1.ResourceList{corev1.ResourceCPU: quantity("1")},
			quotav1alpha1.ResourceList{corev1.ResourceCPU: *resource.NewQuantity(0, resource.DecimalSI)},
		)
		Expect(validateCRQStatusUsage(crq, corev1.ResourceCPU, quantity("1"), logger, "")).To(Succeed())
	})
})
