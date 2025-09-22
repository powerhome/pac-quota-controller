package services

import (
	"context"
	"fmt"

	"github.com/powerhome/pac-quota-controller/pkg/kubernetes/usage"
	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/resource"

	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/client-go/kubernetes"
)

// CountServices returns the total number of services and a breakdown by type in the namespace (public interface).
func (c *ServiceResourceCalculator) CountServices(ctx context.Context, namespace string) (int64, map[corev1.ServiceType]int64, error) {
	return c.countServicesByType(ctx, namespace)
}

// Ensure ServiceResourceCalculator implements usage.ResourceCalculatorInterface
var _ usage.ResourceCalculatorInterface = &ServiceResourceCalculator{}

// ServiceResourceCalculator provides methods for counting services and subtypes in a namespace.
type ServiceResourceCalculator struct {
	Client kubernetes.Interface
}

// NewServiceResourceCalculator creates a new ServiceResourceCalculator.
func NewServiceResourceCalculator(c kubernetes.Interface) *ServiceResourceCalculator {
	return &ServiceResourceCalculator{Client: c}
}

// resourceNameToServiceType maps usage resource names to corev1.ServiceType values.
var resourceNameToServiceType = map[corev1.ResourceName]corev1.ServiceType{
	usage.ResourceServicesLoadBalancers: corev1.ServiceTypeLoadBalancer,
	usage.ResourceServicesNodePorts:     corev1.ServiceTypeNodePort,
}

// CalculateUsage returns the usage count for a specific service type resource in the namespace.
func (c *ServiceResourceCalculator) CalculateUsage(ctx context.Context, namespace string, resourceName corev1.ResourceName) (resource.Quantity, error) {
	total, byType, err := c.countServicesByType(ctx, namespace)
	if err != nil {
		return resource.Quantity{}, err
	}

	switch resourceName {
	case usage.ResourceServices:
		return *resource.NewQuantity(total, resource.DecimalSI), nil
	case usage.ResourceServicesLoadBalancers, usage.ResourceServicesNodePorts:
		serviceType, ok := resourceNameToServiceType[resourceName]
		if !ok {
			return resource.Quantity{}, nil
		}
		return *resource.NewQuantity(byType[serviceType], resource.DecimalSI), nil
	default:
		return resource.Quantity{}, nil
	}
}

// CalculateTotalUsage calculates the total usage for all supported service count resources in a namespace.
func (c *ServiceResourceCalculator) CalculateTotalUsage(ctx context.Context, namespace string) (map[corev1.ResourceName]resource.Quantity, error) {
	total, byType, err := c.countServicesByType(ctx, namespace)
	if err != nil {
		return nil, err
	}
	result := map[corev1.ResourceName]resource.Quantity{
		usage.ResourceServices:              *resource.NewQuantity(total, resource.DecimalSI),
		usage.ResourceServicesLoadBalancers: *resource.NewQuantity(byType[corev1.ServiceTypeLoadBalancer], resource.DecimalSI),
		usage.ResourceServicesNodePorts:     *resource.NewQuantity(byType[corev1.ServiceTypeNodePort], resource.DecimalSI),
	}
	return result, nil
}

// CountServices returns the total number of services and a breakdown by type in the namespace.
func (c *ServiceResourceCalculator) countServicesByType(ctx context.Context, namespace string) (total int64, byType map[corev1.ServiceType]int64, err error) {
	serviceList, err := c.Client.CoreV1().Services(namespace).List(ctx, metav1.ListOptions{})
	if err != nil {
		return 0, nil, err
	}
	byType = map[corev1.ServiceType]int64{
		corev1.ServiceTypeNodePort:     0,
		corev1.ServiceTypeLoadBalancer: 0,
	}
	// DEBUG: Print all services being counted
	fmt.Printf("[DEBUG] Counting services in namespace %s:\n", namespace)
	for _, svc := range serviceList.Items {
		fmt.Printf("[DEBUG]   SERVICE: %s/%s type=%s\n", svc.Namespace, svc.Name, svc.Spec.Type)
		byType[svc.Spec.Type]++
	}
	total = int64(len(serviceList.Items))
	fmt.Printf("[DEBUG] Total services counted: %d\n", total)
	return total, byType, nil
}
