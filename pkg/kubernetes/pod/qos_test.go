package pod

import (
	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"

	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/resource"
)

func containerWithResources(name string, requests, limits corev1.ResourceList) corev1.Container {
	return corev1.Container{
		Name: name,
		Resources: corev1.ResourceRequirements{
			Requests: requests,
			Limits:   limits,
		},
	}
}

var _ = Describe("ComputePodQOS", func() {
	cpuMem := func(cpu, mem string) corev1.ResourceList {
		return corev1.ResourceList{
			corev1.ResourceCPU:    resource.MustParse(cpu),
			corev1.ResourceMemory: resource.MustParse(mem),
		}
	}

	It("should return BestEffort for a nil pod", func() {
		Expect(ComputePodQOS(nil)).To(Equal(corev1.PodQOSBestEffort))
	})

	It("should return BestEffort when no container sets cpu or memory", func() {
		pod := &corev1.Pod{
			Spec: corev1.PodSpec{
				Containers: []corev1.Container{containerWithResources("c1", nil, nil)},
			},
		}
		Expect(ComputePodQOS(pod)).To(Equal(corev1.PodQOSBestEffort))
	})

	It("should return BestEffort when only ephemeral-storage is set", func() {
		pod := &corev1.Pod{
			Spec: corev1.PodSpec{
				Containers: []corev1.Container{
					containerWithResources("c1", corev1.ResourceList{
						corev1.ResourceEphemeralStorage: resource.MustParse("1Gi"),
					}, nil),
				},
			},
		}
		Expect(ComputePodQOS(pod)).To(Equal(corev1.PodQOSBestEffort))
	})

	It("should ignore ephemeral-storage when computing limits", func() {
		pod := &corev1.Pod{
			Spec: corev1.PodSpec{
				Containers: []corev1.Container{
					containerWithResources("c1", cpuMem("100m", "128Mi"), corev1.ResourceList{
						corev1.ResourceEphemeralStorage: resource.MustParse("1Gi"),
					}),
				},
			},
		}
		Expect(ComputePodQOS(pod)).To(Equal(corev1.PodQOSBurstable))
	})

	It("should return Burstable when only requests are set", func() {
		pod := &corev1.Pod{
			Spec: corev1.PodSpec{
				Containers: []corev1.Container{
					containerWithResources("c1", cpuMem("100m", "128Mi"), nil),
				},
			},
		}
		Expect(ComputePodQOS(pod)).To(Equal(corev1.PodQOSBurstable))
	})

	It("should return Burstable when only one container is fully limited", func() {
		pod := &corev1.Pod{
			Spec: corev1.PodSpec{
				Containers: []corev1.Container{
					containerWithResources("c1", cpuMem("100m", "128Mi"), cpuMem("100m", "128Mi")),
					containerWithResources("c2", nil, nil),
				},
			},
		}
		Expect(ComputePodQOS(pod)).To(Equal(corev1.PodQOSBurstable))
	})

	It("should return Burstable when an init container sets requests on an otherwise empty pod", func() {
		pod := &corev1.Pod{
			Spec: corev1.PodSpec{
				Containers:     []corev1.Container{containerWithResources("c1", nil, nil)},
				InitContainers: []corev1.Container{containerWithResources("init", cpuMem("100m", "64Mi"), nil)},
			},
		}
		Expect(ComputePodQOS(pod)).To(Equal(corev1.PodQOSBurstable))
	})

	It("should return Guaranteed when all containers have requests equal to limits", func() {
		pod := &corev1.Pod{
			Spec: corev1.PodSpec{
				Containers: []corev1.Container{
					containerWithResources("c1", cpuMem("100m", "128Mi"), cpuMem("100m", "128Mi")),
					containerWithResources("c2", cpuMem("200m", "256Mi"), cpuMem("200m", "256Mi")),
				},
			},
		}
		Expect(ComputePodQOS(pod)).To(Equal(corev1.PodQOSGuaranteed))
	})

	It("should return Burstable for a raw limits-only spec (API defaulting fills requests on real pods)", func() {
		pod := &corev1.Pod{
			Spec: corev1.PodSpec{
				Containers: []corev1.Container{
					containerWithResources("c1", nil, cpuMem("100m", "128Mi")),
				},
			},
		}
		Expect(ComputePodQOS(pod)).To(Equal(corev1.PodQOSBurstable))
	})

	It("should return Burstable when requests differ from limits", func() {
		pod := &corev1.Pod{
			Spec: corev1.PodSpec{
				Containers: []corev1.Container{
					containerWithResources("c1", cpuMem("100m", "128Mi"), cpuMem("200m", "256Mi")),
				},
			},
		}
		Expect(ComputePodQOS(pod)).To(Equal(corev1.PodQOSBurstable))
	})

	It("should prefer pod-level resources over container resources", func() {
		pod := &corev1.Pod{
			Spec: corev1.PodSpec{
				Resources: &corev1.ResourceRequirements{
					Requests: cpuMem("500m", "512Mi"),
					Limits:   cpuMem("500m", "512Mi"),
				},
				Containers: []corev1.Container{containerWithResources("c1", cpuMem("100m", "128Mi"), nil)},
			},
		}
		Expect(ComputePodQOS(pod)).To(Equal(corev1.PodQOSGuaranteed))
	})
})
