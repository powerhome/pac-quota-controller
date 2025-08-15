package pod

import (
	"context"

	"github.com/powerhome/pac-quota-controller/pkg/kubernetes/usage"
)

//go:generate mockery

// PodResourceCalculatorInterface defines the interface for pod resource calculations
type PodResourceCalculatorInterface interface {
	usage.ResourceCalculatorInterface
	// CalculatePodCount calculates the number of non-terminal pods in a namespace
	CalculatePodCount(ctx context.Context, namespace string) (int64, error)
}
