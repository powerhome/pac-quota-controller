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
	"github.com/powerhome/pac-quota-controller/pkg/kubernetes/storage"
	"github.com/powerhome/pac-quota-controller/pkg/kubernetes/usage"
)

const pvcWebhookTestNamespace = "pvc-ns"

func newPVCReview(uid string, pvc *corev1.PersistentVolumeClaim) *admissionv1.AdmissionReview {
	raw, _ := json.Marshal(pvc)
	return &admissionv1.AdmissionReview{
		TypeMeta: metav1.TypeMeta{
			Kind:       "AdmissionReview",
			APIVersion: "admission.k8s.io/v1",
		},
		Request: &admissionv1.AdmissionRequest{
			UID:       types.UID(uid),
			Namespace: pvcWebhookTestNamespace,
			Operation: admissionv1.Create,
			Kind:      metav1.GroupVersionKind{Group: "", Version: "v1", Kind: "PersistentVolumeClaim"},
			Resource: metav1.GroupVersionResource{
				Group: "", Version: "v1", Resource: "persistentvolumeclaims",
			},
			Object: runtime.RawExtension{Raw: raw},
		},
	}
}

func makePVC(name, storageReq, storageClass string) *corev1.PersistentVolumeClaim {
	pvc := &corev1.PersistentVolumeClaim{
		ObjectMeta: metav1.ObjectMeta{Name: name, Namespace: pvcWebhookTestNamespace},
	}
	if storageReq != "" {
		pvc.Spec.Resources.Requests = corev1.ResourceList{
			corev1.ResourceStorage: resource.MustParse(storageReq),
		}
	}
	if storageClass != "" {
		pvc.Spec.StorageClassName = &storageClass
	}
	return pvc
}

