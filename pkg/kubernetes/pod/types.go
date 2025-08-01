package pod

import (
	"github.com/powerhome/pac-quota-controller/pkg/kubernetes/usage"
)

//go:generate mockery

// PodResourceCalculatorInterface defines the interface for pod resource calculations
type PodResourceCalculatorInterface interface {
	usage.ResourceCalculatorInterface
	// Additional pod-specific methods can be added here
}
