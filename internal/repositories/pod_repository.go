// Package repositories contains implementations for retrieving and managing resources.
// It provides abstractions over Kubernetes API interactions, allowing for clean separation
// between data access and business logic.
package repositories

import (
	"context"
	"fmt"

	"github.com/powerhouse/pac-quota-controller/pkg/kube"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/client-go/kubernetes"
)

// PodRepository provides methods for interacting with Kubernetes pods.
// It handles all direct interaction with the Kubernetes API related to pods,
// including listing pods and calculating resource usage.
type PodRepository struct {
	// Client is the Kubernetes clientset used to interact with the API server
	client *kubernetes.Clientset
}

// NewPodRepository creates a new PodRepository with a configured Kubernetes client.
//
// Returns:
//   - A pointer to a new PodRepository instance
//   - An error if the Kubernetes client creation fails
func NewPodRepository() (*PodRepository, error) {
	client, err := kube.GetClientset()
	if err != nil {
		return nil, fmt.Errorf("failed to get Kubernetes client: %v", err)
	}

	return &PodRepository{
		client: client,
	}, nil
}

// ListByNamespace retrieves all pods in the specified namespace.
//
// Parameters:
//   - ctx: The context for the request
//   - namespace: The namespace to list pods from
//
// Returns:
//   - A slice of Pod objects from the namespace
//   - An error if the API request fails
func (r *PodRepository) ListByNamespace(ctx context.Context, namespace string) ([]corev1.Pod, error) {
	pods, err := r.client.CoreV1().Pods(namespace).List(ctx, metav1.ListOptions{})
	if err != nil {
		return nil, fmt.Errorf("failed to list pods in namespace %s: %w", namespace, err)
	}

	return pods.Items, nil
}

// CalculateNamespaceResourceUsage calculates the total CPU and memory usage for a namespace
// based on container resource limits only.
//
// Parameters:
//   - ctx: The context for the request
//   - namespace: The namespace to calculate resource usage for
//
// Returns:
//   - cpuMillis: The total CPU usage in millicores
//   - memoryBytes: The total memory usage in bytes
//   - err: An error if the calculation fails
func (r *PodRepository) CalculateNamespaceResourceUsage(ctx context.Context, namespace string) (cpuMillis int64, memoryBytes int64, err error) {
	pods, err := r.ListByNamespace(ctx, namespace)
	if err != nil {
		return 0, 0, err
	}

	// Sum up resource limits across all pods
	for _, pod := range pods {
		for _, container := range pod.Spec.Containers {
			if container.Resources.Limits != nil {
				cpuLimit, cpuExists := container.Resources.Limits[corev1.ResourceCPU]
				if cpuExists && !cpuLimit.IsZero() {
					cpuMillis += cpuLimit.MilliValue()
				}

				memoryLimit, memExists := container.Resources.Limits[corev1.ResourceMemory]
				if memExists && !memoryLimit.IsZero() {
					memoryBytes += memoryLimit.Value()
				}
			}
		}
	}

	return cpuMillis, memoryBytes, nil
}

// CalculateResourceUsageForNamespaces calculates the total resource usage across multiple namespaces.
// This is used to evaluate the current resource consumption for a ClusterResourceQuota.
//
// Parameters:
//   - ctx: The context for the request
//   - namespaces: A slice of namespace names to calculate resource usage for
//
// Returns:
//   - cpuMillis: The total CPU usage in millicores across all namespaces
//   - memoryBytes: The total memory usage in bytes across all namespaces
//   - err: An error if the calculation fails for any namespace
func (r *PodRepository) CalculateResourceUsageForNamespaces(ctx context.Context, namespaces []string) (cpuMillis int64, memoryBytes int64, err error) {
	for _, namespace := range namespaces {
		nsCpu, nsMemory, err := r.CalculateNamespaceResourceUsage(ctx, namespace)
		if err != nil {
			return 0, 0, fmt.Errorf("failed to calculate resources for namespace %s: %w", namespace, err)
		}
		cpuMillis += nsCpu
		memoryBytes += nsMemory
	}
	return cpuMillis, memoryBytes, nil
}
