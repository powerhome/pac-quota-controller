package v1alpha1

import (
	"github.com/gin-gonic/gin"
	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	"go.uber.org/zap"
	admissionv1 "k8s.io/api/admission/v1"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"

	quotav1alpha1 "github.com/powerhome/pac-quota-controller/api/v1alpha1"
)

func newObjectCountReview(uid, namespace, resource, group string) *admissionv1.AdmissionReview {
	return &admissionv1.AdmissionReview{
		TypeMeta: metav1.TypeMeta{
			Kind:       "AdmissionReview",
			APIVersion: "admission.k8s.io/v1",
		},
		Request: &admissionv1.AdmissionRequest{
			UID:       types.UID(uid),
			Namespace: namespace,
			Operation: admissionv1.Create,
			Resource:  metav1.GroupVersionResource{Resource: resource, Group: group},
		},
	}
}

var _ = Describe("ObjectCountWebhook", func() {
	const (
		nsName  = "test-namespace"
		crqName = "objcount-crq"
	)
	var (
		engine *gin.Engine
		labels = map[string]string{"env": "test"}
	)

	BeforeEach(func() {
		gin.SetMode(gin.TestMode)
		engine = gin.New()
	})

	Describe("NewObjectCountWebhook", func() {
		It("constructs with all dependencies", func() {
			client := newTestCRQClient()
			h := NewObjectCountWebhook(client, zap.NewNop())
			Expect(h).NotTo(BeNil())
			Expect(h.crqClient).To(Equal(client))
		})

		It("substitutes a no-op logger when nil is passed", func() {
			h := NewObjectCountWebhook(nil, nil)
			Expect(h).NotTo(BeNil())
			Expect(h.logger).NotTo(BeNil())
		})
	})

	Describe("Handle (status-read path)", func() {
		It("admits configmap creation when under the quota", func() {
			ns := makeNamespace(nsName, labels)
			crq := makeCRQ(crqName, labels,
				quotav1alpha1.ResourceList{"configmaps": quantity("5")},
				quotav1alpha1.ResourceList{"configmaps": quantity("2")},
			)
			h := NewObjectCountWebhook(newTestCRQClient(ns, crq), zap.NewNop())
			engine.POST("/webhook", h.Handle)

			resp := sendWebhookRequest(engine, newObjectCountReview("1", nsName, "configmaps", ""))
			Expect(resp.Response.Allowed).To(BeTrue())
		})

		It("denies configmap creation when at the quota", func() {
			ns := makeNamespace(nsName, labels)
			crq := makeCRQ(crqName, labels,
				quotav1alpha1.ResourceList{"configmaps": quantity("2")},
				quotav1alpha1.ResourceList{"configmaps": quantity("2")},
			)
			h := NewObjectCountWebhook(newTestCRQClient(ns, crq), zap.NewNop())
			engine.POST("/webhook", h.Handle)

			resp := sendWebhookRequest(engine, newObjectCountReview("2", nsName, "configmaps", ""))
			Expect(resp.Response.Allowed).To(BeFalse())
			Expect(resp.Response.Result.Message).To(ContainSubstring("configmaps limit exceeded"))
		})

		It("uses '<resource>.<group>' as the CRQ key", func() {
			ns := makeNamespace(nsName, labels)
			crq := makeCRQ(crqName, labels,
				quotav1alpha1.ResourceList{"deployments.apps": quantity("1")},
				quotav1alpha1.ResourceList{"deployments.apps": quantity("1")},
			)
			h := NewObjectCountWebhook(newTestCRQClient(ns, crq), zap.NewNop())
			engine.POST("/webhook", h.Handle)

			resp := sendWebhookRequest(engine, newObjectCountReview("3", nsName, "deployments", "apps"))
			Expect(resp.Response.Allowed).To(BeFalse())
			Expect(resp.Response.Result.Message).To(ContainSubstring("deployments.apps limit exceeded"))
		})

		It("admits when the resource is not in the CRQ hard map", func() {
			ns := makeNamespace(nsName, labels)
			crq := makeCRQ(crqName, labels,
				quotav1alpha1.ResourceList{"configmaps": quantity("1")},
				quotav1alpha1.ResourceList{"configmaps": quantity("0")},
			)
			h := NewObjectCountWebhook(newTestCRQClient(ns, crq), zap.NewNop())
			engine.POST("/webhook", h.Handle)

			resp := sendWebhookRequest(engine, newObjectCountReview("4", nsName, "secrets", ""))
			Expect(resp.Response.Allowed).To(BeTrue())
		})

		It("admits when no CRQ matches the namespace", func() {
			ns := makeNamespace(nsName, labels)
			h := NewObjectCountWebhook(newTestCRQClient(ns), zap.NewNop())
			engine.POST("/webhook", h.Handle)

			resp := sendWebhookRequest(engine, newObjectCountReview("5", nsName, "configmaps", ""))
			Expect(resp.Response.Allowed).To(BeTrue())
		})

		It("admits when the CRQ client is nil", func() {
			h := NewObjectCountWebhook(nil, zap.NewNop())
			engine.POST("/webhook", h.Handle)

			resp := sendWebhookRequest(engine, newObjectCountReview("6", nsName, "configmaps", ""))
			Expect(resp.Response.Allowed).To(BeTrue())
		})

		It("denies when namespace is empty", func() {
			h := NewObjectCountWebhook(newTestCRQClient(), zap.NewNop())
			engine.POST("/webhook", h.Handle)

			resp := sendWebhookRequest(engine, newObjectCountReview("7", "", "configmaps", ""))
			Expect(resp.Response.Allowed).To(BeFalse())
			Expect(resp.Response.Result.Message).To(ContainSubstring("Namespace is required"))
		})

		It("admits DELETE operation as unsupported (returns 400)", func() {
			h := NewObjectCountWebhook(newTestCRQClient(), zap.NewNop())
			engine.POST("/webhook", h.Handle)

			review := newObjectCountReview("8", nsName, "configmaps", "")
			review.Request.Operation = admissionv1.Delete
			resp := sendWebhookRequest(engine, review)
			Expect(resp.Response.Allowed).To(BeFalse())
			Expect(resp.Response.Result.Message).To(ContainSubstring("not supported for ObjectCount"))
		})

		It("rejects UPDATE as unsupported (defensive seatbelt: chart only subscribes to CREATE)", func() {
			h := NewObjectCountWebhook(newTestCRQClient(), zap.NewNop())
			engine.POST("/webhook", h.Handle)

			review := newObjectCountReview("8u", nsName, "configmaps", "")
			review.Request.Operation = admissionv1.Update
			resp := sendWebhookRequest(engine, review)
			Expect(resp.Response.Allowed).To(BeFalse())
			Expect(resp.Response.Result.Code).To(Equal(int32(400)))
			Expect(resp.Response.Result.Message).To(ContainSubstring("Operation UPDATE is not supported for ObjectCount"))
		})

		It("admits (fail-open) when CRQ status is missing usage for the resource", func() {
			ns := makeNamespace(nsName, labels)
			crq := makeCRQ(crqName, labels,
				quotav1alpha1.ResourceList{"configmaps": quantity("2")},
				nil,
			)
			// Force compile-time use of corev1 import.
			_ = corev1.ResourceCPU
			h := NewObjectCountWebhook(newTestCRQClient(ns, crq), zap.NewNop())
			engine.POST("/webhook", h.Handle)

			resp := sendWebhookRequest(engine, newObjectCountReview("9", nsName, "configmaps", ""))
			Expect(resp.Response.Allowed).To(BeTrue())
		})
	})
})
