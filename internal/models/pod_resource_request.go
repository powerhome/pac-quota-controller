// Package models provides data structures for the PAC Resource Sharing Validation Webhook.
// It defines the core domain models used throughout the application, including
// ClusterResourceQuota and PodResourceRequest.
package models

import (
	corev1 "k8s.io/api/core/v1"
)

// PodResourceRequest represents the resources for a pod based on container limits.
// It is used to validate resource usage against ClusterResourceQuota constraints.
type PodResourceRequest struct {
	// CPUMilliValue is the CPU limit in millicores
	CPUMilliValue int64

	// MemoryBytes is the memory limit in bytes
	MemoryBytes int64
}

// NewPodResourceRequestFromPod creates a PodResourceRequest from a corev1.Pod
// using only the container limits. If a container doesn't specify limits,
// those resources won't be counted towards quota usage.
//
// Parameters:
//   - pod: The Kubernetes pod to extract resource limits from
//
// Returns:
//   - A PodResourceRequest containing the aggregated CPU and memory limits
func NewPodResourceRequestFromPod(pod *corev1.Pod) *PodResourceRequest {
	var cpuMillis int64
	var memoryBytes int64

	// Calculate total resource limits for the pod
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

	return &PodResourceRequest{
		CPUMilliValue: cpuMillis,
		MemoryBytes:   memoryBytes,
	}
}
