package v1alpha1

import (
	"fmt"

	"go.uber.org/zap"
	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/resource"

	"github.com/powerhome/pac-quota-controller/pkg/kubernetes/quota"
)

// validateCRQResourceQuotaWithNamespace is a shared function for validating resource quotas across webhooks with actual namespace object
func validateCRQResourceQuotaWithNamespace(
	crqClient *quota.CRQClient,
	ns *corev1.Namespace,
	resourceName corev1.ResourceName,
	requestedQuantity resource.Quantity,
	calculateCurrentUsage func(string, corev1.ResourceName) (resource.Quantity, error),
	log *zap.Logger,
) error {
	// If no CRQ client is available, skip validation
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
	crq, err := crqClient.GetCRQByNamespace(ns)
	if err != nil {
		log.Error("Failed to get CRQ for namespace",
			zap.String("namespace", ns.Name),
			zap.Error(err))
		return fmt.Errorf("failed to get CRQ for namespace %s: %w", ns.Name, err)
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

		// Calculate current usage for this resource
		currentUsage, err := calculateCurrentUsage(ns.Name, resourceName)
		if err != nil {
			log.Error("Failed to calculate current usage",
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
			return fmt.Errorf("resource quota would be exceeded: requested %s, current usage %s, quota limit %s, total would be %s",
				requestedQuantity.String(), currentUsage.String(), quotaLimit.String(), totalUsage.String())
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
