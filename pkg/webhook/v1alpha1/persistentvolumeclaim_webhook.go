package v1alpha1

import (
	"context"
	"fmt"
	"net/http"
	"strings"

	"github.com/gin-gonic/gin"
	"go.uber.org/zap"
	admissionv1 "k8s.io/api/admission/v1"
	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/resource"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/runtime/serializer"
	"k8s.io/client-go/kubernetes"

	"github.com/powerhome/pac-quota-controller/pkg/kubernetes/quota"
	"github.com/powerhome/pac-quota-controller/pkg/kubernetes/storage"
	"github.com/powerhome/pac-quota-controller/pkg/kubernetes/usage"
	"github.com/powerhome/pac-quota-controller/pkg/metrics"
	"github.com/prometheus/client_golang/prometheus"
)

// PersistentVolumeClaimWebhook handles webhook requests for PersistentVolumeClaim resources
type PersistentVolumeClaimWebhook struct {
	client            kubernetes.Interface
	storageCalculator storage.StorageResourceCalculator
	crqClient         *quota.CRQClient
	log               *zap.Logger
}

// NewPersistentVolumeClaimWebhook creates a new PersistentVolumeClaimWebhook
func NewPersistentVolumeClaimWebhook(
	k8sClient kubernetes.Interface,
	crqClient *quota.CRQClient,
	log *zap.Logger,
) *PersistentVolumeClaimWebhook {
	return &PersistentVolumeClaimWebhook{
		client:            k8sClient,
		storageCalculator: *storage.NewStorageResourceCalculator(k8sClient),
		crqClient:         crqClient,
		log:               log,
	}
}

