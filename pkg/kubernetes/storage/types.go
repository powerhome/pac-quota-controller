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
}
