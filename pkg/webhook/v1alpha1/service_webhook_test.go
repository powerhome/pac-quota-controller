package v1alpha1

import (
	"encoding/json"

	"github.com/gin-gonic/gin"
	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	"go.uber.org/zap"
	admissionv1 "k8s.io/api/admission/v1"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/types"

	quotav1alpha1 "github.com/powerhome/pac-quota-controller/api/v1alpha1"
	"github.com/powerhome/pac-quota-controller/pkg/kubernetes/usage"
)

const serviceWebhookTestNamespace = "svc-ns"

func newServiceReview(uid string, svc *corev1.Service) *admissionv1.AdmissionReview {
	raw, _ := json.Marshal(svc)
	return &admissionv1.AdmissionReview{
		TypeMeta: metav1.TypeMeta{
			Kind:       "AdmissionReview",
			APIVersion: "admission.k8s.io/v1",
		},
		Request: &admissionv1.AdmissionRequest{
			UID:       types.UID(uid),
			Namespace: serviceWebhookTestNamespace,
			Operation: admissionv1.Create,
			Kind:      metav1.GroupVersionKind{Group: "", Version: "v1", Kind: "Service"},
			Resource:  metav1.GroupVersionResource{Group: "", Version: "v1", Resource: "services"},
			Object:    runtime.RawExtension{Raw: raw},
		},
	}
}

func makeService(svcType corev1.ServiceType) *corev1.Service {
	return &corev1.Service{
		ObjectMeta: metav1.ObjectMeta{Name: "s1", Namespace: serviceWebhookTestNamespace},
		Spec:       corev1.ServiceSpec{Type: svcType},
	}
}

