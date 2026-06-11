package services

import (
	"github.com/powerhome/pac-quota-controller/pkg/kubernetes/usage"
	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/resource"
)

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
