package v1alpha1

import (
	"context"
	"fmt"
	"net/http"

	"github.com/gin-gonic/gin"
	"go.uber.org/zap"
	admissionv1 "k8s.io/api/admission/v1"
	"k8s.io/apimachinery/pkg/api/resource"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/runtime/serializer"
	"k8s.io/client-go/kubernetes"

	quotav1alpha1 "github.com/powerhome/pac-quota-controller/api/v1alpha1"
	"github.com/powerhome/pac-quota-controller/pkg/kubernetes/namespace"
	"github.com/powerhome/pac-quota-controller/pkg/kubernetes/quota"
	"github.com/powerhome/pac-quota-controller/pkg/kubernetes/services"
)

// ClusterResourceQuotaWebhook handles webhook requests for ClusterResourceQuota resources
type ClusterResourceQuotaWebhook struct {
	client            kubernetes.Interface
	crqClient         *quota.CRQClient
	serviceCalculator services.ServiceResourceCalculatorInterface
	log               *zap.Logger
}

// NewClusterResourceQuotaWebhook creates a new ClusterResourceQuotaWebhook
func NewClusterResourceQuotaWebhook(
	k8sClient kubernetes.Interface,
	crqClient *quota.CRQClient,
	log *zap.Logger,
) *ClusterResourceQuotaWebhook {
	return &ClusterResourceQuotaWebhook{
		client:            k8sClient,
		crqClient:         crqClient,
		serviceCalculator: services.NewServiceResourceCalculator(k8sClient),
		log:               log,
	}
}

// Handle handles the webhook request for ClusterResourceQuota
func (h *ClusterResourceQuotaWebhook) Handle(c *gin.Context) {
	var admissionReview admissionv1.AdmissionReview
	if err := c.ShouldBindJSON(&admissionReview); err != nil {
		h.log.Error("Invalid admission review request", zap.Error(err))
		c.JSON(http.StatusBadRequest, gin.H{
			"error":   "Invalid request format",
			"details": err.Error(),
		})
		return
	}

	// Validate the request first
	if admissionReview.Request == nil {
		h.log.Info("Admission review missing request field")
		admissionReview.Response = &admissionv1.AdmissionResponse{
			Allowed: false,
			Result: &metav1.Status{
				Code:    http.StatusBadRequest,
				Message: "Missing admission request",
			},
		}
		c.JSON(http.StatusOK, admissionReview)
		return
	}

	// Set the response type
	admissionReview.Response = &admissionv1.AdmissionResponse{
		UID: admissionReview.Request.UID,
	}

	// Check if this is for the correct resource
	expectedGVK := metav1.GroupVersionKind{
		Group:   "quota.powerapp.cloud",
		Version: "v1alpha1",
		Kind:    "ClusterResourceQuota",
	}
	if admissionReview.Request.Kind != expectedGVK {
		h.log.Info("Unexpected resource type",
			zap.String("expected", expectedGVK.Kind),
			zap.String("got", admissionReview.Request.Kind.Kind))
		admissionReview.Response.Allowed = false
		admissionReview.Response.Result = &metav1.Status{
			Code:    http.StatusBadRequest,
			Message: fmt.Sprintf("Expected %s resource, got %s", expectedGVK.Kind, admissionReview.Request.Kind.Kind),
		}
		c.JSON(http.StatusOK, admissionReview)
		return
	}

	// Decode the object
	var crq quotav1alpha1.ClusterResourceQuota
	if err := runtime.DecodeInto(
		serializer.NewCodecFactory(runtime.NewScheme()).UniversalDeserializer(),
		admissionReview.Request.Object.Raw,
		&crq,
	); err != nil {
		h.log.Error("Failed to decode ClusterResourceQuota", zap.Error(err))
		admissionReview.Response.Allowed = false
		admissionReview.Response.Result = &metav1.Status{
			Code:    http.StatusBadRequest,
			Message: "Unable to decode ClusterResourceQuota object",
		}
		c.JSON(http.StatusOK, admissionReview)
		return
	}

	// Validate based on operation

	var err error
	ctx := c.Request.Context()

	switch admissionReview.Request.Operation {
	case admissionv1.Create:
		h.log.Info("Validating ClusterResourceQuota on create",
			zap.String("name", crq.GetName()))
		err = h.validateCreate(ctx, &crq)
	case admissionv1.Update:
		h.log.Info("Validating ClusterResourceQuota on update",
			zap.String("name", crq.GetName()))
		err = h.validateUpdate(ctx, &crq)
	case admissionv1.Delete:
		h.log.Info("Validating ClusterResourceQuota on delete",
			zap.String("name", crq.GetName()))
		err = h.validateDelete(ctx)
	default:
		h.log.Info("Unsupported operation", zap.String("operation", string(admissionReview.Request.Operation)))
		admissionReview.Response.Allowed = false
		admissionReview.Response.Result = &metav1.Status{
			Code:    http.StatusBadRequest,
			Message: fmt.Sprintf("Operation %s is not supported for ClusterResourceQuota", admissionReview.Request.Operation),
		}
		c.JSON(http.StatusOK, admissionReview)
		return
	}

	if err != nil {
		h.log.Error("Validation failed", zap.Error(err))
		admissionReview.Response.Allowed = false
		admissionReview.Response.Result = &metav1.Status{
			Code:    http.StatusForbidden,
			Message: err.Error(),
		}
	} else {
		admissionReview.Response.Allowed = true
	}

	c.JSON(http.StatusOK, admissionReview)
}

