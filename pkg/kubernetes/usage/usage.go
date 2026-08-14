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

// PodEligibleResources are the resources whose usage is computed from pod specs.
// They're also the only resources a pod scope (other than BestEffort, which
// further narrows to just pods) may restrict in spec.hard — ephemeral-storage
// is pod-derived too but deliberately excluded from scope eligibility,
// matching upstream's podComputeQuotaResources.
var PodEligibleResources = map[corev1.ResourceName]bool{
	ResourcePods:           true,
	ResourceRequestsCPU:    true,
	ResourceRequestsMemory: true,
	ResourceLimitsCPU:      true,
	ResourceLimitsMemory:   true,
}

// ServiceResources are resource names whose usage is computed from Services.
var ServiceResources = map[corev1.ResourceName]bool{
	ResourceServices:              true,
	ResourceServicesLoadBalancers: true,
	ResourceServicesNodePorts:     true,
}

// PVCResources are resource names whose usage is computed from
// PersistentVolumeClaims, excluding the per-storage-class variants
// (*.storageclass.storage.k8s.io/*), which are matched by suffix instead.
var PVCResources = map[corev1.ResourceName]bool{
	ResourceRequestsStorage:        true,
	ResourcePersistentVolumeClaims: true,
}

// ObjectCountResources are resource names tracked via a plain List+len object count.
var ObjectCountResources = map[corev1.ResourceName]bool{
	ResourceConfigMaps:               true,
	ResourceSecrets:                  true,
	ResourceReplicationControllers:   true,
	ResourceDeployments:              true,
	ResourceStatefulSets:             true,
	ResourceDaemonSets:               true,
	ResourceJobs:                     true,
	ResourceCronJobs:                 true,
	ResourceHorizontalPodAutoscalers: true,
	ResourceIngresses:                true,
}

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