var _ = Describe("ServiceWebhook", func() {
	const (
		nsName  = serviceWebhookTestNamespace
		crqName = "svc-crq"
	)
	var (
		engine *gin.Engine
		labels = map[string]string{"team": "alpha"}
	)

	BeforeEach(func() {
		gin.SetMode(gin.TestMode)
		engine = gin.New()
	})

	Describe("NewServiceWebhook", func() {
		It("constructs with all dependencies", func() {
			client := newTestCRQClient()
			h := NewServiceWebhook(client, zap.NewNop())
			Expect(h).NotTo(BeNil())
			Expect(h.crqClient).To(Equal(client))
		})

		It("uses a no-op logger when nil is passed", func() {
			h := NewServiceWebhook(nil, nil)
			Expect(h).NotTo(BeNil())
			Expect(h.logger).NotTo(BeNil())
		})
	})

	Describe("Handle (status-read path)", func() {
		It("admits a ClusterIP service when under the services quota", func() {
			ns := makeNamespace(nsName, labels)
			crq := makeCRQ(crqName, labels,
				quotav1alpha1.ResourceList{usage.ResourceServices: quantity("5")},
				quotav1alpha1.ResourceList{usage.ResourceServices: quantity("2")},
			)
			h := NewServiceWebhook(newTestCRQClient(ns, crq), zap.NewNop())
			engine.POST("/webhook", h.Handle)

			resp := sendWebhookRequest(engine,
				newServiceReview("1", makeService(corev1.ServiceTypeClusterIP)))
			Expect(resp.Response.Allowed).To(BeTrue())
		})

		It("denies a ClusterIP service when at the services quota", func() {
			ns := makeNamespace(nsName, labels)
			crq := makeCRQ(crqName, labels,
				quotav1alpha1.ResourceList{usage.ResourceServices: quantity("2")},
				quotav1alpha1.ResourceList{usage.ResourceServices: quantity("2")},
			)
			h := NewServiceWebhook(newTestCRQClient(ns, crq), zap.NewNop())
			engine.POST("/webhook", h.Handle)

			resp := sendWebhookRequest(engine,
				newServiceReview("2", makeService(corev1.ServiceTypeClusterIP)))
			Expect(resp.Response.Allowed).To(BeFalse())
			Expect(resp.Response.Result.Message).To(ContainSubstring("services limit exceeded"))
		})

		It("denies a LoadBalancer service when over the LB quota", func() {
			ns := makeNamespace(nsName, labels)
			crq := makeCRQ(crqName, labels,
				quotav1alpha1.ResourceList{
					usage.ResourceServices:              quantity("10"),
					usage.ResourceServicesLoadBalancers: quantity("1"),
				},
				quotav1alpha1.ResourceList{
					usage.ResourceServices:              quantity("0"),
					usage.ResourceServicesLoadBalancers: quantity("1"),
				},
			)
			h := NewServiceWebhook(newTestCRQClient(ns, crq), zap.NewNop())
			engine.POST("/webhook", h.Handle)

			resp := sendWebhookRequest(engine,
				newServiceReview("3", makeService(corev1.ServiceTypeLoadBalancer)))
			Expect(resp.Response.Allowed).To(BeFalse())
			Expect(resp.Response.Result.Message).To(ContainSubstring("services.loadbalancers limit exceeded"))
		})

		It("denies a NodePort service when over the NodePort quota", func() {
			ns := makeNamespace(nsName, labels)
			crq := makeCRQ(crqName, labels,
				quotav1alpha1.ResourceList{
					usage.ResourceServices:          quantity("10"),
					usage.ResourceServicesNodePorts: quantity("0"),
				},
				quotav1alpha1.ResourceList{
					usage.ResourceServices:          quantity("0"),
					usage.ResourceServicesNodePorts: quantity("0"),
				},
			)
			h := NewServiceWebhook(newTestCRQClient(ns, crq), zap.NewNop())
			engine.POST("/webhook", h.Handle)

			resp := sendWebhookRequest(engine,
				newServiceReview("4", makeService(corev1.ServiceTypeNodePort)))
			Expect(resp.Response.Allowed).To(BeFalse())
			Expect(resp.Response.Result.Message).To(ContainSubstring("services.nodeports limit exceeded"))
		})

		It("does not check subtype quotas for a ClusterIP service", func() {
			ns := makeNamespace(nsName, labels)
			crq := makeCRQ(crqName, labels,
				quotav1alpha1.ResourceList{
					usage.ResourceServices:              quantity("10"),
					usage.ResourceServicesLoadBalancers: quantity("0"),
				},
				quotav1alpha1.ResourceList{
					usage.ResourceServices:              quantity("0"),
					usage.ResourceServicesLoadBalancers: quantity("0"),
				},
			)
			h := NewServiceWebhook(newTestCRQClient(ns, crq), zap.NewNop())
			engine.POST("/webhook", h.Handle)

			resp := sendWebhookRequest(engine,
				newServiceReview("5", makeService(corev1.ServiceTypeClusterIP)))
			Expect(resp.Response.Allowed).To(BeTrue())
		})

		It("admits when no CRQ matches the namespace", func() {
			ns := makeNamespace(nsName, labels)
			h := NewServiceWebhook(newTestCRQClient(ns), zap.NewNop())
			engine.POST("/webhook", h.Handle)

			resp := sendWebhookRequest(engine,
				newServiceReview("6", makeService(corev1.ServiceTypeClusterIP)))
			Expect(resp.Response.Allowed).To(BeTrue())
		})

		It("admits when the CRQ client is nil", func() {
			h := NewServiceWebhook(nil, zap.NewNop())
			engine.POST("/webhook", h.Handle)

			resp := sendWebhookRequest(engine,
				newServiceReview("7", makeService(corev1.ServiceTypeClusterIP)))
			Expect(resp.Response.Allowed).To(BeTrue())
		})

		It("rejects DELETE as unsupported", func() {
			h := NewServiceWebhook(newTestCRQClient(), zap.NewNop())
			engine.POST("/webhook", h.Handle)

			review := newServiceReview("8", makeService(corev1.ServiceTypeClusterIP))
			review.Request.Operation = admissionv1.Delete
			resp := sendWebhookRequest(engine, review)
			Expect(resp.Response.Allowed).To(BeFalse())
			Expect(resp.Response.Result.Message).To(ContainSubstring("Operation DELETE is not supported"))
		})

		It("denies a non-Service GVK", func() {
			h := NewServiceWebhook(newTestCRQClient(), zap.NewNop())
			engine.POST("/webhook", h.Handle)

			review := newServiceReview("9", makeService(corev1.ServiceTypeClusterIP))
			review.Request.Kind = metav1.GroupVersionKind{Kind: "Pod"}
			resp := sendWebhookRequest(engine, review)
			Expect(resp.Response.Allowed).To(BeFalse())
			Expect(resp.Response.Result.Message).To(ContainSubstring("Expected Service"))
		})
	})

	Describe("Handle UPDATE", func() {
		updateReview := func(uid string, newSvc, oldSvc *corev1.Service) *admissionv1.AdmissionReview {
			r := newServiceReview(uid, newSvc)
			r.Request.Operation = admissionv1.Update
			oldRaw, _ := json.Marshal(oldSvc)
			r.Request.OldObject = runtime.RawExtension{Raw: oldRaw}
			return r
		}

		It("allows updating a service when services count is at the limit", func() {
			ns := makeNamespace(nsName, labels)
			crq := makeCRQ(crqName, labels,
				quotav1alpha1.ResourceList{usage.ResourceServices: quantity("2")},
				quotav1alpha1.ResourceList{usage.ResourceServices: quantity("2")},
			)
			h := NewServiceWebhook(newTestCRQClient(ns, crq), zap.NewNop())
			engine.POST("/webhook", h.Handle)

			old := makeService(corev1.ServiceTypeClusterIP)
			new := makeService(corev1.ServiceTypeClusterIP)
			resp := sendWebhookRequest(engine, updateReview("u1", new, old))
			Expect(resp.Response.Allowed).To(BeTrue())
		})

		It("allows updating a LoadBalancer when LB quota is at the limit and type is unchanged", func() {
			ns := makeNamespace(nsName, labels)
			crq := makeCRQ(crqName, labels,
				quotav1alpha1.ResourceList{
					usage.ResourceServices:              quantity("10"),
					usage.ResourceServicesLoadBalancers: quantity("1"),
				},
				quotav1alpha1.ResourceList{
					usage.ResourceServices:              quantity("1"),
					usage.ResourceServicesLoadBalancers: quantity("1"),
				},
			)
			h := NewServiceWebhook(newTestCRQClient(ns, crq), zap.NewNop())
			engine.POST("/webhook", h.Handle)

			old := makeService(corev1.ServiceTypeLoadBalancer)
			new := makeService(corev1.ServiceTypeLoadBalancer)
			resp := sendWebhookRequest(engine, updateReview("u2", new, old))
			Expect(resp.Response.Allowed).To(BeTrue())
		})

		It("denies a ClusterIP -> LoadBalancer transition when LB quota is full", func() {
			ns := makeNamespace(nsName, labels)
			crq := makeCRQ(crqName, labels,
				quotav1alpha1.ResourceList{
					usage.ResourceServices:              quantity("10"),
					usage.ResourceServicesLoadBalancers: quantity("1"),
				},
				quotav1alpha1.ResourceList{
					usage.ResourceServices:              quantity("1"),
					usage.ResourceServicesLoadBalancers: quantity("1"),
				},
			)
			h := NewServiceWebhook(newTestCRQClient(ns, crq), zap.NewNop())
			engine.POST("/webhook", h.Handle)

			old := makeService(corev1.ServiceTypeClusterIP)
			new := makeService(corev1.ServiceTypeLoadBalancer)
			resp := sendWebhookRequest(engine, updateReview("u3", new, old))
			Expect(resp.Response.Allowed).To(BeFalse())
			Expect(resp.Response.Result.Message).To(ContainSubstring("services.loadbalancers limit exceeded"))
		})

		It("allows a LoadBalancer -> ClusterIP transition even when LB quota is full", func() {
			ns := makeNamespace(nsName, labels)
			crq := makeCRQ(crqName, labels,
				quotav1alpha1.ResourceList{
					usage.ResourceServices:              quantity("10"),
					usage.ResourceServicesLoadBalancers: quantity("1"),
				},
				quotav1alpha1.ResourceList{
					usage.ResourceServices:              quantity("1"),
					usage.ResourceServicesLoadBalancers: quantity("1"),
				},
			)
			h := NewServiceWebhook(newTestCRQClient(ns, crq), zap.NewNop())
			engine.POST("/webhook", h.Handle)

			old := makeService(corev1.ServiceTypeLoadBalancer)
			new := makeService(corev1.ServiceTypeClusterIP)
			resp := sendWebhookRequest(engine, updateReview("u4", new, old))
			Expect(resp.Response.Allowed).To(BeTrue())
		})

		It("denies a NodePort -> LoadBalancer transition when LB quota is full", func() {
			ns := makeNamespace(nsName, labels)
			crq := makeCRQ(crqName, labels,
				quotav1alpha1.ResourceList{
					usage.ResourceServices:              quantity("10"),
					usage.ResourceServicesLoadBalancers: quantity("1"),
					usage.ResourceServicesNodePorts:     quantity("5"),
				},
				quotav1alpha1.ResourceList{
					usage.ResourceServices:              quantity("1"),
					usage.ResourceServicesLoadBalancers: quantity("1"),
					usage.ResourceServicesNodePorts:     quantity("0"),
				},
			)
			h := NewServiceWebhook(newTestCRQClient(ns, crq), zap.NewNop())
			engine.POST("/webhook", h.Handle)

			old := makeService(corev1.ServiceTypeNodePort)
			new := makeService(corev1.ServiceTypeLoadBalancer)
			resp := sendWebhookRequest(engine, updateReview("u5", new, old))
			Expect(resp.Response.Allowed).To(BeFalse())
			Expect(resp.Response.Result.Message).To(ContainSubstring("services.loadbalancers limit exceeded"))
		})
	})
})
