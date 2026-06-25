package v1alpha1

import (
	"encoding/json"

	"github.com/gin-gonic/gin"
	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	"go.uber.org/zap"
	admissionv1 "k8s.io/api/admission/v1"
	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/resource"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/types"

	quotav1alpha1 "github.com/powerhome/pac-quota-controller/api/v1alpha1"
	"github.com/powerhome/pac-quota-controller/pkg/kubernetes/usage"
)

const podWebhookTestNamespace = "pod-ns"

func newPodReview(uid string, pod *corev1.Pod) *admissionv1.AdmissionReview {
	raw, _ := json.Marshal(pod)
	return &admissionv1.AdmissionReview{
		TypeMeta: metav1.TypeMeta{
			Kind:       "AdmissionReview",
			APIVersion: "admission.k8s.io/v1",
		},
		Request: &admissionv1.AdmissionRequest{
			UID:       types.UID(uid),
			Namespace: podWebhookTestNamespace,
			Operation: admissionv1.Create,
			Kind:      metav1.GroupVersionKind{Group: "", Version: "v1", Kind: "Pod"},
			Resource:  metav1.GroupVersionResource{Group: "", Version: "v1", Resource: "pods"},
			Object:    runtime.RawExtension{Raw: raw},
		},
	}
}

func makePod(name, cpuReq, memReq, cpuLim, memLim string) *corev1.Pod {
	requests := corev1.ResourceList{}
	limits := corev1.ResourceList{}
	if cpuReq != "" {
		requests[corev1.ResourceCPU] = resource.MustParse(cpuReq)
	}
	if memReq != "" {
		requests[corev1.ResourceMemory] = resource.MustParse(memReq)
	}
	if cpuLim != "" {
		limits[corev1.ResourceCPU] = resource.MustParse(cpuLim)
	}
	if memLim != "" {
		limits[corev1.ResourceMemory] = resource.MustParse(memLim)
	}
	return &corev1.Pod{
		ObjectMeta: metav1.ObjectMeta{Name: name, Namespace: podWebhookTestNamespace},
		Spec: corev1.PodSpec{
			Containers: []corev1.Container{
				{
					Name:  "c",
					Image: "busybox",
					Resources: corev1.ResourceRequirements{
						Requests: requests,
						Limits:   limits,
					},
				},
			},
		},
	}
}

func makeEphemeralPod(name, ephemeralReq, ephemeralLim string) *corev1.Pod {
	requests := corev1.ResourceList{}
	limits := corev1.ResourceList{}
	if ephemeralReq != "" {
		requests[corev1.ResourceEphemeralStorage] = resource.MustParse(ephemeralReq)
	}
	if ephemeralLim != "" {
		limits[corev1.ResourceEphemeralStorage] = resource.MustParse(ephemeralLim)
	}
	return &corev1.Pod{
		ObjectMeta: metav1.ObjectMeta{Name: name, Namespace: podWebhookTestNamespace},
		Spec: corev1.PodSpec{
			Containers: []corev1.Container{
				{
					Name:  "c",
					Image: "busybox",
					Resources: corev1.ResourceRequirements{
						Requests: requests,
						Limits:   limits,
					},
				},
			},
		},
	}
}

