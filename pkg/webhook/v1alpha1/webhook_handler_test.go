package v1alpha1

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"

	"github.com/gin-gonic/gin"
	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	"go.uber.org/zap"
	admissionv1 "k8s.io/api/admission/v1"
	corev1 "k8s.io/api/core/v1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/api/resource"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/runtime/schema"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/client-go/kubernetes/fake"
	k8stesting "k8s.io/client-go/testing"
	ctrlclient "sigs.k8s.io/controller-runtime/pkg/client"
	ctrlclientfake "sigs.k8s.io/controller-runtime/pkg/client/fake"
	"sigs.k8s.io/controller-runtime/pkg/client/interceptor"

	quotav1alpha1 "github.com/powerhome/pac-quota-controller/api/v1alpha1"
	"github.com/powerhome/pac-quota-controller/pkg/kubernetes/quota"
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
		cfg    webhookConfig
	)

	BeforeEach(func() {
		gin.SetMode(gin.TestMode)
		engine = gin.New()
		logger = zap.NewNop()
		cfg = webhookConfig{
			name:             "test",
			expectedGVK:      &metav1.GroupVersionKind{Group: "", Version: "v1", Kind: "Pod"},
			requireNamespace: true,
		}
	})

	mount := func(v validateFn) {
		engine.POST("/webhook", func(c *gin.Context) { runWebhook(c, logger, cfg, v) })
	}

	It("returns 400 on malformed JSON body", func() {
		mount(func(context.Context, *admissionv1.AdmissionRequest) ([]string, error) { return nil, nil })
		code, _ := postReview(engine, []byte("{not-json"))
		Expect(code).To(Equal(http.StatusBadRequest))
	})

	It("returns 400 when AdmissionReview.Request is nil", func() {
		mount(func(context.Context, *admissionv1.AdmissionRequest) ([]string, error) { return nil, nil })
		body, _ := json.Marshal(admissionv1.AdmissionReview{})
		code, _ := postReview(engine, body)
		Expect(code).To(Equal(http.StatusBadRequest))
	})

	It("denies with templated namespace message when namespace required and empty", func() {
		cfg.name = "objectcount"
		mount(func(context.Context, *admissionv1.AdmissionRequest) ([]string, error) { return nil, nil })
		body, _ := json.Marshal(admissionv1.AdmissionReview{
			Request: &admissionv1.AdmissionRequest{
				UID:       types.UID("u1"),
				Operation: admissionv1.Create,
				Kind:      metav1.GroupVersionKind{Group: "", Version: "v1", Kind: "Pod"},
			},
		})
		code, resp := postReview(engine, body)
		Expect(code).To(Equal(http.StatusOK))
		Expect(resp.Response.Allowed).To(BeFalse())
		Expect(resp.Response.Result.Message).To(ContainSubstring("Namespace is required for objectcount validation"))
		Expect(resp.Response.Result.Code).To(Equal(int32(http.StatusBadRequest)))
	})

	It("denies when AdmissionRequest.Kind does not match expectedGVK", func() {
		mount(func(context.Context, *admissionv1.AdmissionRequest) ([]string, error) { return nil, nil })
		body, _ := json.Marshal(admissionv1.AdmissionReview{
			Request: &admissionv1.AdmissionRequest{
				UID:       types.UID("u2"),
				Namespace: "default",
				Operation: admissionv1.Create,
				Kind:      metav1.GroupVersionKind{Group: "", Version: "v1", Kind: "Service"},
			},
		})
		code, resp := postReview(engine, body)
		Expect(code).To(Equal(http.StatusOK))
		Expect(resp.Response.Allowed).To(BeFalse())
		Expect(resp.Response.Result.Message).To(ContainSubstring("Expected Pod resource, got Service"))
	})

	It("skips GVK check when expectedGVK is nil", func() {
		cfg.expectedGVK = nil
		mount(func(context.Context, *admissionv1.AdmissionRequest) ([]string, error) { return nil, nil })
		body, _ := json.Marshal(admissionv1.AdmissionReview{
			Request: &admissionv1.AdmissionRequest{
				UID:       types.UID("u3"),
				Namespace: "default",
				Operation: admissionv1.Create,
				Kind:      metav1.GroupVersionKind{Kind: "AnythingGoes"},
			},
		})
		code, resp := postReview(engine, body)
		Expect(code).To(Equal(http.StatusOK))
		Expect(resp.Response.Allowed).To(BeTrue())
	})

	It("allows cluster-scoped requests when requireNamespace is false", func() {
		cfg.requireNamespace = false
		mount(func(context.Context, *admissionv1.AdmissionRequest) ([]string, error) { return nil, nil })
		body, _ := json.Marshal(admissionv1.AdmissionReview{
			Request: &admissionv1.AdmissionRequest{
				UID:       types.UID("u4"),
				Operation: admissionv1.Create,
				Kind:      metav1.GroupVersionKind{Group: "", Version: "v1", Kind: "Pod"},
			},
		})
		code, resp := postReview(engine, body)
		Expect(code).To(Equal(http.StatusOK))
		Expect(resp.Response.Allowed).To(BeTrue())
	})

	It("uses 403 forbidden by default when validate returns a plain error", func() {
		mount(func(context.Context, *admissionv1.AdmissionRequest) ([]string, error) {
			return nil, errors.New("quota exceeded")
		})
		body, _ := json.Marshal(admissionv1.AdmissionReview{
			Request: &admissionv1.AdmissionRequest{
				UID: types.UID("u5"), Namespace: "default", Operation: admissionv1.Create,
				Kind: metav1.GroupVersionKind{Group: "", Version: "v1", Kind: "Pod"},
			},
		})
		_, resp := postReview(engine, body)
		Expect(resp.Response.Allowed).To(BeFalse())
		Expect(resp.Response.Result.Code).To(Equal(int32(http.StatusForbidden)))
		Expect(resp.Response.Result.Message).To(Equal("quota exceeded"))
	})

	It("honors statusError.code when validate returns one", func() {
		mount(func(context.Context, *admissionv1.AdmissionRequest) ([]string, error) {
			return nil, newStatusErrorf(http.StatusBadRequest, "bad %s", "thing")
		})
		body, _ := json.Marshal(admissionv1.AdmissionReview{
			Request: &admissionv1.AdmissionRequest{
				UID: types.UID("u6"), Namespace: "default", Operation: admissionv1.Create,
				Kind: metav1.GroupVersionKind{Group: "", Version: "v1", Kind: "Pod"},
			},
		})
		_, resp := postReview(engine, body)
		Expect(resp.Response.Result.Code).To(Equal(int32(http.StatusBadRequest)))
		Expect(resp.Response.Result.Message).To(Equal("bad thing"))
	})

	It("attaches warnings on success when validate returns them", func() {
		mount(func(context.Context, *admissionv1.AdmissionRequest) ([]string, error) {
			return []string{"watch out"}, nil
		})
		body, _ := json.Marshal(admissionv1.AdmissionReview{
			Request: &admissionv1.AdmissionRequest{
				UID: types.UID("u7"), Namespace: "default", Operation: admissionv1.Create,
				Kind: metav1.GroupVersionKind{Group: "", Version: "v1", Kind: "Pod"},
			},
		})
		_, resp := postReview(engine, body)
		Expect(resp.Response.Allowed).To(BeTrue())
		Expect(resp.Response.Warnings).To(Equal([]string{"watch out"}))
	})

	It("propagates the AdmissionRequest UID to the response", func() {
		mount(func(context.Context, *admissionv1.AdmissionRequest) ([]string, error) { return nil, nil })
		body, _ := json.Marshal(admissionv1.AdmissionReview{
			Request: &admissionv1.AdmissionRequest{
				UID: types.UID("uid-echo"), Namespace: "default", Operation: admissionv1.Create,
				Kind: metav1.GroupVersionKind{Group: "", Version: "v1", Kind: "Pod"},
			},
		})
		_, resp := postReview(engine, body)
		Expect(resp.Response.UID).To(Equal(types.UID("uid-echo")))
	})
})