func (h *ClusterResourceQuotaWebhook) validateCreate(
	ctx context.Context,
	crq *quotav1alpha1.ClusterResourceQuota,
) error {
	if h.crqClient == nil {
		return fmt.Errorf("CRQ client not available for validation")
	}

	validator := namespace.NewNamespaceValidator(h.client, h.crqClient)
	if err := validator.ValidateCRQNamespaceConflicts(ctx, crq); err != nil {
		return err
	}

	// Validate service object count quotas for all supported service resource types
	if crq.Spec.Hard != nil && crq.Spec.NamespaceSelector != nil {
		selector, err := namespace.NewLabelBasedNamespaceSelector(h.client, crq.Spec.NamespaceSelector)
		if err != nil {
			return fmt.Errorf("failed to create namespace selector: %w", err)
		}
		selectedNamespaces, err := selector.GetSelectedNamespaces(ctx)
		if err != nil {
			return fmt.Errorf("failed to get selected namespaces: %w", err)
		}
		for resourceName := range crq.Spec.Hard {
			switch resourceName {
			case "services", "services.loadbalancers", "services.nodeports", "services.clusterips", "services.externalnames":
				var totalUsage resource.Quantity
				for _, ns := range selectedNamespaces {
					usageQty, err := h.serviceCalculator.CalculateUsage(ctx, ns, resourceName)
					if err != nil {
						return fmt.Errorf("failed to calculate usage for %s in namespace %s: %w", resourceName, ns, err)
					}
					if totalUsage.IsZero() {
						totalUsage = usageQty.DeepCopy()
					} else {
						totalUsage.Add(usageQty)
					}
				}
				hardQty := crq.Spec.Hard[resourceName]
				if totalUsage.Cmp(hardQty) > 0 {
					return fmt.Errorf("quota exceeded for %s: used %s, hard limit %s", resourceName, totalUsage.String(), hardQty.String())
				}
			}
		}
	}
	return nil
}

func (h *ClusterResourceQuotaWebhook) validateUpdate(
	ctx context.Context,
	crq *quotav1alpha1.ClusterResourceQuota,
) error {
	if h.crqClient == nil {
		return fmt.Errorf("CRQ client not available for validation")
	}

	validator := namespace.NewNamespaceValidator(h.client, h.crqClient)
	if err := validator.ValidateCRQNamespaceConflicts(ctx, crq); err != nil {
		return err
	}

	// Validate service object count quotas for all supported service resource types
	if crq.Spec.Hard != nil && crq.Spec.NamespaceSelector != nil {
		selector, err := namespace.NewLabelBasedNamespaceSelector(h.client, crq.Spec.NamespaceSelector)
		if err != nil {
			return fmt.Errorf("failed to create namespace selector: %w", err)
		}
		selectedNamespaces, err := selector.GetSelectedNamespaces(ctx)
		if err != nil {
			return fmt.Errorf("failed to get selected namespaces: %w", err)
		}
		for resourceName := range crq.Spec.Hard {
			switch resourceName {
			case "services", "services.loadbalancers", "services.nodeports", "services.clusterips", "services.externalnames":
				var totalUsage resource.Quantity
				for _, ns := range selectedNamespaces {
					usageQty, err := h.serviceCalculator.CalculateUsage(ctx, ns, resourceName)
					if err != nil {
						return fmt.Errorf("failed to calculate usage for %s in namespace %s: %w", resourceName, ns, err)
					}
					if totalUsage.IsZero() {
						totalUsage = usageQty.DeepCopy()
					} else {
						totalUsage.Add(usageQty)
					}
				}
				hardQty := crq.Spec.Hard[resourceName]
				if totalUsage.Cmp(hardQty) > 0 {
					return fmt.Errorf("quota exceeded for %s: used %s, hard limit %s", resourceName, totalUsage.String(), hardQty.String())
				}
			}
		}
	}
	return nil
}

func (h *ClusterResourceQuotaWebhook) validateDelete(_ context.Context) error {
	// No validation needed for delete operations
	return nil
}
