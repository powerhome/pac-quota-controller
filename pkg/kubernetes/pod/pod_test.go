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

		It("should include init containers in calculation", func() {
			pod := &corev1.Pod{
				ObjectMeta: metav1.ObjectMeta{Name: "test-pod"},
				Spec: corev1.PodSpec{
					InitContainers: []corev1.Container{
						{
							Name: "init-container",
							Resources: corev1.ResourceRequirements{
								Requests: corev1.ResourceList{
									corev1.ResourceCPU: resource.MustParse("100m"),
								},
							},
						},
					},
					Containers: []corev1.Container{
						{
							Name: "main-container",
							Resources: corev1.ResourceRequirements{
								Requests: corev1.ResourceList{
									corev1.ResourceCPU: resource.MustParse("200m"),
								},
							},
						},
					},
				},
			}

			result := CalculatePodUsage(pod, corev1.ResourceRequestsCPU)
			expected := resource.MustParse("300m") // 100m + 200m = 300m
			Expect(result.Equal(expected)).To(BeTrue())
		})

		It("should calculate CPU limits correctly", func() {
			pod := &corev1.Pod{
				ObjectMeta: metav1.ObjectMeta{Name: "test-pod"},
				Spec: corev1.PodSpec{
					Containers: []corev1.Container{
						{
							Name: "container1",
							Resources: corev1.ResourceRequirements{
								Limits: corev1.ResourceList{
									corev1.ResourceCPU: resource.MustParse("500m"),
								},
							},
						},
					},
				},
			}

			result := CalculatePodUsage(pod, corev1.ResourceLimitsCPU)
			expected := resource.MustParse("500m")
			Expect(result.Equal(expected)).To(BeTrue())
		})

		It("should calculate memory limits correctly", func() {
			pod := &corev1.Pod{
				ObjectMeta: metav1.ObjectMeta{Name: "test-pod"},
				Spec: corev1.PodSpec{
					Containers: []corev1.Container{
						{
							Name: "container1",
							Resources: corev1.ResourceRequirements{
								Limits: corev1.ResourceList{
									corev1.ResourceMemory: resource.MustParse("2Gi"),
								},
							},
						},
					},
				},
			}

			result := CalculatePodUsage(pod, corev1.ResourceLimitsMemory)
			expected := resource.MustParse("2Gi")
			Expect(result.Equal(expected)).To(BeTrue())
		})

		It("should handle hugepages correctly", func() {
			pod := &corev1.Pod{
				ObjectMeta: metav1.ObjectMeta{Name: "test-pod"},
				Spec: corev1.PodSpec{
					Containers: []corev1.Container{
						{
							Name: "container1",
							Resources: corev1.ResourceRequirements{
								Requests: corev1.ResourceList{
									"hugepages-2Mi": resource.MustParse("1Gi"),
								},
							},
						},
					},
				},
			}

			result := CalculatePodUsage(pod, "hugepages-2Mi")
			expected := resource.MustParse("1Gi")
			Expect(result.Equal(expected)).To(BeTrue())
		})

		It("should return zero for missing resources", func() {
			pod := &corev1.Pod{
				ObjectMeta: metav1.ObjectMeta{Name: "test-pod"},
				Spec: corev1.PodSpec{
					Containers: []corev1.Container{
						{
							Name:      "container1",
							Resources: corev1.ResourceRequirements{},
						},
					},
				},
			}

			result := CalculatePodUsage(pod, corev1.ResourceRequestsCPU)
			expected := resource.MustParse("0")
			Expect(result.Equal(expected)).To(BeTrue())
		})

		It("should sum multiple init containers and regular containers", func() {
			pod := &corev1.Pod{
				ObjectMeta: metav1.ObjectMeta{Name: "test-pod"},
				Spec: corev1.PodSpec{
					InitContainers: []corev1.Container{
						{
							Name: "init-container-1",
							Resources: corev1.ResourceRequirements{
								Requests: corev1.ResourceList{
									corev1.ResourceCPU: resource.MustParse("50m"),
								},
							},
						},
						{
							Name: "init-container-2",
							Resources: corev1.ResourceRequirements{
								Requests: corev1.ResourceList{
									corev1.ResourceCPU: resource.MustParse("75m"),
								},
							},
						},
					},
					Containers: []corev1.Container{
						{
							Name: "main-container-1",
							Resources: corev1.ResourceRequirements{
								Requests: corev1.ResourceList{
									corev1.ResourceCPU: resource.MustParse("100m"),
								},
							},
						},
						{
							Name: "main-container-2",
							Resources: corev1.ResourceRequirements{
								Requests: corev1.ResourceList{
									corev1.ResourceCPU: resource.MustParse("200m"),
								},
							},
						},
					},
				},
			}

			result := CalculatePodUsage(pod, corev1.ResourceRequestsCPU)
			expected := resource.MustParse("425m") // 50m + 75m + 100m + 200m = 425m
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
