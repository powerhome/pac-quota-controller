package v1alpha1

import (
	"bytes"
	"encoding/json"
	"net/http"
	"net/http/httptest"

	"github.com/gin-gonic/gin"
	admissionv1 "k8s.io/api/admission/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

// sendWebhookRequest drives the gin engine through an HTTP round-trip and
// decodes the resulting AdmissionReview. Test-only.
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