var _ = Describe("decodeAdmissionObject", func() {
	It("returns a 400 statusError when raw bytes are invalid", func() {
		var pod corev1.Pod
		err := decodeAdmissionObject([]byte("not-json"), &pod, "Pod")
		Expect(err).To(HaveOccurred())
		se, ok := err.(*statusError)
		Expect(ok).To(BeTrue())
		Expect(se.code).To(Equal(http.StatusBadRequest))
		Expect(se.Error()).To(ContainSubstring("Unable to decode Pod object"))
	})

	It("populates the target object on valid input", func() {
		raw, _ := json.Marshal(&corev1.Pod{ObjectMeta: metav1.ObjectMeta{Name: "p"}})
		var pod corev1.Pod
		Expect(decodeAdmissionObject(raw, &pod, "Pod")).To(Succeed())
		Expect(pod.Name).To(Equal("p"))
	})
})

var _ = Describe("unsupportedOperationError", func() {
	It("renders a 400 statusError mentioning op and resource", func() {
		err := unsupportedOperationError(admissionv1.Delete, "Pod")
		se, ok := err.(*statusError)
		Expect(ok).To(BeTrue())
		Expect(se.code).To(Equal(http.StatusBadRequest))
		Expect(se.Error()).To(Equal("Operation DELETE is not supported for Pod"))
	})
})

