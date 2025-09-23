package pod

import (
	"context"
	"strings"

	"go.uber.org/zap"
	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/equality"
	"k8s.io/apimachinery/pkg/api/resource"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/client-go/kubernetes"

	"github.com/powerhome/pac-quota-controller/pkg/kubernetes/usage"
)

var log = zap.NewNop()

// PodResourceCalculator handles compute resource usage calculations for pods
type PodResourceCalculator struct {
	usage.BaseResourceCalculator
}

// Ensure PodResourceCalculator implements PodResourceCalculatorInterface
var _ PodResourceCalculatorInterface = &PodResourceCalculator{}

// NewPodResourceCalculator creates a new PodResourceCalculator
func NewPodResourceCalculator(c kubernetes.Interface) *PodResourceCalculator {
	return &PodResourceCalculator{
		BaseResourceCalculator: *usage.NewBaseResourceCalculator(c),
	}
}

// IsPodTerminal checks if a pod is in a terminal phase (Succeeded or Failed).
// Terminal pods don't consume compute resources as they're not actively running.
func IsPodTerminal(pod *corev1.Pod) bool {
	if pod == nil {
		return false
	}
	return pod.Status.Phase == corev1.PodSucceeded || pod.Status.Phase == corev1.PodFailed
}

// CalculatePodUsage calculates the resource usage for a single pod
// by summing all resources from both init containers and regular containers.
func CalculatePodUsage(pod *corev1.Pod, resourceName corev1.ResourceName) resource.Quantity {
	if pod == nil {
		return resource.Quantity{}
	}

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
	case corev1.ResourceRequestsEphemeralStorage:
		if ephemeralStorage, ok := container.Resources.Requests[corev1.ResourceEphemeralStorage]; ok {
			return ephemeralStorage
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
		// Handle extended resources with 'requests.' prefix
		s := string(resourceName)
		if strings.HasPrefix(s, "requests.") {
			extName := corev1.ResourceName(s[len("requests."):])
			if resourceValue, ok := container.Resources.Requests[extName]; ok {
				return resourceValue
			}
		}
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

// CalculateUsage calculates the usage for compute resources (CPU/Memory requests or limits)
// across all non-terminal pods in the specified namespace
func (c *PodResourceCalculator) CalculateUsage(
	ctx context.Context,
	namespace string,
	resourceName corev1.ResourceName,
) (resource.Quantity, error) {
	// Handle pod count separately
	if resourceName == usage.ResourcePods {
		podCount, err := c.CalculatePodCount(ctx, namespace)
		if err != nil {
			return resource.Quantity{}, err
		}
		return *resource.NewQuantity(podCount, resource.DecimalSI), nil
	}

	podList, err := c.Client.CoreV1().Pods(namespace).List(ctx, metav1.ListOptions{})
	if err != nil {
		log.Error("Failed to list pods", zap.Error(err))
		return resource.Quantity{}, err
	}

	totalUsage := resource.NewQuantity(0, resource.DecimalSI)

	for i := range podList.Items {
		pod := &podList.Items[i]

		// Skip terminal pods (Succeeded or Failed) as they don't consume resources
		if IsPodTerminal(pod) {
			continue
		}

		podUsage := CalculatePodUsage(pod, resourceName)
		totalUsage.Add(podUsage)
	}

	log.Debug("Calculated compute usage",
		zap.String("totalUsage", totalUsage.String()),
		zap.Int("podCount", len(podList.Items)))
	return *totalUsage, nil
}

// CalculateTotalUsage calculates the total usage across all resources in a namespace
func (c *PodResourceCalculator) CalculateTotalUsage(ctx context.Context, namespace string) (
	map[corev1.ResourceName]resource.Quantity, error) {
	result := make(map[corev1.ResourceName]resource.Quantity)

	// Calculate usage for common resources
	resources := []corev1.ResourceName{
		usage.ResourceRequestsCPU,
		usage.ResourceRequestsMemory,
		usage.ResourceLimitsCPU,
		usage.ResourceLimitsMemory,
		usage.ResourceRequestsEphemeralStorage,
		usage.ResourceLimitsEphemeralStorage,
		usage.ResourcePods, // Add pod count
	}

	for _, resourceName := range resources {
		resourceUsage, err := c.CalculateUsage(ctx, namespace, resourceName)
		if err != nil {
			return nil, err
		}
		result[resourceName] = resourceUsage
	}

	return result, nil
}

// CalculatePodCount calculates the number of non-terminal pods in a namespace
func (c *PodResourceCalculator) CalculatePodCount(ctx context.Context, namespace string) (int64, error) {
	podList, err := c.Client.CoreV1().Pods(namespace).List(ctx, metav1.ListOptions{})
	if err != nil {
		log.Error("Failed to list pods", zap.Error(err))
		return 0, err
	}

	count := int64(0)
	for i := range podList.Items {
		pod := &podList.Items[i]

		// Skip terminal pods (Succeeded or Failed) as they don't consume resources
		if IsPodTerminal(pod) {
			continue
		}

		count++
	}

	log.Debug("Calculated pod count",
		zap.String("namespace", namespace),
		zap.Int64("podCount", count))
	return count, nil
}

// SpecEqual compares two pod specs to determine if they are equivalent.
// This is used to detect if a pod update actually changes the resource requirements.
func SpecEqual(oldPod, newPod *corev1.Pod) bool {
	if oldPod == nil && newPod == nil {
		return true
	}
	if oldPod == nil || newPod == nil {
		return false
	}
	return equality.Semantic.DeepEqual(oldPod.Spec, newPod.Spec)
}
