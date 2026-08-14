package usage

import (
	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"

	corev1 "k8s.io/api/core/v1"
)

var _ = Describe("Usage", func() {
	Describe("Common Resource Names", func() {
		It("should have correct CPU resource names", func() {
			Expect(ResourceRequestsCPU).To(Equal(corev1.ResourceRequestsCPU))
			Expect(ResourceLimitsCPU).To(Equal(corev1.ResourceLimitsCPU))
			Expect(ResourceCPU).To(Equal(corev1.ResourceCPU))
		})

		It("should have correct memory resource names", func() {
			Expect(ResourceRequestsMemory).To(Equal(corev1.ResourceRequestsMemory))
			Expect(ResourceLimitsMemory).To(Equal(corev1.ResourceLimitsMemory))
			Expect(ResourceMemory).To(Equal(corev1.ResourceMemory))
		})

		It("should have correct storage resource names", func() {
			Expect(ResourceRequestsStorage).To(Equal(corev1.ResourceRequestsStorage))
			Expect(ResourceStorage).To(Equal(corev1.ResourceStorage))
		})

		It("should have correct ephemeral storage resource names", func() {
			Expect(ResourceRequestsEphemeralStorage).To(Equal(corev1.ResourceRequestsEphemeralStorage))
			Expect(ResourceLimitsEphemeralStorage).To(Equal(corev1.ResourceLimitsEphemeralStorage))
			Expect(ResourceEphemeralStorage).To(Equal(corev1.ResourceEphemeralStorage))
		})
	})

	Describe("Resource category tables", func() {
		It("should mark pod-derived compute resources as pod-eligible", func() {
			Expect(PodEligibleResources[ResourcePods]).To(BeTrue())
			Expect(PodEligibleResources[ResourceRequestsCPU]).To(BeTrue())
			Expect(PodEligibleResources[ResourceRequestsMemory]).To(BeTrue())
			Expect(PodEligibleResources[ResourceLimitsCPU]).To(BeTrue())
			Expect(PodEligibleResources[ResourceLimitsMemory]).To(BeTrue())
		})

		It("should exclude ephemeral-storage from pod-eligible resources", func() {
			// Pod-derived, but upstream's podComputeQuotaResources deliberately
			// excludes it from scope eligibility.
			Expect(PodEligibleResources[ResourceRequestsEphemeralStorage]).To(BeFalse())
			Expect(PodEligibleResources[ResourceLimitsEphemeralStorage]).To(BeFalse())
		})

		It("should categorize service resources", func() {
			Expect(ServiceResources[ResourceServices]).To(BeTrue())
			Expect(ServiceResources[ResourceServicesLoadBalancers]).To(BeTrue())
			Expect(ServiceResources[ResourceServicesNodePorts]).To(BeTrue())
			Expect(ServiceResources[ResourcePods]).To(BeFalse())
		})

		It("should categorize PVC resources", func() {
			Expect(PVCResources[ResourceRequestsStorage]).To(BeTrue())
			Expect(PVCResources[ResourcePersistentVolumeClaims]).To(BeTrue())
			Expect(PVCResources[ResourcePods]).To(BeFalse())
		})

		It("should categorize object-count resources", func() {
			for _, name := range []corev1.ResourceName{
				ResourceConfigMaps, ResourceSecrets, ResourceReplicationControllers,
				ResourceDeployments, ResourceStatefulSets, ResourceDaemonSets,
				ResourceJobs, ResourceCronJobs, ResourceHorizontalPodAutoscalers, ResourceIngresses,
			} {
				Expect(ObjectCountResources[name]).To(BeTrue(), "expected %s to be an object-count resource", name)
			}
			Expect(ObjectCountResources[ResourcePods]).To(BeFalse())
		})

		It("should not overlap between categories", func() {
			for name := range PodEligibleResources {
				Expect(ServiceResources[name]).To(BeFalse())
				Expect(PVCResources[name]).To(BeFalse())
				Expect(ObjectCountResources[name]).To(BeFalse())
			}
			for name := range ServiceResources {
				Expect(PVCResources[name]).To(BeFalse())
				Expect(ObjectCountResources[name]).To(BeFalse())
			}
			for name := range PVCResources {
				Expect(ObjectCountResources[name]).To(BeFalse())
			}
		})
	})

	Describe("GetBaseResourceName", func() {
		It("should strip 'requests.' prefix", func() {
			Expect(GetBaseResourceName(corev1.ResourceRequestsCPU)).To(Equal(corev1.ResourceCPU))
			Expect(GetBaseResourceName(corev1.ResourceRequestsMemory)).To(Equal(corev1.ResourceMemory))
			Expect(GetBaseResourceName(corev1.ResourceRequestsEphemeralStorage)).To(Equal(corev1.ResourceEphemeralStorage))
		})

		It("should strip 'limits.' prefix", func() {
			Expect(GetBaseResourceName(corev1.ResourceLimitsCPU)).To(Equal(corev1.ResourceCPU))
			Expect(GetBaseResourceName(corev1.ResourceLimitsMemory)).To(Equal(corev1.ResourceMemory))
			Expect(GetBaseResourceName(corev1.ResourceLimitsEphemeralStorage)).To(Equal(corev1.ResourceEphemeralStorage))
		})

		It("should return unchanged for other resources", func() {
			Expect(GetBaseResourceName(corev1.ResourcePods)).To(Equal(corev1.ResourcePods))
			Expect(GetBaseResourceName(corev1.ResourceCPU)).To(Equal(corev1.ResourceCPU))
			Expect(GetBaseResourceName("nvidia.com/gpu")).To(Equal(corev1.ResourceName("nvidia.com/gpu")))
		})
	})
})
