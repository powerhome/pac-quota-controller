package pod

import (
	"reflect"

	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/resource"
)

// IsTerminal checks if a pod is in a terminal phase (Succeeded or Failed).
// Terminal pods don't consume compute resources as they're not actively running.
func IsTerminal(pod *corev1.Pod) bool {
	return pod.Status.Phase == corev1.PodSucceeded || pod.Status.Phase == corev1.PodFailed
}

// CalculateResourceUsage calculates the resource usage for a single pod
// by summing all resources from both init containers and regular containers.
func CalculateResourceUsage(pod *corev1.Pod, resourceName corev1.ResourceName) resource.Quantity {
	// Calculate total usage for all containers (init + regular)
	totalUsage := resource.NewQuantity(0, resource.DecimalSI)

	// Add usage from init containers
	for _, container := range pod.Spec.InitContainers {
		containerUsage := getContainerResourceUsage(container, resourceName)
		totalUsage.Add(containerUsage)
	}

	// Add usage from regular containers
	for _, container := range pod.Spec.Containers {
		containerUsage := getContainerResourceUsage(container, resourceName)
		totalUsage.Add(containerUsage)
	}

	return *totalUsage
}

// getContainerResourceUsage extracts the specified resource usage from a container
func getContainerResourceUsage(container corev1.Container, resourceName corev1.ResourceName) resource.Quantity {
	switch resourceName {
	case corev1.ResourceRequestsCPU:
		if cpu, ok := container.Resources.Requests[corev1.ResourceCPU]; ok {
			return cpu
		}
	case corev1.ResourceRequestsMemory:
		if memory, ok := container.Resources.Requests[corev1.ResourceMemory]; ok {
			return memory
		}
	case corev1.ResourceLimitsCPU:
		if cpu, ok := container.Resources.Limits[corev1.ResourceCPU]; ok {
			return cpu
		}
	case corev1.ResourceLimitsMemory:
		if memory, ok := container.Resources.Limits[corev1.ResourceMemory]; ok {
			return memory
		}
	default:
		// Handle hugepages and other resource types
		if resourceValue, ok := container.Resources.Requests[resourceName]; ok {
			return resourceValue
		}
		if resourceValue, ok := container.Resources.Limits[resourceName]; ok {
			return resourceValue
		}
	}

	return resource.Quantity{}
}

// SpecEqual compares two pod specs to determine if they are equivalent.
// This is used to detect if a pod update actually changes the resource requirements.
func SpecEqual(oldPod, newPod *corev1.Pod) bool {
	// TODO: There is probably a better/more efficient way to compare pod specs
	return reflect.DeepEqual(oldPod.Spec, newPod.Spec)
}
