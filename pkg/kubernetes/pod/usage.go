package pod

import (
	"context"

	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/resource"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/log"
)

// ComputeResourceCalculator handles compute resource usage calculations for pods
type ComputeResourceCalculator struct {
	client.Client
}

// NewComputeResourceCalculator creates a new ComputeResourceCalculator
func NewComputeResourceCalculator(c client.Client) *ComputeResourceCalculator {
	return &ComputeResourceCalculator{Client: c}
}

// CalculateComputeUsage calculates the usage for compute resources (CPU/Memory requests or limits)
// across all non-terminal pods in the specified namespace
func (c *ComputeResourceCalculator) CalculateComputeUsage(
	ctx context.Context,
	namespace string,
	resourceName corev1.ResourceName,
) (resource.Quantity, error) {
	logger := log.FromContext(ctx).WithValues("namespace", namespace, "resource", resourceName)

	podList := &corev1.PodList{}
	listOpts := &client.ListOptions{
		Namespace: namespace,
	}

	if err := c.List(ctx, podList, listOpts); err != nil {
		logger.Error(err, "Failed to list pods")
		return resource.Quantity{}, err
	}

	totalUsage := resource.NewQuantity(0, resource.DecimalSI)

	for i := range podList.Items {
		pod := &podList.Items[i]

		// Skip terminal pods (Succeeded or Failed) as they don't consume resources
		if IsTerminal(pod) {
			continue
		}

		podUsage := CalculateResourceUsage(pod, resourceName)
		totalUsage.Add(podUsage)
	}

	logger.V(1).Info("Calculated compute usage", "totalUsage", totalUsage.String(), "podCount", len(podList.Items))
	return *totalUsage, nil
}
