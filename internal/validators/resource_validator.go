// Package validators contains validation logic for various resources
package validators

import (
	"context"
	"fmt"

	"github.com/powerhome/pac-quota-controller/internal/repositories"
	"github.com/powerhome/pac-quota-controller/pkg/logging"
	"go.uber.org/zap"
	"k8s.io/apimachinery/pkg/api/resource"
)

var (
	logger = logging.NewLogger()
)

// ResourceValidator provides methods to validate resource usage in namespaces
type ResourceValidator struct {
	podRepo *repositories.PodRepository
}

// NewResourceValidator creates a new instance of ResourceValidator
func NewResourceValidator() (*ResourceValidator, error) {
	podRepo, err := repositories.NewPodRepository()
	if err != nil {
		return nil, fmt.Errorf("failed to create pod repository: %v", err)
	}

	return &ResourceValidator{
		podRepo: podRepo,
	}, nil
}

// ValidateQuotaNotExceeded checks if adding a new pod would exceed the quota limits
func (v *ResourceValidator) ValidateQuotaNotExceeded(
	ctx context.Context,
	namespaces []string,
	cpuLimitStr string,
	memoryLimitStr string,
	additionalCPUMillis int64,
	additionalMemoryBytes int64,
) error {
	// Get current resource usage for all namespaces in the quota
	currentCPUMillis, currentMemoryBytes, err := v.podRepo.CalculateResourceUsageForNamespaces(ctx, namespaces)
	if err != nil {
		return fmt.Errorf("failed to calculate current resource usage: %v", err)
	}

	// Add the additional resources
	totalCPUMillis := currentCPUMillis + additionalCPUMillis
	totalMemoryBytes := currentMemoryBytes + additionalMemoryBytes

	// Parse the quota limits - specifically using MilliValue for CPU
	cpuLimitMillis, err := ParseCPUMilliValue(cpuLimitStr)
	if err != nil {
		return fmt.Errorf("invalid CPU limit: %v", err)
	}

	memoryLimit, err := ParseMemoryBytes(memoryLimitStr)
	if err != nil {
		return fmt.Errorf("invalid memory limit: %v", err)
	}

	logger.Debug("resource validation",
		zap.Int64("currentCPUMillis", currentCPUMillis),
		zap.Int64("additionalCPUMillis", additionalCPUMillis),
		zap.Int64("totalCPUMillis", totalCPUMillis),
		zap.Int64("cpuLimitMillis", cpuLimitMillis),
		zap.Int64("currentMemoryBytes", currentMemoryBytes),
		zap.Int64("additionalMemoryBytes", additionalMemoryBytes),
		zap.Int64("totalMemoryBytes", totalMemoryBytes),
		zap.Int64("memoryLimit", memoryLimit))

	// Check if the total would exceed the limits
	if totalCPUMillis > cpuLimitMillis {
		return fmt.Errorf("CPU limit of %s would be exceeded (current: %dm, additional: %dm, limit: %dm)",
			cpuLimitStr, currentCPUMillis, additionalCPUMillis, cpuLimitMillis)
	}
	logger.Debug("CPU limit check passed")
	if totalMemoryBytes > memoryLimit {
		return fmt.Errorf("memory limit of %s would be exceeded (current: %d, additional: %d, limit: %d)",
			memoryLimitStr, currentMemoryBytes, additionalMemoryBytes, memoryLimit)
	}

	return nil
}

// ParseCPUMilliValue parses a CPU resource quantity string (e.g., "500m", "1") into millicore value
func ParseCPUMilliValue(cpuStr string) (int64, error) {
	quantity, err := resource.ParseQuantity(cpuStr)
	if err != nil {
		return 0, fmt.Errorf("failed to parse CPU quantity %s: %w", cpuStr, err)
	}

	// Convert to millicores (1 CPU = 1000m)
	milliValue := quantity.MilliValue()
	return milliValue, nil
}

// ParseMemoryBytes parses a memory resource quantity string (e.g., "1Gi", "500Mi") into its byte value
func ParseMemoryBytes(memoryStr string) (int64, error) {
	quantity, err := resource.ParseQuantity(memoryStr)
	if err != nil {
		return 0, fmt.Errorf("failed to parse memory quantity %s: %w", memoryStr, err)
	}
	return quantity.Value(), nil
}
