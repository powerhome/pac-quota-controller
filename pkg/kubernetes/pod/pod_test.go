package pod

import (
	"context"
	"fmt"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"

	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/resource"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/client-go/kubernetes/fake"
	"k8s.io/client-go/testing"

	"github.com/powerhome/pac-quota-controller/pkg/kubernetes/usage"
)

var _ = Describe("Pod", func() {
	var ctx context.Context
	BeforeEach(func() {
		ctx = context.Background()
	})
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

		It("should return false for nil pod", func() {
			Expect(IsPodTerminal(nil)).To(BeFalse())
		})
	})

	Describe("NewPodResourceCalculator", func() {
		It("should create a new calculator", func() {
			fakeClient := fake.NewSimpleClientset()
			calc := NewPodResourceCalculator(fakeClient)
			Expect(calc).NotTo(BeNil())
			Expect(calc.Client).To(Equal(fakeClient))
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

		It("should return zero quantity for missing CPU requests", func() {
			pod := &corev1.Pod{Spec: corev1.PodSpec{Containers: []corev1.Container{{Name: "c"}}}}
			result := CalculatePodUsage(pod, corev1.ResourceRequestsCPU)
			Expect(result.IsZero()).To(BeTrue())
		})

		It("should return zero quantity for unknown resources", func() {
			pod := &corev1.Pod{Spec: corev1.PodSpec{Containers: []corev1.Container{{Name: "c"}}}}
			result := CalculatePodUsage(pod, "unknown-resource")
			Expect(result.IsZero()).To(BeTrue())
		})

		It("should handle extended resources without 'requests.' prefix", func() {
			pod := &corev1.Pod{
				Spec: corev1.PodSpec{
					Containers: []corev1.Container{
						{
							Resources: corev1.ResourceRequirements{
								Requests: corev1.ResourceList{
									"nvidia.com/gpu": resource.MustParse("1"),
								},
							},
						},
					},
				},
			}
			result := CalculatePodUsage(pod, "nvidia.com/gpu")
			Expect(result.Equal(resource.MustParse("1"))).To(BeTrue())
		})

		It("should return zero for extended resources when neither requests nor limits are present", func() {
			pod := &corev1.Pod{
				Spec: corev1.PodSpec{
					Containers: []corev1.Container{{Name: "c"}},
				},
			}
			result := CalculatePodUsage(pod, "requests.nvidia.com/gpu")
			Expect(result.IsZero()).To(BeTrue())
		})

		It("should handle ephemeral storage requests", func() {
			pod := &corev1.Pod{
				Spec: corev1.PodSpec{
					Containers: []corev1.Container{
						{
							Resources: corev1.ResourceRequirements{
								Requests: corev1.ResourceList{
									corev1.ResourceEphemeralStorage: resource.MustParse("1Gi"),
								},
							},
						},
					},
				},
			}
			result := CalculatePodUsage(pod, corev1.ResourceRequestsEphemeralStorage)
			Expect(result.Equal(resource.MustParse("1Gi"))).To(BeTrue())
		})

		It("should handle CPU limits", func() {
			pod := &corev1.Pod{
				Spec: corev1.PodSpec{
					Containers: []corev1.Container{
						{
							Resources: corev1.ResourceRequirements{
								Limits: corev1.ResourceList{
									corev1.ResourceCPU: resource.MustParse("1"),
								},
							},
						},
					},
				},
			}
			result := CalculatePodUsage(pod, corev1.ResourceLimitsCPU)
			Expect(result.Equal(resource.MustParse("1"))).To(BeTrue())
		})

		It("should handle Memory limits", func() {
			pod := &corev1.Pod{
				Spec: corev1.PodSpec{
					Containers: []corev1.Container{
						{
							Resources: corev1.ResourceRequirements{
								Limits: corev1.ResourceList{
									corev1.ResourceMemory: resource.MustParse("1Gi"),
								},
							},
						},
					},
				},
			}
			result := CalculatePodUsage(pod, corev1.ResourceLimitsMemory)
			Expect(result.Equal(resource.MustParse("1Gi"))).To(BeTrue())
		})

		It("should handle extended resources with 'requests.' prefix", func() {
			pod := &corev1.Pod{
				Spec: corev1.PodSpec{
					Containers: []corev1.Container{
						{
							Resources: corev1.ResourceRequirements{
								Requests: corev1.ResourceList{
									"nvidia.com/gpu": resource.MustParse("1"),
								},
							},
						},
					},
				},
			}
			result := CalculatePodUsage(pod, "requests.nvidia.com/gpu")
			Expect(result.Equal(resource.MustParse("1"))).To(BeTrue())
		})

		It("should handle direct resource list usage for non-prefixed resources (e.g. hugepages)", func() {
			pod := &corev1.Pod{
				Spec: corev1.PodSpec{
					Containers: []corev1.Container{
						{
							Resources: corev1.ResourceRequirements{
								Requests: corev1.ResourceList{
									"hugepages-2Mi": resource.MustParse("128Mi"),
								},
							},
						},
					},
				},
			}
			result := CalculatePodUsage(pod, "hugepages-2Mi")
			Expect(result.Equal(resource.MustParse("128Mi"))).To(BeTrue())
		})
	})

	Describe("PodResourceCalculator", func() {
		var (
			calculator *PodResourceCalculator
			fakeClient *fake.Clientset
		)

		BeforeEach(func() {
			fakeClient = fake.NewSimpleClientset()
			calculator = &PodResourceCalculator{
				BaseResourceCalculator: usage.BaseResourceCalculator{
					Client: fakeClient,
				},
			}
		})

		Describe("CalculatePodCount", func() {
			It("should count non-terminal pods", func() {
				pods := []corev1.Pod{
					{
						ObjectMeta: metav1.ObjectMeta{Name: "pod1", Namespace: "test"},
						Status:     corev1.PodStatus{Phase: corev1.PodRunning},
					},
					{
						ObjectMeta: metav1.ObjectMeta{Name: "pod2", Namespace: "test"},
						Status:     corev1.PodStatus{Phase: corev1.PodSucceeded}, // Terminal
					},
					{
						ObjectMeta: metav1.ObjectMeta{Name: "pod3", Namespace: "test"},
						Status:     corev1.PodStatus{Phase: corev1.PodPending},
					},
				}
				for _, p := range pods {
					_, err := fakeClient.CoreV1().Pods("test").Create(ctx, &p, metav1.CreateOptions{})
					Expect(err).NotTo(HaveOccurred())
				}

				count, err := calculator.CalculatePodCount(ctx, "test")
				Expect(err).NotTo(HaveOccurred())
				Expect(count).To(Equal(int64(2))) // pod1 and pod3
			})

			It("should handle client errors when listing pods", func() {
				fakeClient.PrependReactor("list", "pods",
					func(action testing.Action) (handled bool, ret runtime.Object, err error) {
						return true, nil, fmt.Errorf("list error")
					})

				_, err := calculator.CalculatePodCount(ctx, "test")
				Expect(err).To(HaveOccurred())
				Expect(err.Error()).To(Equal("list error"))
			})
		})

		Describe("CalculateUsage", func() {
			It("should delegate to CalculatePodCount for 'pods' resource", func() {
				pod := corev1.Pod{
					ObjectMeta: metav1.ObjectMeta{Name: "pod1", Namespace: "test"},
					Status:     corev1.PodStatus{Phase: corev1.PodRunning},
				}
				_, err := fakeClient.CoreV1().Pods("test").Create(ctx, &pod, metav1.CreateOptions{})
				Expect(err).NotTo(HaveOccurred())

				usageResult, err := calculator.CalculateUsage(ctx, "test", usage.ResourcePods)
				Expect(err).NotTo(HaveOccurred())
				Expect(usageResult.Value()).To(Equal(int64(1)))
			})

			It("should handle podcast count errors when resource is pods", func() {
				fakeClient.PrependReactor("list", "pods",
					func(action testing.Action) (handled bool, ret runtime.Object, err error) {
						return true, nil, fmt.Errorf("pod count error")
					})

				_, err := calculator.CalculateUsage(ctx, "test", usage.ResourcePods)
				Expect(err).To(HaveOccurred())
				Expect(err.Error()).To(Equal("pod count error"))
			})

			It("should sum usage across non-terminal pods", func() {
				pods := []corev1.Pod{
					{
						ObjectMeta: metav1.ObjectMeta{Name: "pod1", Namespace: "test"},
						Spec: corev1.PodSpec{
							Containers: []corev1.Container{
								{Resources: corev1.ResourceRequirements{Requests: corev1.ResourceList{
									corev1.ResourceCPU: resource.MustParse("100m"),
								}}},
							},
						},
						Status: corev1.PodStatus{Phase: corev1.PodRunning},
					},
					{
						ObjectMeta: metav1.ObjectMeta{Name: "pod2", Namespace: "test"},
						Spec: corev1.PodSpec{
							Containers: []corev1.Container{
								{Resources: corev1.ResourceRequirements{Requests: corev1.ResourceList{
									corev1.ResourceCPU: resource.MustParse("200m"),
								}}},
							},
						},
						Status: corev1.PodStatus{Phase: corev1.PodSucceeded}, // Terminal
					},
				}
				for _, p := range pods {
					_, err := fakeClient.CoreV1().Pods("test").Create(ctx, &p, metav1.CreateOptions{})
					Expect(err).NotTo(HaveOccurred())
				}

				usageResult, err := calculator.CalculateUsage(ctx, "test", corev1.ResourceRequestsCPU)
				Expect(err).NotTo(HaveOccurred())
				Expect(usageResult.Equal(resource.MustParse("100m"))).To(BeTrue())
			})

			It("should handle client errors when listing pods", func() {
				fakeClient.PrependReactor("list", "pods",
					func(action testing.Action) (handled bool, ret runtime.Object, err error) {
						return true, nil, fmt.Errorf("list error")
					})

				_, err := calculator.CalculateUsage(ctx, "test", corev1.ResourceRequestsCPU)
				Expect(err).To(HaveOccurred())
				Expect(err.Error()).To(Equal("list error"))
			})
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
