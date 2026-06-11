package services

import (
	"context"

	"github.com/powerhome/pac-quota-controller/pkg/kubernetes/quota"
	"github.com/powerhome/pac-quota-controller/pkg/kubernetes/usage"
	"go.uber.org/zap"
	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/resource"
	"sigs.k8s.io/controller-runtime/pkg/client"
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
	Client client.Client
	logger *zap.Logger
}

// NewServiceResourceCalculator creates a new ServiceResourceCalculator.
func NewServiceResourceCalculator(c client.Client, logger *zap.Logger) *ServiceResourceCalculator {
	if logger == nil {
		logger = zap.NewNop()
	}
	return &ServiceResourceCalculator{
		Client: c,
		logger: logger.Named("service-calculator"),
	}
}

// CalculateUsageFromServices calculates service quota usage from an already loaded service list.
func CalculateUsageFromServices(svcs []corev1.Service, resourceName corev1.ResourceName) resource.Quantity {
	var count int64

	switch resourceName {
	case usage.ResourceServices:
		count = int64(len(svcs))
	case usage.ResourceServicesLoadBalancers:
		for i := range svcs {
			if svcs[i].Spec.Type == corev1.ServiceTypeLoadBalancer {
				count++
			}
		}
	case usage.ResourceServicesNodePorts:
		for i := range svcs {
			if svcs[i].Spec.Type == corev1.ServiceTypeNodePort {
				count++
			}
		}
	}

	return *resource.NewQuantity(count, resource.DecimalSI)
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
	correlationID := quota.GetCorrelationID(ctx)
	serviceList := &corev1.ServiceList{}
	if err := c.Client.List(ctx, serviceList, client.InNamespace(namespace)); err != nil {
		c.logger.Error("Failed to list services",
			zap.String("correlation_id", correlationID),
			zap.String("namespace", namespace),
			zap.Error(err))
		return resource.Quantity{}, err
	}

	usageQty := CalculateUsageFromServices(serviceList.Items, resourceName)

	c.logger.Debug("Calculated service usage",
		zap.String("correlation_id", correlationID),
		zap.String("namespace", namespace),
		zap.String("resource", string(resourceName)),
		zap.String("usage", usageQty.String()),
		zap.Int("service_count", len(serviceList.Items)))

	return usageQty, nil
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
	serviceList := &corev1.ServiceList{}
	if err = c.Client.List(ctx, serviceList, client.InNamespace(namespace)); err != nil {
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
