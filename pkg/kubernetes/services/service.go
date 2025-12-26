package services

import (
	"context"

	"github.com/powerhome/pac-quota-controller/pkg/kubernetes/quota"
	"github.com/powerhome/pac-quota-controller/pkg/kubernetes/usage"
	"go.uber.org/zap"
	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/resource"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/client-go/kubernetes"
)

// CountServices returns the total number of services and a breakdown by type in the namespace (public interface).
func (c *ServiceResourceCalculator) CountServices(
	ctx context.Context,
	namespace string,
) (
	int64,
	map[corev1.ServiceType]int64,
	error,
) {
	return c.countServicesByType(ctx, namespace)
}

// ServiceResourceCalculator provides methods for counting services and subtypes in a namespace.
type ServiceResourceCalculator struct {
	Client kubernetes.Interface
	logger *zap.Logger
}

// NewServiceResourceCalculator creates a new ServiceResourceCalculator.
func NewServiceResourceCalculator(c kubernetes.Interface, logger *zap.Logger) *ServiceResourceCalculator {
	if logger == nil {
		logger = zap.NewNop()
	}
	return &ServiceResourceCalculator{
		Client: c,
		logger: logger.Named("service-calculator"),
	}
}

// resourceNameToServiceType maps usage resource names to corev1.ServiceType values.
var resourceNameToServiceType = map[corev1.ResourceName]corev1.ServiceType{
	usage.ResourceServicesLoadBalancers: corev1.ServiceTypeLoadBalancer,
	usage.ResourceServicesNodePorts:     corev1.ServiceTypeNodePort,
}

// CalculateUsage returns the usage count for a specific service type resource in the namespace.
func (c *ServiceResourceCalculator) CalculateUsage(
	ctx context.Context,
	namespace string,
	resourceName corev1.ResourceName,
) (
	resource.Quantity,
	error,
) {
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

// CountServices returns the total number of services and a breakdown by type in the namespace.
func (c *ServiceResourceCalculator) countServicesByType(
	ctx context.Context,
	namespace string,
) (
	total int64,
	byType map[corev1.ServiceType]int64,
	err error,
) {
	correlationID := quota.GetCorrelationID(ctx)
	serviceList, err := c.Client.CoreV1().Services(namespace).List(ctx, metav1.ListOptions{})
	if err != nil {
		c.logger.Error("Failed to list services",
			zap.String("correlation_id", correlationID),
			zap.String("namespace", namespace),
			zap.Error(err))
		return 0, nil, err
	}
	byType = map[corev1.ServiceType]int64{
		corev1.ServiceTypeNodePort:     0,
		corev1.ServiceTypeLoadBalancer: 0,
	}
	for _, svc := range serviceList.Items {
		byType[svc.Spec.Type]++
	}
	total = int64(len(serviceList.Items))
	c.logger.Debug("Calculated service usage",
		zap.String("correlation_id", correlationID),
		zap.String("namespace", namespace),
		zap.Int64("total_services", total),
		zap.Int64("load_balancer_count", byType[corev1.ServiceTypeLoadBalancer]),
		zap.Int64("node_port_count", byType[corev1.ServiceTypeNodePort]))
	return total, byType, nil
}
