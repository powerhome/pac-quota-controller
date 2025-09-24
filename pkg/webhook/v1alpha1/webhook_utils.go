package v1alpha1

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"

	"go.uber.org/zap"
	admissionv1 "k8s.io/api/admission/v1"
	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/resource"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/client-go/kubernetes"

	"github.com/gin-gonic/gin"
	quotav1alpha1 "github.com/powerhome/pac-quota-controller/api/v1alpha1"
	"github.com/powerhome/pac-quota-controller/pkg/kubernetes/namespace"
	"github.com/powerhome/pac-quota-controller/pkg/kubernetes/quota"
)

type operation string

const (
	OperationCreate operation = "creation"
	OperationUpdate operation = "update"
	OperationDelete operation = "deletion"
)

// validateCRQResourceQuotaWithNamespace is a shared function for validating resource quotas
// across webhooks with actual namespace object
func validateCRQResourceQuotaWithNamespace(
	ctx context.Context,
	crqClient *quota.CRQClient,
	kubernetesClient kubernetes.Interface,
	ns *corev1.Namespace,
	resourceName corev1.ResourceName,
	requestedQuantity resource.Quantity,
	calculateCurrentUsage func(string, corev1.ResourceName) (resource.Quantity, error),
	log *zap.Logger,
) error {
	if crqClient == nil {
		log.Info("Skipping CRQ validation - no CRQ client available",
			zap.String("namespace", ns.Name),
			zap.String("resource", string(resourceName)),
			zap.String("requested", requestedQuantity.String()))
		return nil
	}

	log.Info("Starting CRQ validation",
		zap.String("namespace", ns.Name),
		zap.String("resource", string(resourceName)),
		zap.String("requested", requestedQuantity.String()),
		zap.Any("namespace_labels", ns.Labels))

	// Find the CRQ that applies to this namespace
	crq, err := crqClient.GetCRQByNamespace(ctx, ns)
	if err != nil {
		log.Error("Failed to get CRQ for namespace",
			zap.String("namespace", ns.Name),
			zap.Error(err))
		return nil
	}

	// If no CRQ applies to this namespace, allow the operation
	if crq == nil {
		log.Info("No CRQ applies to namespace, allowing operation",
			zap.String("namespace", ns.Name),
			zap.String("resource", string(resourceName)))
		return nil
	}

	log.Info("Found matching CRQ",
		zap.String("namespace", ns.Name),
		zap.String("crq", crq.Name),
		zap.String("resource", string(resourceName)))

	// Check if the requested quantity would exceed the quota
	if quotaLimit, exists := crq.Spec.Hard[resourceName]; exists {
		log.Info("Found quota limit for resource",
			zap.String("resource", string(resourceName)),
			zap.String("limit", quotaLimit.String()))

		// Calculate current usage across ALL namespaces that match the CRQ selector
		currentUsage, err := calculateCRQCurrentUsage(ctx, kubernetesClient, crq, resourceName, calculateCurrentUsage, log)
		if err != nil {
			log.Error("Failed to calculate current usage across CRQ namespaces",
				zap.String("namespace", ns.Name),
				zap.String("resource", string(resourceName)),
				zap.Error(err))
			return fmt.Errorf("failed to calculate current usage: %w", err)
		}

		// Calculate total usage after this operation
		totalUsage := currentUsage.DeepCopy()
		totalUsage.Add(requestedQuantity)

		log.Info("Quota validation check",
			zap.String("namespace", ns.Name),
			zap.String("resource", string(resourceName)),
			zap.String("current_usage", currentUsage.String()),
			zap.String("requested", requestedQuantity.String()),
			zap.String("total_usage", totalUsage.String()),
			zap.String("limit", quotaLimit.String()))

		// Check if total usage would exceed quota
		if totalUsage.Cmp(quotaLimit) > 0 {
			log.Error("Resource quota would be exceeded",
				zap.String("namespace", ns.Name),
				zap.String("resource", string(resourceName)),
				zap.String("current_usage", currentUsage.String()),
				zap.String("requested", requestedQuantity.String()),
				zap.String("total_usage", totalUsage.String()),
				zap.String("limit", quotaLimit.String()),
				zap.String("crq", crq.Name))
			return fmt.Errorf("ClusterResourceQuota '%s' %s limit exceeded: requested %s, current usage %s, "+
				"quota limit %s, total would be %s",
				crq.Name, resourceName, requestedQuantity.String(), currentUsage.String(), quotaLimit.String(), totalUsage.String())
		}
	} else {
		log.Info("No quota limit defined for resource, allowing operation",
			zap.String("namespace", ns.Name),
			zap.String("resource", string(resourceName)),
			zap.String("crq", crq.Name))
	}

	log.Info("CRQ validation passed",
		zap.String("namespace", ns.Name),
		zap.String("resource", string(resourceName)),
		zap.String("requested", requestedQuantity.String()),
		zap.String("crq", crq.Name))
	return nil
}