var _ = Describe("validateAgainstCRQ", func() {
	var (
		ctx        context.Context
		fakeClient *fake.Clientset
		logger     *zap.Logger
		crqClient  *quota.CRQClient
		ns         *corev1.Namespace
	)

	const nsName = "vh-ns"

	BeforeEach(func() {
		ctx = context.Background()
		logger = zap.NewNop()
		ns = &corev1.Namespace{
			ObjectMeta: metav1.ObjectMeta{
				Name:   nsName,
				Labels: map[string]string{"team": "alpha"},
			},
		}
		fakeClient = fake.NewSimpleClientset(ns)
	})

	newCRQClient := func(objs ...runtime.Object) *quota.CRQClient {
		scheme := runtime.NewScheme()
		_ = quotav1alpha1.AddToScheme(scheme)
		_ = corev1.AddToScheme(scheme)
		cl := ctrlclientfake.NewClientBuilder().WithScheme(scheme).WithRuntimeObjects(objs...).Build()
		return quota.NewCRQClient(cl, logger)
	}

	calc := func(_ context.Context, _ string, _ corev1.ResourceName) (resource.Quantity, error) {
		return *resource.NewQuantity(0, resource.DecimalSI), nil
	}

	It("returns nil when crqClient is nil (validation skipped)", func() {
		err := validateAgainstCRQ(ctx, fakeClient, nil, logger,
			nsName, corev1.ResourceCPU, resource.MustParse("1"), calc)
		Expect(err).NotTo(HaveOccurred())
	})

	It("returns an error when the namespace lookup fails", func() {
		crqClient = newCRQClient()
		err := validateAgainstCRQ(ctx, fakeClient, crqClient, logger,
			"missing", corev1.ResourceCPU, resource.MustParse("1"), calc)
		Expect(err).To(HaveOccurred())
		Expect(err.Error()).To(ContainSubstring("failed to get namespace missing"))
	})

	It("swallows CRQ-lookup errors and allows the operation", func() {
		// Use a CRQ client whose underlying List call returns an error.
		scheme := runtime.NewScheme()
		_ = quotav1alpha1.AddToScheme(scheme)
		_ = corev1.AddToScheme(scheme)
		cl := ctrlclientfake.NewClientBuilder().
			WithScheme(scheme).
			WithInterceptorFuncs(interceptListError()).
			Build()
		crqClient = quota.NewCRQClient(cl, logger)

		err := validateAgainstCRQ(ctx, fakeClient, crqClient, logger,
			nsName, corev1.ResourceCPU, resource.MustParse("1"), calc)
		Expect(err).NotTo(HaveOccurred())
	})

	It("allows when no CRQ applies to the namespace", func() {
		crqClient = newCRQClient() // no CRQ objects
		err := validateAgainstCRQ(ctx, fakeClient, crqClient, logger,
			nsName, corev1.ResourceCPU, resource.MustParse("1"), calc)
		Expect(err).NotTo(HaveOccurred())
	})

	It("allows when CRQ matches but the resource has no hard limit", func() {
		crq := &quotav1alpha1.ClusterResourceQuota{
			ObjectMeta: metav1.ObjectMeta{Name: "crq-other"},
			Spec: quotav1alpha1.ClusterResourceQuotaSpec{
				NamespaceSelector: &metav1.LabelSelector{
					MatchLabels: map[string]string{"team": "alpha"},
				},
				Hard: quotav1alpha1.ResourceList{
					corev1.ResourceMemory: resource.MustParse("1Gi"),
				},
			},
		}
		crqClient = newCRQClient(crq)
		err := validateAgainstCRQ(ctx, fakeClient, crqClient, logger,
			nsName, corev1.ResourceCPU, resource.MustParse("1"), calc)
		Expect(err).NotTo(HaveOccurred())
	})

	It("denies when total usage exceeds the hard quota limit", func() {
		crq := &quotav1alpha1.ClusterResourceQuota{
			ObjectMeta: metav1.ObjectMeta{Name: "crq-cpu"},
			Spec: quotav1alpha1.ClusterResourceQuotaSpec{
				NamespaceSelector: &metav1.LabelSelector{
					MatchLabels: map[string]string{"team": "alpha"},
				},
				Hard: quotav1alpha1.ResourceList{
					corev1.ResourceCPU: resource.MustParse("2"),
				},
			},
		}
		crqClient = newCRQClient(crq)
		usage := func(_ context.Context, _ string, _ corev1.ResourceName) (resource.Quantity, error) {
			return resource.MustParse("2"), nil
		}
		err := validateAgainstCRQ(ctx, fakeClient, crqClient, logger,
			nsName, corev1.ResourceCPU, resource.MustParse("1"), usage)
		Expect(err).To(HaveOccurred())
		Expect(err.Error()).To(ContainSubstring("ClusterResourceQuota 'crq-cpu' cpu limit exceeded"))
	})

	It("allows when total usage stays at or under the hard limit", func() {
		crq := &quotav1alpha1.ClusterResourceQuota{
			ObjectMeta: metav1.ObjectMeta{Name: "crq-cpu"},
			Spec: quotav1alpha1.ClusterResourceQuotaSpec{
				NamespaceSelector: &metav1.LabelSelector{
					MatchLabels: map[string]string{"team": "alpha"},
				},
				Hard: quotav1alpha1.ResourceList{
					corev1.ResourceCPU: resource.MustParse("5"),
				},
			},
		}
		crqClient = newCRQClient(crq)
		usage := func(_ context.Context, _ string, _ corev1.ResourceName) (resource.Quantity, error) {
			return resource.MustParse("2"), nil
		}
		err := validateAgainstCRQ(ctx, fakeClient, crqClient, logger,
			nsName, corev1.ResourceCPU, resource.MustParse("1"), usage)
		Expect(err).NotTo(HaveOccurred())
	})

	It("wraps the underlying error when the usage calculator fails", func() {
		crq := &quotav1alpha1.ClusterResourceQuota{
			ObjectMeta: metav1.ObjectMeta{Name: "crq-cpu"},
			Spec: quotav1alpha1.ClusterResourceQuotaSpec{
				NamespaceSelector: &metav1.LabelSelector{
					MatchLabels: map[string]string{"team": "alpha"},
				},
				Hard: quotav1alpha1.ResourceList{
					corev1.ResourceCPU: resource.MustParse("5"),
				},
			},
		}
		crqClient = newCRQClient(crq)
		failing := func(_ context.Context, _ string, _ corev1.ResourceName) (resource.Quantity, error) {
			return resource.Quantity{}, errors.New("boom")
		}
		err := validateAgainstCRQ(ctx, fakeClient, crqClient, logger,
			nsName, corev1.ResourceCPU, resource.MustParse("1"), failing)
		Expect(err).To(HaveOccurred())
		Expect(err.Error()).To(ContainSubstring("failed to calculate current usage"))
		Expect(err.Error()).To(ContainSubstring("boom"))
	})
})

