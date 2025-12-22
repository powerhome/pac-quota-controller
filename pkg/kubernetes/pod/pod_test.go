package pod

import (
	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"

	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/resource"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

var _ = Describe("Pod", func() {
	Describe("IsTerminal", func() {
		It("should return true for succeeded pods", func() {
			pod := &corev1.Pod{
				Status: corev1.PodStatus{
					Phase: corev1.PodSucceeded,
				},
			}
			Expect(IsPodTerminal(pod)).To(BeTrue())
		})

		It("should return true for failed pods", func() {
			pod := &corev1.Pod{
				Status: corev1.PodStatus{
					Phase: corev1.PodFailed,
				},
			}
			Expect(IsPodTerminal(pod)).To(BeTrue())
		})

		It("should return false for running pods", func() {
			pod := &corev1.Pod{
				Status: corev1.PodStatus{
					Phase: corev1.PodRunning,
				},
			}
			Expect(IsPodTerminal(pod)).To(BeFalse())
		})

		It("should return false for pending pods", func() {
			pod := &corev1.Pod{
				Status: corev1.PodStatus{
					Phase: corev1.PodPending,
				},
			}
			Expect(IsPodTerminal(pod)).To(BeFalse())
		})

		It("should return false for unknown phase pods", func() {
			pod := &corev1.Pod{
				Status: corev1.PodStatus{
					Phase: corev1.PodUnknown,
				},
			}
			Expect(IsPodTerminal(pod)).To(BeFalse())
		})
	})

	Describe("CalculateResourceUsage", func() {
		It("should calculate CPU requests correctly", func() {
			pod := &corev1.Pod{
				ObjectMeta: metav1.ObjectMeta{Name: "test-pod"},
				Spec: corev1.PodSpec{
					Containers: []corev1.Container{
						{
							Name: "container1",
							Resources: corev1.ResourceRequirements{
								Requests: corev1.ResourceList{
									corev1.ResourceCPU: resource.MustParse("200m"),
								},
							},
						},
						{
							Name: "container2",
							Resources: corev1.ResourceRequirements{
								Requests: corev1.ResourceList{
									corev1.ResourceCPU: resource.MustParse("300m"),
								},
							},
						},
					},
				},
			}

			result := CalculatePodUsage(pod, corev1.ResourceRequestsCPU)
			expected := resource.MustParse("500m")
			Expect(result.Equal(expected)).To(BeTrue())
		})

		It("should calculate memory requests correctly", func() {
			pod := &corev1.Pod{
				ObjectMeta: metav1.ObjectMeta{Name: "test-pod"},
				Spec: corev1.PodSpec{
					Containers: []corev1.Container{
						{
							Name: "container1",
							Resources: corev1.ResourceRequirements{
								Requests: corev1.ResourceList{
									corev1.ResourceMemory: resource.MustParse("512Mi"),
								},
							},
						},
						{
							Name: "container2",
							Resources: corev1.ResourceRequirements{
								Requests: corev1.ResourceList{
									corev1.ResourceMemory: resource.MustParse("1Gi"),
								},
							},
						},
					},
				},
			}

			result := CalculatePodUsage(pod, corev1.ResourceRequestsMemory)
			expected := resource.MustParse("1536Mi") // 512Mi + 1024Mi
			Expect(result.Equal(expected)).To(BeTrue())
		})

		It("should take the maximum of init containers (not sum) in calculation", func() {
			pod := &corev1.Pod{
				ObjectMeta: metav1.ObjectMeta{Name: "test-pod"},
				Spec: corev1.PodSpec{
					InitContainers: []corev1.Container{
						{
							Name: "init-1",
							Resources: corev1.ResourceRequirements{
								Requests: corev1.ResourceList{
									corev1.ResourceCPU: resource.MustParse("200m"),
								},
							},
						},
						{
							Name: "init-2",
							Resources: corev1.ResourceRequirements{
								Requests: corev1.ResourceList{
									corev1.ResourceCPU: resource.MustParse("100m"),
								},
							},
						},
					},
					Containers: []corev1.Container{
						{
							Name: "main",
							Resources: corev1.ResourceRequirements{
								Requests: corev1.ResourceList{
									corev1.ResourceCPU: resource.MustParse("150m"),
								},
							},
						},
					},
				},
			}

			// Max(200m, 100m) = 200m. Max(200m, 150m) = 200m.
			result := CalculatePodUsage(pod, corev1.ResourceRequestsCPU)
			expected := resource.MustParse("200m")
			Expect(result.Equal(expected)).To(BeTrue())
		})

		It("should exclude terminated containers from calculation", func() {
			pod := &corev1.Pod{
				ObjectMeta: metav1.ObjectMeta{Name: "test-pod"},
				Spec: corev1.PodSpec{
					Containers: []corev1.Container{
						{
							Name: "running-container",
							Resources: corev1.ResourceRequirements{
								Requests: corev1.ResourceList{
									corev1.ResourceCPU: resource.MustParse("200m"),
								},
							},
						},
						{
							Name: "terminated-container",
							Resources: corev1.ResourceRequirements{
								Requests: corev1.ResourceList{
									corev1.ResourceCPU: resource.MustParse("500m"),
								},
							},
						},
					},
				},
				Status: corev1.PodStatus{
					ContainerStatuses: []corev1.ContainerStatus{
						{
							Name: "running-container",
							State: corev1.ContainerState{
								Running: &corev1.ContainerStateRunning{},
							},
						},
						{
							Name: "terminated-container",
							State: corev1.ContainerState{
								Terminated: &corev1.ContainerStateTerminated{
									ExitCode: 0,
								},
							},
						},
					},
				},
			}

			// Terminated container (500m) should be ignored. Only 200m remains.
			result := CalculatePodUsage(pod, corev1.ResourceRequestsCPU)
			expected := resource.MustParse("200m")
			Expect(result.Equal(expected)).To(BeTrue())
		})

		It("should exclude terminated init containers from calculation", func() {
			pod := &corev1.Pod{
				ObjectMeta: metav1.ObjectMeta{Name: "test-pod"},
				Spec: corev1.PodSpec{
					InitContainers: []corev1.Container{
						{
							Name: "done-init",
							Resources: corev1.ResourceRequirements{
								Requests: corev1.ResourceList{
									corev1.ResourceCPU: resource.MustParse("1000m"),
								},
							},
						},
						{
							Name: "upcoming-init",
							Resources: corev1.ResourceRequirements{
								Requests: corev1.ResourceList{
									corev1.ResourceCPU: resource.MustParse("100m"),
								},
							},
						},
					},
					Containers: []corev1.Container{
						{
							Name: "main",
							Resources: corev1.ResourceRequirements{
								Requests: corev1.ResourceList{
									corev1.ResourceCPU: resource.MustParse("200m"),
								},
							},
						},
					},
				},
				Status: corev1.PodStatus{
					InitContainerStatuses: []corev1.ContainerStatus{
						{
							Name: "done-init",
							State: corev1.ContainerState{
								Terminated: &corev1.ContainerStateTerminated{ExitCode: 0},
							},
						},
					},
				},
			}

			// done-init (1000m) is terminated.
			// remaining: maxInit(100m), appSum(200m). Max is 200m.
			result := CalculatePodUsage(pod, corev1.ResourceRequestsCPU)
			expected := resource.MustParse("200m")
			Expect(result.Equal(expected)).To(BeTrue())
		})

		It("should use only app container sum when all init containers are terminated", func() {
			pod := &corev1.Pod{
				ObjectMeta: metav1.ObjectMeta{Name: "test-pod"},
				Spec: corev1.PodSpec{
					Overhead: corev1.ResourceList{
						corev1.ResourceCPU: resource.MustParse("50m"),
					},
					InitContainers: []corev1.Container{
						{
							Name: "init-1",
							Resources: corev1.ResourceRequirements{
								Requests: corev1.ResourceList{
									corev1.ResourceCPU: resource.MustParse("500m"),
								},
							},
						},
						{
							Name: "init-2",
							Resources: corev1.ResourceRequirements{
								Requests: corev1.ResourceList{
									corev1.ResourceCPU: resource.MustParse("800m"),
								},
							},
						},
					},
					Containers: []corev1.Container{
						{
							Name: "app-1",
							Resources: corev1.ResourceRequirements{
								Requests: corev1.ResourceList{
									corev1.ResourceCPU: resource.MustParse("200m"),
								},
							},
						},
						{
							Name: "app-2",
							Resources: corev1.ResourceRequirements{
								Requests: corev1.ResourceList{
									corev1.ResourceCPU: resource.MustParse("300m"),
								},
							},
						},
					},
				},
				Status: corev1.PodStatus{
					InitContainerStatuses: []corev1.ContainerStatus{
						{
							Name: "init-1",
							State: corev1.ContainerState{
								Terminated: &corev1.ContainerStateTerminated{ExitCode: 0},
							},
						},
						{
							Name: "init-2",
							State: corev1.ContainerState{
								Terminated: &corev1.ContainerStateTerminated{ExitCode: 0},
							},
						},
					},
					ContainerStatuses: []corev1.ContainerStatus{
						{
							Name: "app-1",
							State: corev1.ContainerState{
								Running: &corev1.ContainerStateRunning{},
							},
						},
						{
							Name: "app-2",
							State: corev1.ContainerState{
								Running: &corev1.ContainerStateRunning{},
							},
						},
					},
				},
			}

			// All init containers are terminated, so they don't count.
			// Result should be: overhead (50m) + sum(app containers: 200m + 300m = 500m) = 550m
			result := CalculatePodUsage(pod, corev1.ResourceRequestsCPU)
			expected := resource.MustParse("550m")
			Expect(result.Equal(expected)).To(BeTrue())
		})

		It("should include Pod overhead", func() {
			pod := &corev1.Pod{
				ObjectMeta: metav1.ObjectMeta{Name: "test-pod"},
				Spec: corev1.PodSpec{
					Overhead: corev1.ResourceList{
						corev1.ResourceCPU: resource.MustParse("100m"),
					},
					Containers: []corev1.Container{
						{
							Name: "main",
							Resources: corev1.ResourceRequirements{
								Requests: corev1.ResourceList{
									corev1.ResourceCPU: resource.MustParse("200m"),
								},
							},
						},
					},
				},
			}

			// 100m (overhead) + 200m (app) = 300m
			result := CalculatePodUsage(pod, corev1.ResourceRequestsCPU)
			expected := resource.MustParse("300m")
			Expect(result.Equal(expected)).To(BeTrue())
		})

		It("should include Pod overhead with base resource name", func() {
			pod := &corev1.Pod{
				ObjectMeta: metav1.ObjectMeta{Name: "test-pod"},
				Spec: corev1.PodSpec{
					Overhead: corev1.ResourceList{
						corev1.ResourceCPU: resource.MustParse("100m"),
					},
					Containers: []corev1.Container{
						{
							Name: "main",
							Resources: corev1.ResourceRequirements{
								Requests: corev1.ResourceList{
									corev1.ResourceCPU: resource.MustParse("200m"),
								},
							},
						},
					},
				},
			}

			// overhead specifies 'cpu', we request 'requests.cpu'
			result := CalculatePodUsage(pod, corev1.ResourceRequestsCPU)
			expected := resource.MustParse("300m")
			Expect(result.Equal(expected)).To(BeTrue())
		})
	})

	Describe("SpecEqual", func() {
		It("should return true for identical pod specs", func() {
			pod1 := &corev1.Pod{
				Spec: corev1.PodSpec{
					Containers: []corev1.Container{
						{
							Name: "container1",
							Resources: corev1.ResourceRequirements{
								Requests: corev1.ResourceList{
									corev1.ResourceCPU: resource.MustParse("100m"),
								},
							},
						},
					},
				},
			}
			pod2 := &corev1.Pod{
				Spec: corev1.PodSpec{
					Containers: []corev1.Container{
						{
							Name: "container1",
							Resources: corev1.ResourceRequirements{
								Requests: corev1.ResourceList{
									corev1.ResourceCPU: resource.MustParse("100m"),
								},
							},
						},
					},
				},
			}

			Expect(SpecEqual(pod1, pod2)).To(BeTrue())
		})

		It("should return false for different pod specs", func() {
			pod1 := &corev1.Pod{
				Spec: corev1.PodSpec{
					Containers: []corev1.Container{
						{
							Name: "container1",
							Resources: corev1.ResourceRequirements{
								Requests: corev1.ResourceList{
									corev1.ResourceCPU: resource.MustParse("100m"),
								},
							},
						},
					},
				},
			}
			pod2 := &corev1.Pod{
				Spec: corev1.PodSpec{
					Containers: []corev1.Container{
						{
							Name: "container1",
							Resources: corev1.ResourceRequirements{
								Requests: corev1.ResourceList{
									corev1.ResourceCPU: resource.MustParse("200m"),
								},
							},
						},
					},
				},
			}

			Expect(SpecEqual(pod1, pod2)).To(BeFalse())
		})

		It("should return true for nil pods", func() {
			Expect(SpecEqual(nil, nil)).To(BeTrue())
		})

		It("should return false when one pod is nil", func() {
			pod1 := &corev1.Pod{
				Spec: corev1.PodSpec{
					Containers: []corev1.Container{
						{
							Name: "container1",
						},
					},
				},
			}

			Expect(SpecEqual(pod1, nil)).To(BeFalse())
			Expect(SpecEqual(nil, pod1)).To(BeFalse())
		})

		It("should handle complex pod specs", func() {
			pod1 := &corev1.Pod{
				Spec: corev1.PodSpec{
					InitContainers: []corev1.Container{
						{
							Name: "init-container",
							Resources: corev1.ResourceRequirements{
								Requests: corev1.ResourceList{
									corev1.ResourceMemory: resource.MustParse("128Mi"),
								},
							},
						},
					},
					Containers: []corev1.Container{
						{
							Name: "container1",
							Resources: corev1.ResourceRequirements{
								Requests: corev1.ResourceList{
									corev1.ResourceCPU:    resource.MustParse("100m"),
									corev1.ResourceMemory: resource.MustParse("256Mi"),
								},
								Limits: corev1.ResourceList{
									corev1.ResourceCPU:    resource.MustParse("200m"),
									corev1.ResourceMemory: resource.MustParse("512Mi"),
								},
							},
						},
					},
				},
			}
			pod2 := &corev1.Pod{
				Spec: corev1.PodSpec{
					InitContainers: []corev1.Container{
						{
							Name: "init-container",
							Resources: corev1.ResourceRequirements{
								Requests: corev1.ResourceList{
									corev1.ResourceMemory: resource.MustParse("128Mi"),
								},
							},
						},
					},
					Containers: []corev1.Container{
						{
							Name: "container1",
							Resources: corev1.ResourceRequirements{
								Requests: corev1.ResourceList{
									corev1.ResourceCPU:    resource.MustParse("100m"),
									corev1.ResourceMemory: resource.MustParse("256Mi"),
								},
								Limits: corev1.ResourceList{
									corev1.ResourceCPU:    resource.MustParse("200m"),
									corev1.ResourceMemory: resource.MustParse("512Mi"),
								},
							},
						},
					},
				},
			}

			Expect(SpecEqual(pod1, pod2)).To(BeTrue())
		})
	})
})
