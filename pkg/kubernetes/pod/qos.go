package pod

import (
	corev1 "k8s.io/api/core/v1"
)

// isSupportedQoSComputeResource reports whether a resource participates in QoS classification.
func isSupportedQoSComputeResource(name corev1.ResourceName) bool {
	return name == corev1.ResourceCPU || name == corev1.ResourceMemory
}

// ComputePodQOS mirrors upstream qos.ComputePodQOS, always deriving the class from
// the pod spec (never status.qosClass) so admission and reconciliation agree.
// Only cpu and memory participate; init containers count, ephemeral containers cannot
// set resources. Pod-level resources take precedence over per-container ones when set.
// Ported from k8s.io/kubernetes/pkg/apis/core/v1/helper/qos/qos.go @ release-1.36;
// re-diff against that file (not master, which has since diverged) on client-go bumps.
func ComputePodQOS(p *corev1.Pod) corev1.PodQOSClass {
	if p == nil {
		return corev1.PodQOSBestEffort
	}

	var resourceSets []corev1.ResourceRequirements
	if p.Spec.Resources != nil && (len(p.Spec.Resources.Requests) > 0 || len(p.Spec.Resources.Limits) > 0) {
		resourceSets = []corev1.ResourceRequirements{*p.Spec.Resources}
	} else {
		resourceSets = make([]corev1.ResourceRequirements, 0, len(p.Spec.Containers)+len(p.Spec.InitContainers))
		for _, c := range p.Spec.Containers {
			resourceSets = append(resourceSets, c.Resources)
		}
		for _, c := range p.Spec.InitContainers {
			resourceSets = append(resourceSets, c.Resources)
		}
	}

	requests := corev1.ResourceList{}
	limits := corev1.ResourceList{}
	isGuaranteed := true

	for _, res := range resourceSets {
		for name, quantity := range res.Requests {
			if !isSupportedQoSComputeResource(name) || quantity.Sign() != 1 {
				continue
			}
			delta := quantity.DeepCopy()
			if prev, exists := requests[name]; exists {
				delta.Add(prev)
			}
			requests[name] = delta
		}
		var hasCPULimit, hasMemoryLimit bool
		for name, quantity := range res.Limits {
			if !isSupportedQoSComputeResource(name) || quantity.Sign() != 1 {
				continue
			}
			switch name {
			case corev1.ResourceCPU:
				hasCPULimit = true
			case corev1.ResourceMemory:
				hasMemoryLimit = true
			}
			delta := quantity.DeepCopy()
			if prev, exists := limits[name]; exists {
				delta.Add(prev)
			}
			limits[name] = delta
		}
		if !hasCPULimit || !hasMemoryLimit {
			isGuaranteed = false
		}
	}

	if len(requests) == 0 && len(limits) == 0 {
		return corev1.PodQOSBestEffort
	}
	if isGuaranteed {
		for name, req := range requests {
			if lim, exists := limits[name]; !exists || lim.Cmp(req) != 0 {
				isGuaranteed = false
				break
			}
		}
	}
	if isGuaranteed && len(requests) == len(limits) {
		return corev1.PodQOSGuaranteed
	}
	return corev1.PodQOSBurstable
}
