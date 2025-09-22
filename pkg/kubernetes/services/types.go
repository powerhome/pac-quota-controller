//go:generate mockery --name=ServiceResourceCalculatorInterface
package services

import (
	"context"

	"github.com/powerhome/pac-quota-controller/pkg/kubernetes/usage"
	corev1 "k8s.io/api/core/v1"
)

//go:generate mockery

// ServiceResourceCalculatorInterface defines the interface for service resource calculations
type ServiceResourceCalculatorInterface interface {
	usage.ResourceCalculatorInterface
	CountServices(ctx context.Context, namespace string) (total int64, byType map[corev1.ServiceType]int64, err error)
}