var _ = Describe("PodWebhook", func() {
	const (
		nsName  = podWebhookTestNamespace
		crqName = "pod-crq"
	)
	var (
		engine *gin.Engine
		labels = map[string]string{"team": "alpha"}
	)

	BeforeEach(func() {
		gin.SetMode(gin.TestMode)
		engine = gin.New()
	})

	Describe("NewPodWebhook", func() {
		It("constructs with all dependencies", func() {
			client := newTestCRQClient()
			h := NewPodWebhook(client, zap.NewNop())
			Expect(h).NotTo(BeNil())
			Expect(h.crqClient).To(Equal(client))
		})

		It("uses a no-op logger when nil is passed", func() {
			h := NewPodWebhook(nil, nil)
			Expect(h).NotTo(BeNil())
			Expect(h.logger).NotTo(BeNil())
		})
	})

	Describe("Handle (status-read path)", func() {
		It("admits a pod when all resources stay under the quota", func() {
			ns := makeNamespace(nsName, labels)
			crq := makeCRQ(crqName, labels,
				quotav1alpha1.ResourceList{
					usage.ResourceRequestsCPU:    quantity("4"),
					usage.ResourceRequestsMemory: quantity("4Gi"),
					usage.ResourceLimitsCPU:      quantity("8"),
					usage.ResourceLimitsMemory:   quantity("8Gi"),
					usage.ResourcePods:           quantity("10"),
				},
				quotav1alpha1.ResourceList{
					usage.ResourceRequestsCPU:    quantity("1"),
					usage.ResourceRequestsMemory: quantity("1Gi"),
					usage.ResourceLimitsCPU:      quantity("2"),
					usage.ResourceLimitsMemory:   quantity("2Gi"),
					usage.ResourcePods:           quantity("2"),
				},
			)
			h := NewPodWebhook(newTestCRQClient(ns, crq), zap.NewNop())
			engine.POST("/webhook", h.Handle)

			pod := makePod("p1", "1", "1Gi", "2", "2Gi")
			resp := sendWebhookRequest(engine, newPodReview("1", pod))
			Expect(resp.Response.Allowed).To(BeTrue())
		})

		It("denies when CPU requests would exceed the quota", func() {
			ns := makeNamespace(nsName, labels)
			crq := makeCRQ(crqName, labels,
				quotav1alpha1.ResourceList{
					usage.ResourceRequestsCPU: quantity("2"),
					usage.ResourcePods:        quantity("10"),
				},
				quotav1alpha1.ResourceList{
					usage.ResourceRequestsCPU: quantity("2"),
					usage.ResourcePods:        quantity("0"),
				},
			)
			h := NewPodWebhook(newTestCRQClient(ns, crq), zap.NewNop())
			engine.POST("/webhook", h.Handle)

			pod := makePod("p1", "1", "", "", "")
			resp := sendWebhookRequest(engine, newPodReview("2", pod))
			Expect(resp.Response.Allowed).To(BeFalse())
			Expect(resp.Response.Result.Message).To(ContainSubstring("CPU requests"))
			Expect(resp.Response.Result.Message).To(ContainSubstring("requests.cpu limit exceeded"))
		})

		It("denies when memory requests would exceed the quota", func() {
			ns := makeNamespace(nsName, labels)
			crq := makeCRQ(crqName, labels,
				quotav1alpha1.ResourceList{
					usage.ResourceRequestsMemory: quantity("1Gi"),
					usage.ResourcePods:           quantity("10"),
				},
				quotav1alpha1.ResourceList{
					usage.ResourceRequestsMemory: quantity("1Gi"),
					usage.ResourcePods:           quantity("0"),
				},
			)
			h := NewPodWebhook(newTestCRQClient(ns, crq), zap.NewNop())
			engine.POST("/webhook", h.Handle)

			pod := makePod("p1", "", "512Mi", "", "")
			resp := sendWebhookRequest(engine, newPodReview("3", pod))
			Expect(resp.Response.Allowed).To(BeFalse())
			Expect(resp.Response.Result.Message).To(ContainSubstring("memory requests"))
		})

		It("denies when CPU limits would exceed the quota", func() {
			ns := makeNamespace(nsName, labels)
			crq := makeCRQ(crqName, labels,
				quotav1alpha1.ResourceList{
					usage.ResourceLimitsCPU: quantity("2"),
					usage.ResourcePods:      quantity("10"),
				},
				quotav1alpha1.ResourceList{
					usage.ResourceLimitsCPU: quantity("2"),
					usage.ResourcePods:      quantity("0"),
				},
			)
			h := NewPodWebhook(newTestCRQClient(ns, crq), zap.NewNop())
			engine.POST("/webhook", h.Handle)

			pod := makePod("p1", "", "", "1", "")
			resp := sendWebhookRequest(engine, newPodReview("4", pod))
			Expect(resp.Response.Allowed).To(BeFalse())
			Expect(resp.Response.Result.Message).To(ContainSubstring("CPU limits"))
		})

		It("denies when memory limits would exceed the quota", func() {
			ns := makeNamespace(nsName, labels)
			crq := makeCRQ(crqName, labels,
				quotav1alpha1.ResourceList{
					usage.ResourceLimitsMemory: quantity("1Gi"),
					usage.ResourcePods:         quantity("10"),
				},
				quotav1alpha1.ResourceList{
					usage.ResourceLimitsMemory: quantity("1Gi"),
					usage.ResourcePods:         quantity("0"),
				},
			)
			h := NewPodWebhook(newTestCRQClient(ns, crq), zap.NewNop())
			engine.POST("/webhook", h.Handle)

			pod := makePod("p1", "", "", "", "256Mi")
			resp := sendWebhookRequest(engine, newPodReview("5", pod))
			Expect(resp.Response.Allowed).To(BeFalse())
			Expect(resp.Response.Result.Message).To(ContainSubstring("memory limits"))
		})

		It("denies when ephemeral-storage requests would exceed the quota", func() {
			ns := makeNamespace(nsName, labels)
			crq := makeCRQ(crqName, labels,
				quotav1alpha1.ResourceList{
					usage.ResourceRequestsEphemeralStorage: quantity("2Gi"),
					usage.ResourcePods:                     quantity("10"),
				},
				quotav1alpha1.ResourceList{
					usage.ResourceRequestsEphemeralStorage: quantity("2Gi"),
					usage.ResourcePods:                     quantity("0"),
				},
			)
			h := NewPodWebhook(newTestCRQClient(ns, crq), zap.NewNop())
			engine.POST("/webhook", h.Handle)

			pod := makeEphemeralPod("p1", "1Gi", "")
			resp := sendWebhookRequest(engine, newPodReview("6", pod))
			Expect(resp.Response.Allowed).To(BeFalse())
			Expect(resp.Response.Result.Message).To(ContainSubstring("ephemeral-storage requests"))
		})

		It("denies when ephemeral-storage limits would exceed the quota", func() {
			ns := makeNamespace(nsName, labels)
			crq := makeCRQ(crqName, labels,
				quotav1alpha1.ResourceList{
					usage.ResourceLimitsEphemeralStorage: quantity("2Gi"),
					usage.ResourcePods:                   quantity("10"),
				},
				quotav1alpha1.ResourceList{
					usage.ResourceLimitsEphemeralStorage: quantity("2Gi"),
					usage.ResourcePods:                   quantity("0"),
				},
			)
			h := NewPodWebhook(newTestCRQClient(ns, crq), zap.NewNop())
			engine.POST("/webhook", h.Handle)

			pod := makeEphemeralPod("p1", "", "1Gi")
			resp := sendWebhookRequest(engine, newPodReview("7", pod))
			Expect(resp.Response.Allowed).To(BeFalse())
			Expect(resp.Response.Result.Message).To(ContainSubstring("ephemeral-storage limits"))
		})

		It("admits a pod when ephemeral-storage stays under the quota", func() {
			ns := makeNamespace(nsName, labels)
			crq := makeCRQ(crqName, labels,
				quotav1alpha1.ResourceList{
					usage.ResourceLimitsEphemeralStorage: quantity("4Gi"),
					usage.ResourcePods:                   quantity("10"),
				},
				quotav1alpha1.ResourceList{
					usage.ResourceLimitsEphemeralStorage: quantity("1Gi"),
					usage.ResourcePods:                   quantity("1"),
				},
			)
			h := NewPodWebhook(newTestCRQClient(ns, crq), zap.NewNop())
			engine.POST("/webhook", h.Handle)

			pod := makeEphemeralPod("p1", "", "2Gi")
			resp := sendWebhookRequest(engine, newPodReview("8", pod))
			Expect(resp.Response.Allowed).To(BeTrue())
		})

		It("denies when the pod count would exceed the quota even with no resource requests", func() {
			ns := makeNamespace(nsName, labels)
			crq := makeCRQ(crqName, labels,
				quotav1alpha1.ResourceList{usage.ResourcePods: quantity("2")},
				quotav1alpha1.ResourceList{usage.ResourcePods: quantity("2")},
			)
			h := NewPodWebhook(newTestCRQClient(ns, crq), zap.NewNop())
			engine.POST("/webhook", h.Handle)

			pod := makePod("p1", "", "", "", "")
			resp := sendWebhookRequest(engine, newPodReview("6", pod))
			Expect(resp.Response.Allowed).To(BeFalse())
			Expect(resp.Response.Result.Message).To(ContainSubstring("pod count"))
			Expect(resp.Response.Result.Message).To(ContainSubstring("pods limit exceeded"))
		})

		It("admits when no CRQ matches the namespace", func() {
			ns := makeNamespace(nsName, labels)
			h := NewPodWebhook(newTestCRQClient(ns), zap.NewNop())
			engine.POST("/webhook", h.Handle)

			pod := makePod("p1", "1", "1Gi", "", "")
			resp := sendWebhookRequest(engine, newPodReview("7", pod))
			Expect(resp.Response.Allowed).To(BeTrue())
		})

		It("admits when the CRQ client is nil", func() {
			h := NewPodWebhook(nil, zap.NewNop())
			engine.POST("/webhook", h.Handle)

			pod := makePod("p1", "1", "1Gi", "", "")
			resp := sendWebhookRequest(engine, newPodReview("8", pod))
			Expect(resp.Response.Allowed).To(BeTrue())
		})

		It("denies non-Pod GVK", func() {
			h := NewPodWebhook(newTestCRQClient(), zap.NewNop())
			engine.POST("/webhook", h.Handle)

			review := newPodReview("9", makePod("p", "", "", "", ""))
			review.Request.Kind = metav1.GroupVersionKind{Kind: "Service"}
			resp := sendWebhookRequest(engine, review)
			Expect(resp.Response.Allowed).To(BeFalse())
			Expect(resp.Response.Result.Message).To(ContainSubstring("Expected Pod"))
		})

		It("rejects DELETE as unsupported", func() {
			h := NewPodWebhook(newTestCRQClient(), zap.NewNop())
			engine.POST("/webhook", h.Handle)

			review := newPodReview("10", makePod("p", "", "", "", ""))
			review.Request.Operation = admissionv1.Delete
			resp := sendWebhookRequest(engine, review)
			Expect(resp.Response.Allowed).To(BeFalse())
			Expect(resp.Response.Result.Message).To(ContainSubstring("Operation DELETE is not supported"))
		})

		It("admits (fail-open) when CRQ status is missing the requested resource", func() {
			ns := makeNamespace(nsName, labels)
			crq := makeCRQ(crqName, labels,
				quotav1alpha1.ResourceList{
					usage.ResourceRequestsCPU: quantity("2"),
					usage.ResourcePods:        quantity("10"),
				},
				// Status only populates pods; cpu is missing.
				quotav1alpha1.ResourceList{usage.ResourcePods: quantity("0")},
			)
			h := NewPodWebhook(newTestCRQClient(ns, crq), zap.NewNop())
			engine.POST("/webhook", h.Handle)

			pod := makePod("p1", "5", "", "", "")
			resp := sendWebhookRequest(engine, newPodReview("11", pod))
			Expect(resp.Response.Allowed).To(BeTrue())
		})
	})

	Describe("Pod Resize (UPDATE) Quota Validation", func() {
		// resizeReview builds a review matching what the apiserver sends for the
		// pods/resize subresource: Operation=UPDATE, SubResource="resize", and
		// OldObject populated with the pre-resize pod.
		resizeReview := func(uid string, newPod, oldPod *corev1.Pod) *admissionv1.AdmissionReview {
			r := newPodReview(uid, newPod)
			r.Request.Operation = admissionv1.Update
			r.Request.SubResource = "resize"
			oldRaw, _ := json.Marshal(oldPod)
			r.Request.OldObject = runtime.RawExtension{Raw: oldRaw}
			return r
		}

		It("allows a resize up when the delta fits in remaining quota", func() {
			ns := makeNamespace(nsName, labels)
			crq := makeCRQ(crqName, labels,
				quotav1alpha1.ResourceList{
					usage.ResourceRequestsCPU: quantity("1"),
					usage.ResourcePods:        quantity("10"),
				},
				// Existing pod uses 50m; remaining = 950m.
				quotav1alpha1.ResourceList{
					usage.ResourceRequestsCPU: quantity("50m"),
					usage.ResourcePods:        quantity("1"),
				},
			)
			h := NewPodWebhook(newTestCRQClient(ns, crq), zap.NewNop())
			engine.POST("/webhook", h.Handle)

			oldPod := makePod("p1", "50m", "", "", "")
			newPod := makePod("p1", "60m", "", "", "")
			resp := sendWebhookRequest(engine, resizeReview("r1", newPod, oldPod))
			Expect(resp.Response.Allowed).To(BeTrue())
		})

		It("rejects a resize up when the delta exceeds remaining quota", func() {
			ns := makeNamespace(nsName, labels)
			crq := makeCRQ(crqName, labels,
				quotav1alpha1.ResourceList{
					usage.ResourceRequestsCPU: quantity("100m"),
					usage.ResourcePods:        quantity("10"),
				},
				// Quota fully consumed by the existing pod.
				quotav1alpha1.ResourceList{
					usage.ResourceRequestsCPU: quantity("100m"),
					usage.ResourcePods:        quantity("1"),
				},
			)
			h := NewPodWebhook(newTestCRQClient(ns, crq), zap.NewNop())
			engine.POST("/webhook", h.Handle)

			oldPod := makePod("p1", "100m", "", "", "")
			newPod := makePod("p1", "200m", "", "", "")
			resp := sendWebhookRequest(engine, resizeReview("r2", newPod, oldPod))
			Expect(resp.Response.Allowed).To(BeFalse())
			Expect(resp.Response.Result.Message).To(ContainSubstring("requests.cpu limit exceeded"))
		})

		It("allows a resize up that would fail without delta accounting", func() {
			ns := makeNamespace(nsName, labels)
			crq := makeCRQ(crqName, labels,
				quotav1alpha1.ResourceList{
					usage.ResourceRequestsCPU: quantity("1"),
					usage.ResourcePods:        quantity("10"),
				},
				// 900m already used; without delta accounting, charging the full
				// new 200m would exceed (900m + 200m = 1100m > 1).
				quotav1alpha1.ResourceList{
					usage.ResourceRequestsCPU: quantity("900m"),
					usage.ResourcePods:        quantity("1"),
				},
			)
			h := NewPodWebhook(newTestCRQClient(ns, crq), zap.NewNop())
			engine.POST("/webhook", h.Handle)

			oldPod := makePod("p1", "100m", "", "", "")
			newPod := makePod("p1", "200m", "", "", "")
			// Delta is 100m; 900m + 100m = 1 (fits exactly).
			resp := sendWebhookRequest(engine, resizeReview("r3", newPod, oldPod))
			Expect(resp.Response.Allowed).To(BeTrue())
		})

		It("allows a resize down even when quota is fully consumed", func() {
			ns := makeNamespace(nsName, labels)
			crq := makeCRQ(crqName, labels,
				quotav1alpha1.ResourceList{
					usage.ResourceRequestsCPU: quantity("100m"),
					usage.ResourcePods:        quantity("10"),
				},
				quotav1alpha1.ResourceList{
					usage.ResourceRequestsCPU: quantity("100m"),
					usage.ResourcePods:        quantity("1"),
				},
			)
			h := NewPodWebhook(newTestCRQClient(ns, crq), zap.NewNop())
			engine.POST("/webhook", h.Handle)

			oldPod := makePod("p1", "100m", "", "", "")
			newPod := makePod("p1", "50m", "", "", "")
			resp := sendWebhookRequest(engine, resizeReview("r4", newPod, oldPod))
			Expect(resp.Response.Allowed).To(BeTrue())
		})

		It("does not charge +1 pod count on UPDATE (resize fits with pod count at limit)", func() {
			ns := makeNamespace(nsName, labels)
			crq := makeCRQ(crqName, labels,
				quotav1alpha1.ResourceList{
					usage.ResourceRequestsCPU: quantity("1"),
					usage.ResourcePods:        quantity("1"),
				},
				// Pod count already at the hard limit; an UPDATE must not be
				// rejected by a +1 pod-count charge (only CREATE charges count).
				quotav1alpha1.ResourceList{
					usage.ResourceRequestsCPU: quantity("50m"),
					usage.ResourcePods:        quantity("1"),
				},
			)
			h := NewPodWebhook(newTestCRQClient(ns, crq), zap.NewNop())
			engine.POST("/webhook", h.Handle)

			oldPod := makePod("p1", "50m", "", "", "")
			newPod := makePod("p1", "60m", "", "", "")
			resp := sendWebhookRequest(engine, resizeReview("r5", newPod, oldPod))
			Expect(resp.Response.Allowed).To(BeTrue())
		})
	})
})
