package usage

import (
	"strings"

	corev1 "k8s.io/api/core/v1"
)

// Core resource names used across the application.
var (
	// Core compute resources
	ResourceRequestsCPU = corev1.ResourceRequestsCPU
	ResourceLimitsCPU   = corev1.ResourceLimitsCPU
	ResourceCPU         = corev1.ResourceCPU

	// Core memory resources
	ResourceRequestsMemory = corev1.ResourceRequestsMemory
	ResourceLimitsMemory   = corev1.ResourceLimitsMemory
	ResourceMemory         = corev1.ResourceMemory

	// Core storage resources
	ResourceRequestsStorage = corev1.ResourceRequestsStorage
	ResourceStorage         = corev1.ResourceStorage

	// Ephemeral storage resources
	ResourceRequestsEphemeralStorage = corev1.ResourceRequestsEphemeralStorage
	ResourceLimitsEphemeralStorage   = corev1.ResourceLimitsEphemeralStorage
	ResourceEphemeralStorage         = corev1.ResourceEphemeralStorage

	// Core countable resources
	ResourcePods                   = corev1.ResourcePods
	ResourcePersistentVolumeClaims = corev1.ResourcePersistentVolumeClaims
	ResourceConfigMaps             = corev1.ResourceConfigMaps
	ResourceReplicationControllers = corev1.ResourceReplicationControllers
	ResourceSecrets                = corev1.ResourceSecrets

	// Additional Kubernetes resource counts
	ResourceDeployments              = corev1.ResourceName("deployments.apps")
	ResourceStatefulSets             = corev1.ResourceName("statefulsets.apps")
	ResourceDaemonSets               = corev1.ResourceName("daemonsets.apps")
	ResourceJobs                     = corev1.ResourceName("jobs.batch")
	ResourceCronJobs                 = corev1.ResourceName("cronjobs.batch")
	ResourceHorizontalPodAutoscalers = corev1.ResourceName("horizontalpodautoscalers.autoscaling")
	ResourceIngresses                = corev1.ResourceName("ingresses.networking.k8s.io")

	// Service-related resources
	ResourceServices              = corev1.ResourceServices
	ResourceServicesLoadBalancers = corev1.ResourceServicesLoadBalancers
	ResourceServicesNodePorts     = corev1.ResourceServicesNodePorts
)

// GetBaseResourceName returns the base resource name for a given resource name.
// For example, it maps 'requests.cpu' or 'limits.cpu' to 'cpu'.
func GetBaseResourceName(resourceName corev1.ResourceName) corev1.ResourceName {
	s := string(resourceName)
	if strings.HasPrefix(s, "requests.") {
		return corev1.ResourceName(s[len("requests."):])
	}
	if strings.HasPrefix(s, "limits.") {
		return corev1.ResourceName(s[len("limits."):])
	}
	return resourceName
}
