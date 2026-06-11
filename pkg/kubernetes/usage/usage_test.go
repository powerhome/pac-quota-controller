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
