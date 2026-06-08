package v1alpha1

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"

	"github.com/gin-gonic/gin"
	admissionv1 "k8s.io/api/admission/v1"
	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/resource"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	ctrlclient "sigs.k8s.io/controller-runtime/pkg/client"
	ctrlclientfake "sigs.k8s.io/controller-runtime/pkg/client/fake"
	"sigs.k8s.io/controller-runtime/pkg/client/interceptor"

	quotav1alpha1 "github.com/powerhome/pac-quota-controller/api/v1alpha1"
	"github.com/powerhome/pac-quota-controller/pkg/kubernetes/quota"
	"github.com/powerhome/pac-quota-controller/pkg/logger"
)

var errFakeList = errors.New("fake list error")

// sendWebhookRequest drives the gin engine via HTTP round-trip and decodes the AdmissionReview.
func sendWebhookRequest(engine *gin.Engine, admissionReview *admissionv1.AdmissionReview) *admissionv1.AdmissionReview {
	body, _ := json.Marshal(admissionReview)
	req, _ := http.NewRequest("POST", "/webhook", bytes.NewBuffer(body))
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()

	engine.ServeHTTP(w, req)

	var response admissionv1.AdmissionReview
	if err := json.Unmarshal(w.Body.Bytes(), &response); err != nil {
		response = admissionv1.AdmissionReview{
			Response: &admissionv1.AdmissionResponse{
				Allowed: false,
				Result: &metav1.Status{
					Message: "Failed to parse response",
				},
			},
		}
	}
	return &response
}

// testScheme returns a scheme registered with CRQ + corev1.
func testScheme() *runtime.Scheme {
	s := runtime.NewScheme()
	_ = quotav1alpha1.AddToScheme(s)
	_ = corev1.AddToScheme(s)
	return s
}

// newTestCRQClient builds an in-memory CRQClient seeded with the given objects.
func newTestCRQClient(objs ...ctrlclient.Object) *quota.CRQClient {
	cl := ctrlclientfake.NewClientBuilder().
		WithScheme(testScheme()).
		WithObjects(objs...).
		Build()
	return quota.NewCRQClient(cl, logger.L())
}

// newTestCRQClientWithListError returns a CRQClient whose List operations fail (exercises CRQ-lookup fail-open).
func newTestCRQClientWithListError(seed ...ctrlclient.Object) *quota.CRQClient {
	cl := ctrlclientfake.NewClientBuilder().
		WithScheme(testScheme()).
		WithObjects(seed...).
		WithInterceptorFuncs(interceptor.Funcs{
			List: func(_ context.Context, _ ctrlclient.WithWatch, _ ctrlclient.ObjectList, _ ...ctrlclient.ListOption) error {
				return errFakeList
			},
		}).
		Build()
	return quota.NewCRQClient(cl, logger.L())
}

// makeNamespace returns a Namespace object with the requested labels.
func makeNamespace(name string, labels map[string]string) *corev1.Namespace {
	return &corev1.Namespace{
		ObjectMeta: metav1.ObjectMeta{
			Name:   name,
			Labels: labels,
		},
	}
}

// makeCRQ returns a CRQ that selects namespaces by labels and seeds Spec.Hard / Status.Total.Used.
func makeCRQ(name string, selectorLabels map[string]string,
	hard, used quotav1alpha1.ResourceList) *quotav1alpha1.ClusterResourceQuota {
	return &quotav1alpha1.ClusterResourceQuota{
		ObjectMeta: metav1.ObjectMeta{Name: name},
		Spec: quotav1alpha1.ClusterResourceQuotaSpec{
			NamespaceSelector: &metav1.LabelSelector{MatchLabels: selectorLabels},
			Hard:              hard,
		},
		Status: quotav1alpha1.ClusterResourceQuotaStatus{
			Total: quotav1alpha1.ResourceQuotaStatus{
				Hard: hard,
				Used: used,
			},
		},
	}
}

// quantity is a one-liner for resource.MustParse.
func quantity(s string) resource.Quantity { return resource.MustParse(s) }
