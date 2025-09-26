package v1alpha1

import (
	"bytes"
	"context"
	"encoding/json"
	"net/http/httptest"

	"github.com/gin-gonic/gin"
	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	"go.uber.org/zap"
	admissionv1 "k8s.io/api/admission/v1"
	appsv1 "k8s.io/api/apps/v1"
	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/resource"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/client-go/kubernetes/fake"
	ctrlclientfake "sigs.k8s.io/controller-runtime/pkg/client/fake"

	quotav1alpha1 "github.com/powerhome/pac-quota-controller/api/v1alpha1"
	"github.com/powerhome/pac-quota-controller/pkg/kubernetes/quota"
)

var _ = Describe("ObjectCountWebhook", func() {
	var (
		webhook    *ObjectCountWebhook
		fakeClient *fake.Clientset
		crqClient  *quota.CRQClient
		logger     *zap.Logger
		ginEngine  *gin.Engine
		scheme     *runtime.Scheme
		nsName     string
	)

	BeforeEach(func() {
		scheme = runtime.NewScheme()
		_ = quotav1alpha1.AddToScheme(scheme)
		_ = corev1.AddToScheme(scheme)
		_ = appsv1.AddToScheme(scheme)
		fakeClient = fake.NewSimpleClientset()
		logger = zap.NewNop()
		gin.SetMode(gin.TestMode)
		ginEngine = gin.New()
		nsName = "test-namespace"
	})

	AfterEach(func() {
		// No-op for now, but can be used for cleanup
	})

	Describe("NewObjectCountWebhook", func() {
		It("should create a new object count webhook", func() {
			fakeClient = fake.NewSimpleClientset()
			logger = zap.NewNop()
			crqClient = quota.NewCRQClient(nil)
			webhook = NewObjectCountWebhook(fakeClient, crqClient, logger)
			Expect(webhook).NotTo(BeNil())
			Expect(webhook.client).To(Equal(fakeClient))
			Expect(webhook.log).To(Equal(logger))
			Expect(webhook.crqClient).To(Equal(crqClient))
		})

		It("should create webhook with nil client", func() {
			logger = zap.NewNop()
			crqClient = quota.NewCRQClient(nil)
			webhook = NewObjectCountWebhook(nil, crqClient, logger)
			Expect(webhook).NotTo(BeNil())
			Expect(webhook.client).To(BeNil())
		})

		It("should create webhook with nil logger", func() {
			fakeClient = fake.NewSimpleClientset()
			crqClient = quota.NewCRQClient(nil)
			webhook = NewObjectCountWebhook(fakeClient, crqClient, nil)
			Expect(webhook).NotTo(BeNil())
			Expect(webhook.log).To(BeNil())
		})

		It("should create webhook with nil CRQ client", func() {
			fakeClient = fake.NewSimpleClientset()
			logger = zap.NewNop()
			webhook = NewObjectCountWebhook(fakeClient, nil, logger)
			Expect(webhook).NotTo(BeNil())
			Expect(webhook.crqClient).To(BeNil())
		})

		It("should create webhook with all nil parameters", func() {
			webhook = NewObjectCountWebhook(nil, nil, nil)
			Expect(webhook).NotTo(BeNil())
			Expect(webhook.client).To(BeNil())
			Expect(webhook.crqClient).To(BeNil())
			Expect(webhook.log).To(BeNil())
		})
	})

	Describe("Handle AdmissionRequest", func() {
		Context("with CRQ and configmaps", func() {
			BeforeEach(func() {
				crq := &quotav1alpha1.ClusterResourceQuota{
					ObjectMeta: metav1.ObjectMeta{Name: "crq"},
					Spec: quotav1alpha1.ClusterResourceQuotaSpec{
						NamespaceSelector: &metav1.LabelSelector{MatchLabels: map[string]string{"env": "test"}},
						Hard: quotav1alpha1.ResourceList{
							"configmaps":                           resource.MustParse("2"),
							"secrets":                              resource.MustParse("1"),
							"replicationcontrollers":               resource.MustParse("1"),
							"deployments.apps":                     resource.MustParse("1"),
							"statefulsets.apps":                    resource.MustParse("1"),
							"daemonsets.apps":                      resource.MustParse("1"),
							"jobs.batch":                           resource.MustParse("1"),
							"cronjobs.batch":                       resource.MustParse("1"),
							"horizontalpodautoscalers.autoscaling": resource.MustParse("1"),
							"ingresses.networking.k8s.io":          resource.MustParse("1"),
						},
					},
				}
				ns := &corev1.Namespace{
					ObjectMeta: metav1.ObjectMeta{Name: nsName, Labels: map[string]string{"env": "test"}},
				}
				_, _ = fakeClient.CoreV1().Namespaces().Create(context.Background(), ns, metav1.CreateOptions{})
				fakeRuntimeClient := ctrlclientfake.NewClientBuilder().
					WithScheme(scheme).
					WithObjects(crq, ns).Build()
				crqClient = quota.NewCRQClient(fakeRuntimeClient)
				webhook = NewObjectCountWebhook(fakeClient, crqClient, logger)
				ginEngine.POST("/webhook", webhook.Handle)
			})

			It("should allow creation when under quota", func() {
				_, _ = fakeClient.CoreV1().ConfigMaps(nsName).Create(
					context.Background(),
					&corev1.ConfigMap{
						ObjectMeta: metav1.ObjectMeta{
							Name:      "cm1",
							Namespace: nsName,
						},
					},
					metav1.CreateOptions{},
				)
				review := createObjectCountAdmissionReview("123", nsName, "configmaps")
				body, _ := json.Marshal(review)
				req := httptest.NewRequest("POST", "/webhook", bytes.NewReader(body))
				w := httptest.NewRecorder()
				ginEngine.ServeHTTP(w, req)
				Expect(w.Code).To(Equal(200))
				var resp admissionv1.AdmissionReview
				_ = json.Unmarshal(w.Body.Bytes(), &resp)
				Expect(resp.Response.Allowed).To(BeTrue())
			})

			It("should deny creation when quota exceeded", func() {
				// Add 2 configmaps to reach quota
				_, err := fakeClient.CoreV1().ConfigMaps(nsName).Create(
					context.Background(),
					&corev1.ConfigMap{
						ObjectMeta: metav1.ObjectMeta{
							Name:      "cm1",
							Namespace: nsName,
						},
					}, metav1.CreateOptions{},
				)
				Expect(err).ToNot(HaveOccurred())
				_, err = fakeClient.CoreV1().ConfigMaps(nsName).Create(
					context.Background(),
					&corev1.ConfigMap{
						ObjectMeta: metav1.ObjectMeta{
							Name:      "cm2",
							Namespace: nsName,
						},
					},
					metav1.CreateOptions{},
				)
				Expect(err).ToNot(HaveOccurred())
				review := createObjectCountAdmissionReview("456", nsName, "configmaps")
				body, _ := json.Marshal(review)
				req := httptest.NewRequest("POST", "/webhook", bytes.NewReader(body))
				w := httptest.NewRecorder()
				ginEngine.ServeHTTP(w, req)
				Expect(w.Code).To(Equal(200))
				var resp admissionv1.AdmissionReview
				_ = json.Unmarshal(w.Body.Bytes(), &resp)
				Expect(resp.Response.Allowed).To(BeFalse())
				Expect(resp.Response.Result.Message).To(ContainSubstring("ClusterResourceQuota"))
				Expect(resp.Response.Result.Message).To(ContainSubstring("configmaps limit exceeded"))
			})

			It("should allow creation with multiple objects under quota", func() {
				// Add 1 configmap, quota is 2
				cm := &corev1.ConfigMap{
					ObjectMeta: metav1.ObjectMeta{
						Name:      "cm1",
						Namespace: nsName,
					},
				}
				_, err := fakeClient.CoreV1().ConfigMaps(nsName).Create(
					context.Background(),
					cm,
					metav1.CreateOptions{},
				)
				Expect(err).ToNot(HaveOccurred())
				// Simulate batch creation (not strictly supported by AdmissionReview, but test logic)
				review := createObjectCountAdmissionReview("789", nsName, "configmaps")
				body, _ := json.Marshal(review)
				req := httptest.NewRequest("POST", "/webhook", bytes.NewReader(body))
				w := httptest.NewRecorder()
				ginEngine.ServeHTTP(w, req)
				Expect(w.Code).To(Equal(200))
				var resp admissionv1.AdmissionReview
				_ = json.Unmarshal(w.Body.Bytes(), &resp)
				Expect(resp.Response.Allowed).To(BeTrue())
			})

			It("should allow creation of one deployment", func() {
				review := createObjectCountAdmissionReview("2001", nsName, "deployments.apps")
				body, _ := json.Marshal(review)
				req := httptest.NewRequest("POST", "/webhook", bytes.NewReader(body))
				w := httptest.NewRecorder()
				ginEngine.ServeHTTP(w, req)
				Expect(w.Code).To(Equal(200))
				var resp admissionv1.AdmissionReview
				_ = json.Unmarshal(w.Body.Bytes(), &resp)
				Expect(resp.Response.Allowed).To(BeTrue())
			})

			It("should deny creation of two deployments", func() {
				// Create one existing deployment
				dep, err := webhook.client.AppsV1().Deployments(nsName).Create(context.Background(), &appsv1.Deployment{
					ObjectMeta: metav1.ObjectMeta{Name: "dep1", Namespace: nsName},
					Spec: appsv1.DeploymentSpec{
						Selector: &metav1.LabelSelector{
							MatchLabels: map[string]string{"app": "myapp"},
						},
						Template: corev1.PodTemplateSpec{
							ObjectMeta: metav1.ObjectMeta{
								Labels: map[string]string{"app": "myapp"},
							},
							Spec: corev1.PodSpec{
								Containers: []corev1.Container{
									{
										Name:  "minimal",
										Image: "busybox",
									},
								},
							},
						},
					},
				}, metav1.CreateOptions{})
				Expect(err).ToNot(HaveOccurred())
				Expect(dep).NotTo(BeNil())
				Expect(dep).NotTo(BeNil())
				review := createObjectCountAdmissionReview("2002", nsName, "deployments.apps")
				body, _ := json.Marshal(review)
				req := httptest.NewRequest("POST", "/webhook", bytes.NewReader(body))
				w := httptest.NewRecorder()
				ginEngine.ServeHTTP(w, req)
				Expect(w.Code).To(Equal(200))
				var resp admissionv1.AdmissionReview
				_ = json.Unmarshal(w.Body.Bytes(), &resp)
				Expect(resp.Response.Allowed).To(BeFalse())
				Expect(resp.Response.Result.Message).To(ContainSubstring("ClusterResourceQuota"))
				Expect(resp.Response.Result.Message).To(ContainSubstring("deployments.apps limit exceeded"))
			})

			It("should allow creation of one deployment and one ingress", func() {
				reviewDep := createObjectCountAdmissionReview("2003", nsName, "deployments.apps")
				bodyDep, _ := json.Marshal(reviewDep)
				reqDep := httptest.NewRequest("POST", "/webhook", bytes.NewReader(bodyDep))
				wDep := httptest.NewRecorder()
				ginEngine.ServeHTTP(wDep, reqDep)
				Expect(wDep.Code).To(Equal(200))
				var respDep admissionv1.AdmissionReview
				_ = json.Unmarshal(wDep.Body.Bytes(), &respDep)
				Expect(respDep.Response.Allowed).To(BeTrue())

				reviewIng := createObjectCountAdmissionReview("2004", nsName, "ingresses.networking.k8s.io")
				bodyIng, _ := json.Marshal(reviewIng)
				reqIng := httptest.NewRequest("POST", "/webhook", bytes.NewReader(bodyIng))
				wIng := httptest.NewRecorder()
				ginEngine.ServeHTTP(wIng, reqIng)
				Expect(wIng.Code).To(Equal(200))
				var respIng admissionv1.AdmissionReview
				_ = json.Unmarshal(wIng.Body.Bytes(), &respIng)
				Expect(respIng.Response.Allowed).To(BeTrue())
			})

			It("should deny creation with multiple objects over quota", func() {
				// Add 2 configmaps, quota is 2
				// Add 2 configmaps, quota is 2
				cm1 := &corev1.ConfigMap{
					ObjectMeta: metav1.ObjectMeta{
						Name:      "cm1",
						Namespace: nsName,
					},
				}
				cm2 := &corev1.ConfigMap{
					ObjectMeta: metav1.ObjectMeta{
						Name:      "cm2",
						Namespace: nsName,
					},
				}
				_, err := fakeClient.CoreV1().ConfigMaps(nsName).Create(
					context.Background(), cm1, metav1.CreateOptions{})
				Expect(err).ToNot(HaveOccurred())
				_, err = fakeClient.CoreV1().ConfigMaps(nsName).Create(
					context.Background(), cm2, metav1.CreateOptions{})
				Expect(err).ToNot(HaveOccurred())
				review := createObjectCountAdmissionReview("1011", nsName, "configmaps")
				body, _ := json.Marshal(review)
				req := httptest.NewRequest("POST", "/webhook", bytes.NewReader(body))
				w := httptest.NewRecorder()
				ginEngine.ServeHTTP(w, req)
				Expect(w.Code).To(Equal(200))
				var resp admissionv1.AdmissionReview
				_ = json.Unmarshal(w.Body.Bytes(), &resp)
				Expect(resp.Response.Allowed).To(BeFalse())
				Expect(resp.Response.Result.Message).To(ContainSubstring("ClusterResourceQuota"))
				Expect(resp.Response.Result.Message).To(ContainSubstring("configmaps limit exceeded"))
			})

			It("should allow creation with zero objects", func() {
				// No configmaps present
				review := createObjectCountAdmissionReview("1213", nsName, "configmaps")
				body, _ := json.Marshal(review)
				req := httptest.NewRequest("POST", "/webhook", bytes.NewReader(body))
				w := httptest.NewRecorder()
				ginEngine.ServeHTTP(w, req)
				Expect(w.Code).To(Equal(200))
				var resp admissionv1.AdmissionReview
				_ = json.Unmarshal(w.Body.Bytes(), &resp)
				Expect(resp.Response.Allowed).To(BeTrue())
			})

			It("should allow creation of unknown resource type", func() {
				review := createObjectCountAdmissionReview("1415", nsName, "invalidresource")
				body, _ := json.Marshal(review)
				req := httptest.NewRequest("POST", "/webhook", bytes.NewReader(body))
				w := httptest.NewRecorder()
				ginEngine.ServeHTTP(w, req)
				Expect(w.Code).To(Equal(200))
				var resp admissionv1.AdmissionReview
				_ = json.Unmarshal(w.Body.Bytes(), &resp)
				Expect(resp.Response.Allowed).To(BeTrue())
			})

			It("should deny creation when namespace missing", func() {
				review := createObjectCountAdmissionReview("1617", "", "configmaps")
				body, _ := json.Marshal(review)
				req := httptest.NewRequest("POST", "/webhook", bytes.NewReader(body))
				w := httptest.NewRecorder()
				ginEngine.ServeHTTP(w, req)
				Expect(w.Code).To(Equal(200))
				var resp admissionv1.AdmissionReview
				_ = json.Unmarshal(w.Body.Bytes(), &resp)
				Expect(resp.Response.Allowed).To(BeFalse())
				Expect(resp.Response.Result.Message).To(ContainSubstring("Missing admission request namespace"))
			})

			It("should allow creation when CRQClient fails", func() {
				// Simulate CRQClient failure by passing nil client
				webhook.crqClient = quota.NewCRQClient(nil)
				review := createObjectCountAdmissionReview("1819", nsName, "configmaps")
				body, _ := json.Marshal(review)
				req := httptest.NewRequest("POST", "/webhook", bytes.NewReader(body))
				w := httptest.NewRecorder()
				ginEngine.ServeHTTP(w, req)
				Expect(w.Code).To(Equal(200))
				var resp admissionv1.AdmissionReview
				_ = json.Unmarshal(w.Body.Bytes(), &resp)
				Expect(resp.Response.Allowed).To(BeTrue())
			})
		})
	})
})

func createObjectCountAdmissionReview(uid, namespace, resourceName string) admissionv1.AdmissionReview {
	return admissionv1.AdmissionReview{
		Request: &admissionv1.AdmissionRequest{
			UID:       types.UID(uid),
			Namespace: namespace,
			Operation: admissionv1.Create,
			Resource:  metav1.GroupVersionResource{Resource: resourceName},
		},
	}
}