var _ = Describe("PersistentVolumeClaimWebhook", func() {
	const (
		nsName  = pvcWebhookTestNamespace
		crqName = "pvc-crq"
	)
	var (
		engine *gin.Engine
		labels = map[string]string{"team": "alpha"}
	)

	BeforeEach(func() {
		gin.SetMode(gin.TestMode)
		engine = gin.New()
	})

	Describe("NewPersistentVolumeClaimWebhook", func() {
		It("constructs with all dependencies", func() {
			client := newTestCRQClient()
			h := NewPersistentVolumeClaimWebhook(client, zap.NewNop())
			Expect(h).NotTo(BeNil())
			Expect(h.crqClient).To(Equal(client))
		})

		It("uses a no-op logger when nil is passed", func() {
			h := NewPersistentVolumeClaimWebhook(nil, nil)
			Expect(h).NotTo(BeNil())
			Expect(h.logger).NotTo(BeNil())
		})
	})

	Describe("storage.GetPVCStorageRequest", func() {
		It("returns the storage value when present", func() {
			pvc := makePVC("p", "10Gi", "")
			q := storage.GetPVCStorageRequest(pvc)
			Expect(q.Cmp(resource.MustParse("10Gi"))).To(Equal(0))
		})

		It("returns the zero quantity when no requests are set", func() {
			pvc := &corev1.PersistentVolumeClaim{}
			q := storage.GetPVCStorageRequest(pvc)
			Expect(q.IsZero()).To(BeTrue())
		})
	})

	Describe("Handle (status-read path)", func() {
		It("admits a PVC when storage is under the quota", func() {
			ns := makeNamespace(nsName, labels)
			crq := makeCRQ(crqName, labels,
				quotav1alpha1.ResourceList{
					usage.ResourceRequestsStorage:        quantity("100Gi"),
					usage.ResourcePersistentVolumeClaims: quantity("10"),
				},
				quotav1alpha1.ResourceList{
					usage.ResourceRequestsStorage:        quantity("10Gi"),
					usage.ResourcePersistentVolumeClaims: quantity("1"),
				},
			)
			h := NewPersistentVolumeClaimWebhook(newTestCRQClient(ns, crq), zap.NewNop())
			engine.POST("/webhook", h.Handle)

			resp := sendWebhookRequest(engine, newPVCReview("1", makePVC("p1", "5Gi", "")))
			Expect(resp.Response.Allowed).To(BeTrue())
		})

		It("denies a PVC when storage would exceed the quota", func() {
			ns := makeNamespace(nsName, labels)
			crq := makeCRQ(crqName, labels,
				quotav1alpha1.ResourceList{
					usage.ResourceRequestsStorage:        quantity("10Gi"),
					usage.ResourcePersistentVolumeClaims: quantity("10"),
				},
				quotav1alpha1.ResourceList{
					usage.ResourceRequestsStorage:        quantity("10Gi"),
					usage.ResourcePersistentVolumeClaims: quantity("0"),
				},
			)
			h := NewPersistentVolumeClaimWebhook(newTestCRQClient(ns, crq), zap.NewNop())
			engine.POST("/webhook", h.Handle)

			resp := sendWebhookRequest(engine, newPVCReview("2", makePVC("p1", "1Gi", "")))
			Expect(resp.Response.Allowed).To(BeFalse())
			Expect(resp.Response.Result.Message).To(ContainSubstring("requests.storage limit exceeded"))
		})

		It("denies a PVC when the PVC count would exceed the quota", func() {
			ns := makeNamespace(nsName, labels)
			crq := makeCRQ(crqName, labels,
				quotav1alpha1.ResourceList{
					usage.ResourcePersistentVolumeClaims: quantity("2"),
				},
				quotav1alpha1.ResourceList{
					usage.ResourcePersistentVolumeClaims: quantity("2"),
				},
			)
			h := NewPersistentVolumeClaimWebhook(newTestCRQClient(ns, crq), zap.NewNop())
			engine.POST("/webhook", h.Handle)

			resp := sendWebhookRequest(engine, newPVCReview("3", makePVC("p1", "", "")))
			Expect(resp.Response.Allowed).To(BeFalse())
			Expect(resp.Response.Result.Message).To(ContainSubstring("persistentvolumeclaims limit exceeded"))
		})

		It("validates storage-class-prefixed keys when StorageClassName is set", func() {
			ns := makeNamespace(nsName, labels)
			scStorageKey := corev1.ResourceName("fast.storageclass.storage.k8s.io/requests.storage")
			scPVCCountKey := corev1.ResourceName("fast.storageclass.storage.k8s.io/persistentvolumeclaims")
			crq := makeCRQ(crqName, labels,
				quotav1alpha1.ResourceList{
					usage.ResourceRequestsStorage:        quantity("100Gi"),
					usage.ResourcePersistentVolumeClaims: quantity("10"),
					scStorageKey:                         quantity("5Gi"),
					scPVCCountKey:                        quantity("10"),
				},
				quotav1alpha1.ResourceList{
					usage.ResourceRequestsStorage:        quantity("0"),
					usage.ResourcePersistentVolumeClaims: quantity("0"),
					scStorageKey:                         quantity("5Gi"),
					scPVCCountKey:                        quantity("0"),
				},
			)
			h := NewPersistentVolumeClaimWebhook(newTestCRQClient(ns, crq), zap.NewNop())
			engine.POST("/webhook", h.Handle)

			resp := sendWebhookRequest(engine, newPVCReview("4", makePVC("p1", "1Gi", "fast")))
			Expect(resp.Response.Allowed).To(BeFalse())
			Expect(resp.Response.Result.Message).To(ContainSubstring("storage class 'fast' storage validation failed"))
		})

		It("admits when no CRQ matches the namespace", func() {
			ns := makeNamespace(nsName, labels)
			h := NewPersistentVolumeClaimWebhook(newTestCRQClient(ns), zap.NewNop())
			engine.POST("/webhook", h.Handle)

			resp := sendWebhookRequest(engine, newPVCReview("5", makePVC("p1", "10Gi", "")))
			Expect(resp.Response.Allowed).To(BeTrue())
		})

		It("admits when the CRQ client is nil", func() {
			h := NewPersistentVolumeClaimWebhook(nil, zap.NewNop())
			engine.POST("/webhook", h.Handle)

			resp := sendWebhookRequest(engine, newPVCReview("6", makePVC("p1", "10Gi", "")))
			Expect(resp.Response.Allowed).To(BeTrue())
		})

		It("rejects DELETE as unsupported", func() {
			h := NewPersistentVolumeClaimWebhook(newTestCRQClient(), zap.NewNop())
			engine.POST("/webhook", h.Handle)

			review := newPVCReview("7", makePVC("p1", "10Gi", ""))
			review.Request.Operation = admissionv1.Delete
			resp := sendWebhookRequest(engine, review)
			Expect(resp.Response.Allowed).To(BeFalse())
			Expect(resp.Response.Result.Message).To(ContainSubstring("Operation DELETE is not supported"))
		})

		It("admits an Update when storage grows within quota (resize)", func() {
			ns := makeNamespace(nsName, labels)
			crq := makeCRQ(crqName, labels,
				quotav1alpha1.ResourceList{
					usage.ResourceRequestsStorage:        quantity("10Gi"),
					usage.ResourcePersistentVolumeClaims: quantity("10"),
				},
				quotav1alpha1.ResourceList{
					usage.ResourceRequestsStorage:        quantity("5Gi"),
					usage.ResourcePersistentVolumeClaims: quantity("1"),
				},
			)
			h := NewPersistentVolumeClaimWebhook(newTestCRQClient(ns, crq), zap.NewNop())
			engine.POST("/webhook", h.Handle)

			review := newPVCReview("8", makePVC("p1", "6Gi", ""))
			oldRaw, _ := json.Marshal(makePVC("p1", "5Gi", ""))
			review.Request.OldObject = runtime.RawExtension{Raw: oldRaw}
			review.Request.Operation = admissionv1.Update

			resp := sendWebhookRequest(engine, review)
			Expect(resp.Response.Allowed).To(BeTrue())
		})

		It("denies an Update when storage growth would exceed quota", func() {
			ns := makeNamespace(nsName, labels)
			crq := makeCRQ(crqName, labels,
				quotav1alpha1.ResourceList{
					usage.ResourceRequestsStorage:        quantity("8Gi"),
					usage.ResourcePersistentVolumeClaims: quantity("10"),
				},
				quotav1alpha1.ResourceList{
					usage.ResourceRequestsStorage:        quantity("5Gi"),
					usage.ResourcePersistentVolumeClaims: quantity("1"),
				},
			)
			h := NewPersistentVolumeClaimWebhook(newTestCRQClient(ns, crq), zap.NewNop())
			engine.POST("/webhook", h.Handle)

			review := newPVCReview("9", makePVC("p1", "10Gi", ""))
			oldRaw, _ := json.Marshal(makePVC("p1", "5Gi", ""))
			review.Request.OldObject = runtime.RawExtension{Raw: oldRaw}
			review.Request.Operation = admissionv1.Update

			resp := sendWebhookRequest(engine, review)
			Expect(resp.Response.Allowed).To(BeFalse())
			Expect(resp.Response.Result.Message).To(ContainSubstring("requests.storage limit exceeded"))
		})

		It("admits an Update that does not change storage (no-op for quota)", func() {
			ns := makeNamespace(nsName, labels)
			crq := makeCRQ(crqName, labels,
				quotav1alpha1.ResourceList{
					usage.ResourceRequestsStorage:        quantity("5Gi"),
					usage.ResourcePersistentVolumeClaims: quantity("1"),
				},
				quotav1alpha1.ResourceList{
					usage.ResourceRequestsStorage:        quantity("5Gi"),
					usage.ResourcePersistentVolumeClaims: quantity("1"),
				},
			)
			h := NewPersistentVolumeClaimWebhook(newTestCRQClient(ns, crq), zap.NewNop())
			engine.POST("/webhook", h.Handle)

			review := newPVCReview("10", makePVC("p1", "5Gi", ""))
			oldRaw, _ := json.Marshal(makePVC("p1", "5Gi", ""))
			review.Request.OldObject = runtime.RawExtension{Raw: oldRaw}
			review.Request.Operation = admissionv1.Update

			resp := sendWebhookRequest(engine, review)
			Expect(resp.Response.Allowed).To(BeTrue())
		})
	})
})
