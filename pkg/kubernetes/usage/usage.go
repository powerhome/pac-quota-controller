package usage

import (
	"context"

	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/resource"
	"k8s.io/client-go/kubernetes"
)

// ResourceCalculatorInterface defines the common interface for resource usage calculations
type ResourceCalculatorInterface interface {
	// CalculateUsage calculates the total usage for a specific resource in a namespace
	CalculateUsage(ctx context.Context, namespace string, resourceName corev1.ResourceName) (resource.Quantity, error)

	// CalculateTotalUsage calculates the total usage across all resources in a namespace
	CalculateTotalUsage(ctx context.Context, namespace string) (map[corev1.ResourceName]resource.Quantity, error)
}

// BaseResourceCalculator provides common functionality for resource calculators
type BaseResourceCalculator struct {
	Client kubernetes.Interface
}

// NewBaseResourceCalculator creates a new base resource calculator
func NewBaseResourceCalculator(c kubernetes.Interface) *BaseResourceCalculator {
	return &BaseResourceCalculator{
		Client: c,
	}
}

// ResourceUsage represents usage information for a specific resource
type ResourceUsage struct {
	ResourceName corev1.ResourceName
	Quantity     resource.Quantity
	Error        error
}

// UsageResult represents the result of usage calculations
type UsageResult struct {
	Namespace     string
	Usage         map[corev1.ResourceName]resource.Quantity
	Errors        []error
	ResourceCount int
}

// NewUsageResult creates a new usage result
func NewUsageResult(namespace string) *UsageResult {
	return &UsageResult{
		Namespace: namespace,
		Usage:     make(map[corev1.ResourceName]resource.Quantity),
		Errors:    make([]error, 0),
	}
}

// AddUsage adds a resource usage to the result
func (r *UsageResult) AddUsage(resourceName corev1.ResourceName, quantity resource.Quantity) {
	r.Usage[resourceName] = quantity
	r.ResourceCount++
}

// AddError adds an error to the result
func (r *UsageResult) AddError(err error) {
	r.Errors = append(r.Errors, err)
}

// HasErrors returns true if there are any errors
func (r *UsageResult) HasErrors() bool {
	return len(r.Errors) > 0
}

// GetTotalUsage returns the total usage for a specific resource
func (r *UsageResult) GetTotalUsage(resourceName corev1.ResourceName) resource.Quantity {
	if usage, exists := r.Usage[resourceName]; exists {
		return usage
	}
	return resource.Quantity{}
}

// Core resource names used across the application
var (
	// Core compute resources
	ResourceRequestsCPU = corev1.ResourceRequestsCPU
	ResourceLimitsCPU   = corev1.ResourceLimitsCPU
	ResourceCPU         = corev1.ResourceCPU

	// Core memory resources
	ResourceRequestsMemory = corev1.ResourceRequestsMemory
	ResourceLimitsMemory   = corev1.ResourceLimitsMemory
	ResourceMemory         = corev1.ResourceMemory

	// Core storage resources
	ResourceRequestsStorage = corev1.ResourceRequestsStorage
	ResourceStorage         = corev1.ResourceStorage

	// Ephemeral storage resources
	ResourceRequestsEphemeralStorage = corev1.ResourceRequestsEphemeralStorage
	ResourceLimitsEphemeralStorage   = corev1.ResourceLimitsEphemeralStorage
	ResourceEphemeralStorage         = corev1.ResourceEphemeralStorage

	// Core countable resources
	ResourcePods                   = corev1.ResourcePods
	ResourcePersistentVolumeClaims = corev1.ResourcePersistentVolumeClaims
	ResourceConfigMaps             = corev1.ResourceConfigMaps
	ResourceReplicationControllers = corev1.ResourceReplicationControllers
	ResourceSecrets                = corev1.ResourceSecrets

	// Additional Kubernetes resource counts
	ResourceDeployments              = corev1.ResourceName("deployments.apps")
	ResourceStatefulSets             = corev1.ResourceName("statefulsets.apps")
	ResourceDaemonSets               = corev1.ResourceName("daemonsets.apps")
	ResourceJobs                     = corev1.ResourceName("jobs.batch")
	ResourceCronJobs                 = corev1.ResourceName("cronjobs.batch")
	ResourceHorizontalPodAutoscalers = corev1.ResourceName("horizontalpodautoscalers.autoscaling")
	ResourceIngresses                = corev1.ResourceName("ingresses.networking.k8s.io")

	// Service-related resources
	ResourceServices              = corev1.ResourceServices
	ResourceServicesLoadBalancers = corev1.ResourceServicesLoadBalancers
	ResourceServicesNodePorts     = corev1.ResourceServicesNodePorts
)

// Common resource calculation utilities
func NewQuantity(value int64, format resource.Format) resource.Quantity {
	return *resource.NewQuantity(value, format)
}

func NewDecimalQuantity(value int64, format resource.Format) resource.Quantity {
	return *resource.NewQuantity(value, format)
}

func NewBinaryQuantity(value int64, format resource.Format) resource.Quantity {
	return *resource.NewQuantity(value, format)
}