// Handle handles the webhook request for PersistentVolumeClaim
func (h *PersistentVolumeClaimWebhook) Handle(c *gin.Context) {
	var admissionReview admissionv1.AdmissionReview
	if err := c.ShouldBindJSON(&admissionReview); err != nil {
		h.log.Error("Failed to bind admission review", zap.Error(err))
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}

	// Check for malformed requests
	if admissionReview.Request == nil {
		h.log.Error("Malformed admission review request")
		c.JSON(http.StatusBadRequest, http.StatusBadRequest)
		return
	}

	if namespace := admissionReview.Request.Namespace; namespace == "" {
		h.log.Info("Admission review request namespace is empty")
		admissionReview.Response = &admissionv1.AdmissionResponse{
			UID:     admissionReview.Request.UID,
			Allowed: false,
			Result: &metav1.Status{
				Code:    http.StatusBadRequest,
				Message: "Namespace is required for object count validation",
			},
		}
		c.JSON(http.StatusOK, admissionReview)
		return
	}

	// Metrics: start timer and increment validation count
	operation := string(admissionReview.Request.Operation)
	webhookName := "persistentvolumeclaim"
	metrics.WebhookValidationCount.WithLabelValues(webhookName, operation).Inc()
	timer := prometheus.NewTimer(metrics.WebhookValidationDuration.WithLabelValues(webhookName, operation))
	defer timer.ObserveDuration()

	// Set the response type
	admissionReview.Response = &admissionv1.AdmissionResponse{
		UID: admissionReview.Request.UID,
	}

	// Check if this is for the correct resource
	expectedGVK := metav1.GroupVersionKind{
		Group:   "",
		Version: "v1",
		Kind:    "PersistentVolumeClaim",
	}
	if admissionReview.Request.Kind != expectedGVK {
		h.log.Error("Unexpected resource type",
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
	var pvc corev1.PersistentVolumeClaim
	if err := runtime.DecodeInto(
		serializer.NewCodecFactory(runtime.NewScheme()).UniversalDeserializer(),
		admissionReview.Request.Object.Raw,
		&pvc,
	); err != nil {
		h.log.Error("Failed to decode PersistentVolumeClaim", zap.Error(err))
		admissionReview.Response.Allowed = false
		admissionReview.Response.Result = &metav1.Status{
			Code:    http.StatusBadRequest,
			Message: "Unable to decode PersistentVolumeClaim object",
		}
		c.JSON(http.StatusOK, admissionReview)
		return
	}

	// Validate based on operation
	var warnings []string
	var err error

	ctx := c.Request.Context()
	switch admissionReview.Request.Operation {
	case admissionv1.Create:
		h.log.Info("Validating PersistentVolumeClaim on create",
			zap.String("name", pvc.GetName()),
			zap.String("namespace", pvc.GetNamespace()))
		err = h.validateCreate(ctx, &pvc)
	case admissionv1.Update:
		h.log.Info("Validating PersistentVolumeClaim on update",
			zap.String("name", pvc.GetName()),
			zap.String("namespace", pvc.GetNamespace()))
		err = h.validateUpdate(ctx, &pvc)
	default:
		h.log.Info("Unsupported operation", zap.String("operation", string(admissionReview.Request.Operation)))
		admissionReview.Response.Allowed = false
		admissionReview.Response.Result = &metav1.Status{
			Code:    http.StatusBadRequest,
			Message: fmt.Sprintf("Operation %s is not supported for PersistentVolumeClaim", admissionReview.Request.Operation),
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
		metrics.WebhookAdmissionDecision.WithLabelValues(webhookName, operation, "denied").Inc()
	} else {
		admissionReview.Response.Allowed = true
		if len(warnings) > 0 {
			admissionReview.Response.Warnings = warnings
		}
		metrics.WebhookAdmissionDecision.WithLabelValues(webhookName, operation, "allowed").Inc()
	}

	c.JSON(http.StatusOK, admissionReview)
}

func (h *PersistentVolumeClaimWebhook) validateCreate(
	ctx context.Context,
	pvc *corev1.PersistentVolumeClaim,
) error {
	// Check if any ClusterResourceQuota applies to this namespace and would be exceeded
	if err := h.validateStorageQuota(ctx, pvc); err != nil {
		h.log.Error("PVC creation blocked due to quota violation",
			zap.String("pvc", pvc.GetName()),
			zap.String("namespace", pvc.GetNamespace()),
			zap.Error(err))
		return err
	}

	return nil
}

func (h *PersistentVolumeClaimWebhook) validateUpdate(
	ctx context.Context,
	pvc *corev1.PersistentVolumeClaim,
) error {
	// Check if any ClusterResourceQuota applies to this namespace and would be exceeded
	if err := h.validateStorageQuota(ctx, pvc); err != nil {
		h.log.Error("PVC update blocked due to quota violation",
			zap.String("pvc", pvc.GetName()),
			zap.String("namespace", pvc.GetNamespace()),
			zap.Error(err))
		return err
	}

	return nil
}

func (h *PersistentVolumeClaimWebhook) validateStorageQuota(
	ctx context.Context,
	pvc *corev1.PersistentVolumeClaim,
) error {
	// Get storage request from PVC
	storageRequest := getStorageRequest(pvc)

	// Validate general storage quota
	if err := h.validateResourceQuota(ctx, pvc.Namespace, usage.ResourceRequestsStorage, storageRequest); err != nil {
		return fmt.Errorf("ClusterResourceQuota storage validation failed: %w", err)
	}

	// Validate general PVC count (always 1 for a new PVC)
	pvcCount := resource.NewQuantity(1, resource.DecimalSI)
	if err := h.validateResourceQuota(ctx, pvc.Namespace, usage.ResourcePersistentVolumeClaims, *pvcCount); err != nil {
		return fmt.Errorf("ClusterResourceQuota PVC count validation failed: %w", err)
	}

	// Validate storage class specific quotas if storage class is specified
	if pvc.Spec.StorageClassName != nil && *pvc.Spec.StorageClassName != "" {
		storageClass := *pvc.Spec.StorageClassName

		// Validate storage class specific storage quota
		storageClassStorageResource := corev1.ResourceName(fmt.Sprintf(
			"%s.storageclass.storage.k8s.io/requests.storage", storageClass))
		if err := h.validateResourceQuota(ctx, pvc.Namespace, storageClassStorageResource, storageRequest); err != nil {
			return fmt.Errorf("ClusterResourceQuota storage class '%s' storage validation failed: %w", storageClass, err)
		}

		// Validate storage class specific PVC count
		storageClassCountResource := corev1.ResourceName(fmt.Sprintf(
			"%s.storageclass.storage.k8s.io/persistentvolumeclaims", storageClass))
		if err := h.validateResourceQuota(ctx, pvc.Namespace, storageClassCountResource, *pvcCount); err != nil {
			return fmt.Errorf("ClusterResourceQuota storage class '%s' PVC count validation failed: %w", storageClass, err)
		}

		h.log.Info("PVC storage class specific CRQ validation passed",
			zap.String("pvc", pvc.Name),
			zap.String("namespace", pvc.Namespace),
			zap.String("storageClass", storageClass),
			zap.String("storageRequest", storageRequest.String()))
	}

	h.log.Info("PVC CRQ validation passed",
		zap.String("pvc", pvc.Name),
		zap.String("namespace", pvc.Namespace),
		zap.String("storageRequest", storageRequest.String()))
	return nil
}

// validateResourceQuota validates if a resource operation would exceed any applicable ClusterResourceQuota
func (h *PersistentVolumeClaimWebhook) validateResourceQuota(
	ctx context.Context,
	namespace string,
	resourceName corev1.ResourceName,
	requestedQuantity resource.Quantity,
) error {
	// Fetch the actual namespace object with labels
	ns, err := h.client.CoreV1().Namespaces().Get(ctx, namespace, metav1.GetOptions{})
	if err != nil {
		return fmt.Errorf("failed to get namespace %s: %w", namespace, err)
	}

	return validateCRQResourceQuotaWithNamespace(ctx, h.crqClient, h.client, ns, resourceName, requestedQuantity,
		func(ns string, rn corev1.ResourceName) (resource.Quantity, error) {
			return h.calculateCurrentUsage(ctx, ns, rn)
		}, h.log)
}

// calculateCurrentUsage calculates the current usage of a resource in a namespace
func (h *PersistentVolumeClaimWebhook) calculateCurrentUsage(ctx context.Context, namespace string,
	resourceName corev1.ResourceName) (resource.Quantity, error) {
	switch resourceName {
	case usage.ResourceRequestsStorage:
		return h.storageCalculator.CalculateUsage(ctx, namespace, resourceName)
	case usage.ResourcePersistentVolumeClaims:
		count, err := h.storageCalculator.CalculatePVCCount(ctx, namespace)
		if err != nil {
			return resource.Quantity{}, err
		}
		return *resource.NewQuantity(count, resource.DecimalSI), nil
	default:
		// Check if this is a storage class specific resource
		resourceStr := string(resourceName)

		// Pattern: <storage-class>.storageclass.storage.k8s.io/requests.storage
		if strings.HasSuffix(resourceStr, ".storageclass.storage.k8s.io/requests.storage") {
			storageClass := strings.TrimSuffix(resourceStr, ".storageclass.storage.k8s.io/requests.storage")
			return h.storageCalculator.CalculateStorageClassUsage(ctx, namespace, storageClass)
		}

		// Pattern: <storage-class>.storageclass.storage.k8s.io/persistentvolumeclaims
		if strings.HasSuffix(resourceStr, ".storageclass.storage.k8s.io/persistentvolumeclaims") {
			storageClass := strings.TrimSuffix(resourceStr, ".storageclass.storage.k8s.io/persistentvolumeclaims")
			count, err := h.storageCalculator.CalculateStorageClassCount(ctx, namespace, storageClass)
			if err != nil {
				return resource.Quantity{}, err
			}
			return *resource.NewQuantity(count, resource.DecimalSI), nil
		}

		return resource.Quantity{}, fmt.Errorf("unsupported resource type: %s", resourceName)
	}
}

func getStorageRequest(pvc *corev1.PersistentVolumeClaim) resource.Quantity {
	if pvc.Spec.Resources.Requests != nil {
		if storageRequest, exists := pvc.Spec.Resources.Requests[corev1.ResourceStorage]; exists {
			return storageRequest
		}
	}
	return resource.Quantity{}
}