var _ = Describe("calculateCRQCurrentUsage", func() {
	var (
		ctx    context.Context
		logger *zap.Logger
	)

	BeforeEach(func() {
		ctx = context.Background()
		logger = zap.NewNop()
	})

	It("returns 0 when the CRQ selector matches no namespaces", func() {
		fakeClient := fake.NewSimpleClientset()
		crq := &quotav1alpha1.ClusterResourceQuota{
			ObjectMeta: metav1.ObjectMeta{Name: "crq"},
			Spec: quotav1alpha1.ClusterResourceQuotaSpec{
				NamespaceSelector: &metav1.LabelSelector{MatchLabels: map[string]string{"k": "v"}},
			},
		}
		total, err := calculateCRQCurrentUsage(ctx, fakeClient, crq, corev1.ResourceCPU,
			func(context.Context, string, corev1.ResourceName) (resource.Quantity, error) {
				return resource.MustParse("99"), nil
			}, logger)
		Expect(err).NotTo(HaveOccurred())
		Expect(total.Value()).To(BeZero())
	})

	It("sums usage across every selected namespace", func() {
		fakeClient := fake.NewSimpleClientset(
			&corev1.Namespace{ObjectMeta: metav1.ObjectMeta{Name: "a", Labels: map[string]string{"t": "x"}}},
			&corev1.Namespace{ObjectMeta: metav1.ObjectMeta{Name: "b", Labels: map[string]string{"t": "x"}}},
		)
		crq := &quotav1alpha1.ClusterResourceQuota{
			ObjectMeta: metav1.ObjectMeta{Name: "crq"},
			Spec: quotav1alpha1.ClusterResourceQuotaSpec{
				NamespaceSelector: &metav1.LabelSelector{MatchLabels: map[string]string{"t": "x"}},
			},
		}
		total, err := calculateCRQCurrentUsage(ctx, fakeClient, crq, corev1.ResourceCPU,
			func(_ context.Context, _ string, _ corev1.ResourceName) (resource.Quantity, error) {
				return resource.MustParse("3"), nil
			}, logger)
		Expect(err).NotTo(HaveOccurred())
		Expect(total.String()).To(Equal("6"))
	})

	It("returns an error if any per-namespace calculator call fails", func() {
		fakeClient := fake.NewSimpleClientset(
			&corev1.Namespace{ObjectMeta: metav1.ObjectMeta{Name: "a", Labels: map[string]string{"t": "x"}}},
		)
		crq := &quotav1alpha1.ClusterResourceQuota{
			ObjectMeta: metav1.ObjectMeta{Name: "crq"},
			Spec: quotav1alpha1.ClusterResourceQuotaSpec{
				NamespaceSelector: &metav1.LabelSelector{MatchLabels: map[string]string{"t": "x"}},
			},
		}
		_, err := calculateCRQCurrentUsage(ctx, fakeClient, crq, corev1.ResourceCPU,
			func(context.Context, string, corev1.ResourceName) (resource.Quantity, error) {
				return resource.Quantity{}, errors.New("calc boom")
			}, logger)
		Expect(err).To(HaveOccurred())
		Expect(err.Error()).To(ContainSubstring("failed to calculate usage for namespace a"))
	})

	It("returns an error when the namespace selector lookup itself fails", func() {
		fakeClient := fake.NewSimpleClientset()
		fakeClient.PrependReactor("list", "namespaces",
			func(k8stesting.Action) (bool, runtime.Object, error) {
				return true, nil, errors.New("list boom")
			})
		crq := &quotav1alpha1.ClusterResourceQuota{
			ObjectMeta: metav1.ObjectMeta{Name: "crq"},
			Spec: quotav1alpha1.ClusterResourceQuotaSpec{
				NamespaceSelector: &metav1.LabelSelector{MatchLabels: map[string]string{"t": "x"}},
			},
		}
		_, err := calculateCRQCurrentUsage(ctx, fakeClient, crq, corev1.ResourceCPU,
			func(context.Context, string, corev1.ResourceName) (resource.Quantity, error) {
				return resource.Quantity{}, nil
			}, logger)
		Expect(err).To(HaveOccurred())
		Expect(err.Error()).To(ContainSubstring("failed to get namespaces matching CRQ selector"))
	})
})

// interceptListError causes any controller-runtime List call on the fake
// client to fail, exercising the error branch in CRQClient.GetCRQByNamespace.
func interceptListError() interceptor.Funcs {
	return interceptor.Funcs{
		List: func(_ context.Context, _ ctrlclient.WithWatch, _ ctrlclient.ObjectList, _ ...ctrlclient.ListOption) error {
			return apierrors.NewServerTimeout(schema.GroupResource{Resource: "clusterresourcequotas"}, "list", 1)
		},
	}
}
