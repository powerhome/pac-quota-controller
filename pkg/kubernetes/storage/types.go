package storage

import (
	"context"

	"k8s.io/apimachinery/pkg/api/resource"

	"github.com/powerhome/pac-quota-controller/pkg/kubernetes/usage"
)

//go:generate mockery

// StorageResourceCalculatorInterface defines the interface for storage resource calculations
type StorageResourceCalculatorInterface interface {
	usage.ResourceCalculatorInterface
	// Additional storage-specific methods
	CalculateStorageClassUsage(ctx context.Context, namespace, storageClass string) (resource.Quantity, error)
	CalculateStorageClassCount(ctx context.Context, namespace, storageClass string) (int64, error)
	CalculatePVCCount(ctx context.Context, namespace string) (int64, error)
}
