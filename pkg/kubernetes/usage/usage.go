/*
Copyright 2025.

Licensed under the Apache License, Version 2.0 (the "License");
you may not use this file except in compliance with the License.
You may obtain a copy of the License at

    http://www.apache.org/licenses/LICENSE-2.0

Unless required by applicable law or agreed to in writing, software
distributed under the License is distributed on an "AS IS" BASIS,
WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
See the License for the specific language governing permissions and
limitations under the License.
*/

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
	// CPU resources
	ResourceRequestsCPU = corev1.ResourceRequestsCPU
	ResourceLimitsCPU   = corev1.ResourceLimitsCPU
	ResourceCPU         = corev1.ResourceCPU

	// Memory resources
	ResourceRequestsMemory = corev1.ResourceRequestsMemory
	ResourceLimitsMemory   = corev1.ResourceLimitsMemory
	ResourceMemory         = corev1.ResourceMemory

	// Storage resources
	ResourceRequestsStorage = corev1.ResourceRequestsStorage
	ResourceStorage         = corev1.ResourceStorage

	// Ephemeral storage
	ResourceRequestsEphemeralStorage = corev1.ResourceRequestsEphemeralStorage
	ResourceLimitsEphemeralStorage   = corev1.ResourceLimitsEphemeralStorage
	ResourceEphemeralStorage         = corev1.ResourceEphemeralStorage

	// Count resources
	ResourcePods                   = corev1.ResourcePods
	ResourcePersistentVolumeClaims = corev1.ResourcePersistentVolumeClaims
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