// calculateCRQCurrentUsage calculates the current usage of a resource across all namespaces
// that match the ClusterResourceQuota selector
func calculateCRQCurrentUsage(
	ctx context.Context,
	kubernetesClient kubernetes.Interface,
	crq *quotav1alpha1.ClusterResourceQuota,
	resourceName corev1.ResourceName,
	calculateCurrentUsage func(string, corev1.ResourceName) (resource.Quantity, error),
	log *zap.Logger,
) (resource.Quantity, error) {
	// Get all namespaces that match the CRQ selector
	namespaceNames, err := namespace.GetSelectedNamespaces(ctx, kubernetesClient, crq)
	if err != nil {
		return resource.Quantity{}, fmt.Errorf("failed to get namespaces matching CRQ selector: %w", err)
	}

	log.Info("Calculating usage across CRQ namespaces",
		zap.String("crq", crq.Name),
		zap.String("resource", string(resourceName)),
		zap.Strings("namespaces", namespaceNames))

	// Calculate total usage across all matching namespaces
	totalUsage := resource.NewQuantity(0, resource.DecimalSI)
	for _, namespaceName := range namespaceNames {
		nsUsage, err := calculateCurrentUsage(namespaceName, resourceName)
		if err != nil {
			log.Error("Failed to calculate usage for namespace",
				zap.String("namespace", namespaceName),
				zap.String("resource", string(resourceName)),
				zap.Error(err))
			return resource.Quantity{}, fmt.Errorf("failed to calculate usage for namespace %s: %w", namespaceName, err)
		}
		totalUsage.Add(nsUsage)

		log.Debug("Namespace usage calculated",
			zap.String("namespace", namespaceName),
			zap.String("resource", string(resourceName)),
			zap.String("usage", nsUsage.String()))
	}

	log.Info("Total CRQ usage calculated",
		zap.String("crq", crq.Name),
		zap.String("resource", string(resourceName)),
		zap.String("total_usage", totalUsage.String()),
		zap.Strings("namespaces", namespaceNames))

	return *totalUsage, nil
}

func sendWebhookRequest(engine *gin.Engine, admissionReview *admissionv1.AdmissionReview) *admissionv1.AdmissionReview {
	body, _ := json.Marshal(admissionReview)
	req, _ := http.NewRequest("POST", "/webhook", bytes.NewBuffer(body))
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()

	engine.ServeHTTP(w, req)

	var response admissionv1.AdmissionReview
	if err := json.Unmarshal(w.Body.Bytes(), &response); err != nil {
		// If unmarshaling fails, create a default response
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

// handleWebhookOperation is a shared helper for operation switch logic in pod/service webhooks
func handleWebhookOperation(
	log *zap.Logger,
	operation admissionv1.Operation,
	name, ns string,
	createFn func() ([]string, error),
	updateFn func() ([]string, error),
	c *gin.Context,
	admissionReview *admissionv1.AdmissionReview,
	resourceType string,
) ([]string, error) {
	var warnings []string
	var err error
	switch operation {
	case admissionv1.Create:
		log.Info(fmt.Sprintf("Validating %s on create", resourceType),
			zap.String("name", name),
			zap.String("namespace", ns))
		warnings, err = createFn()
	case admissionv1.Update:
		log.Info(fmt.Sprintf("Validating %s on update", resourceType),
			zap.String("name", name),
			zap.String("namespace", ns))
		warnings, err = updateFn()
	default:
		log.Info("Unsupported operation", zap.String("operation", string(operation)))
		admissionReview.Response.Allowed = false
		admissionReview.Response.Result = &metav1.Status{
			Code:    400,
			Message: fmt.Sprintf("Operation %s is not supported for %s", operation, resourceType),
		}
		c.JSON(200, admissionReview)
		return nil, fmt.Errorf("unsupported operation")
	}
	return warnings, err
}
