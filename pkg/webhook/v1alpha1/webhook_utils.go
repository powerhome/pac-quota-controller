package v1alpha1

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"

	"github.com/gin-gonic/gin"
	"go.uber.org/zap"
	admissionv1 "k8s.io/api/admission/v1"
	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/resource"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/client-go/kubernetes"

	quotav1alpha1 "github.com/powerhome/pac-quota-controller/api/v1alpha1"
	"github.com/powerhome/pac-quota-controller/pkg/kubernetes/namespace"
)

type operation string

const (
	OperationCreate operation = "creation"
	OperationUpdate operation = "update"
	OperationDelete operation = "deletion"
)

// calculateCRQCurrentUsage sums the per-resource usage across every namespace
// that matches the CRQ selector. It's the cross-namespace half of
// validateAgainstCRQ in webhook_handler.go.
func calculateCRQCurrentUsage(
	ctx context.Context,
	kubernetesClient kubernetes.Interface,
	crq *quotav1alpha1.ClusterResourceQuota,
	resourceName corev1.ResourceName,
	calculateCurrentUsage func(string, corev1.ResourceName) (resource.Quantity, error),
	logger *zap.Logger,
) (resource.Quantity, error) {
	namespaceNames, err := namespace.GetSelectedNamespaces(ctx, kubernetesClient, crq)
	if err != nil {
		return resource.Quantity{}, fmt.Errorf("failed to get namespaces matching CRQ selector: %w", err)
	}

	logger.Debug("Calculating usage across CRQ namespaces",
		zap.String("crq", crq.Name),
		zap.String("resource", string(resourceName)),
		zap.Strings("namespaces", namespaceNames))

	totalUsage := resource.NewQuantity(0, resource.DecimalSI)
	for _, namespaceName := range namespaceNames {
		nsUsage, err := calculateCurrentUsage(namespaceName, resourceName)
		if err != nil {
			logger.Error("Failed to calculate usage for namespace",
				zap.String("namespace", namespaceName),
				zap.String("resource", string(resourceName)),
				zap.Error(err))
			return resource.Quantity{}, fmt.Errorf("failed to calculate usage for namespace %s: %w", namespaceName, err)
		}
		totalUsage.Add(nsUsage)

		logger.Debug("Namespace usage calculated",
			zap.String("namespace", namespaceName),
			zap.String("resource", string(resourceName)),
			zap.String("usage", nsUsage.String()))
	}

	logger.Debug("Total CRQ usage calculated",
		zap.String("crq", crq.Name),
		zap.String("resource", string(resourceName)),
		zap.String("total_usage", totalUsage.String()),
		zap.Strings("namespaces", namespaceNames))

	return *totalUsage, nil
}

// sendWebhookRequest is a test helper that drives the gin engine through an
// HTTP round-trip and decodes the resulting AdmissionReview.
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
