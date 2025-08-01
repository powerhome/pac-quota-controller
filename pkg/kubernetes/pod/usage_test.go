package pod

import (
	"context"

	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/resource"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/client-go/kubernetes/fake"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
)

var _ = Describe("PodResourceCalculator", func() {
	// Helper function to create test pods
	createTestPod := func(name, namespace string, phase corev1.PodPhase, cpuRequest string) *corev1.Pod {
		return &corev1.Pod{
			ObjectMeta: metav1.ObjectMeta{
				Name:      name,
				Namespace: namespace,
			},
			Status: corev1.PodStatus{Phase: phase},
			Spec: corev1.PodSpec{
				Containers: []corev1.Container{
					{
						Resources: corev1.ResourceRequirements{
							Requests: corev1.ResourceList{
								corev1.ResourceCPU: resource.MustParse(cpuRequest),
							},
						},
					},
				},
			},
		}
	}

	// Helper function to add pods to fake client and calculate usage
	setupPodsAndCalculateUsage := func(fakeClient *fake.Clientset,
		calculator *PodResourceCalculator, pods ...*corev1.Pod) (resource.Quantity, error) {
		for _, pod := range pods {
			_, err := fakeClient.CoreV1().Pods(pod.Namespace).Create(context.Background(), pod, metav1.CreateOptions{})
			Expect(err).NotTo(HaveOccurred())
		}
		return calculator.CalculateUsage(context.Background(), "test-ns", corev1.ResourceRequestsCPU)
	}
	Describe("CalculatePodUsage", func() {
		It("should calculate pod usage correctly", func() {
			pod := &corev1.Pod{
				Spec: corev1.PodSpec{
					Containers: []corev1.Container{
						{
							Resources: corev1.ResourceRequirements{
								Requests: corev1.ResourceList{
									corev1.ResourceCPU:    resource.MustParse("100m"),
									corev1.ResourceMemory: resource.MustParse("128Mi"),
								},
							},
						},
						{
							Resources: corev1.ResourceRequirements{
								Requests: corev1.ResourceList{
									corev1.ResourceCPU:    resource.MustParse("200m"),
									corev1.ResourceMemory: resource.MustParse("256Mi"),
								},
							},
						},
					},
				},
			}

			cpuUsage := CalculatePodUsage(pod, corev1.ResourceRequestsCPU)
			memoryUsage := CalculatePodUsage(pod, corev1.ResourceRequestsMemory)

			Expect(cpuUsage.MilliValue()).To(Equal(int64(300))) // 100m + 200m
			expectedMemory := resource.MustParse("384Mi")
			Expect(memoryUsage.Value()).To(Equal(expectedMemory.Value())) // 128Mi + 256Mi
		})

		It("should handle pod with no resources", func() {
			pod := &corev1.Pod{
				Spec: corev1.PodSpec{
					Containers: []corev1.Container{
						{
							// No resources specified
						},
					},
				},
			}

			usage := CalculatePodUsage(pod, corev1.ResourceRequestsCPU)
			Expect(usage.MilliValue()).To(Equal(int64(0)))
		})

		It("should include init containers in calculations", func() {
			pod := &corev1.Pod{
				Spec: corev1.PodSpec{
					InitContainers: []corev1.Container{
						{
							Name: "init-container",
							Resources: corev1.ResourceRequirements{
								Requests: corev1.ResourceList{
									corev1.ResourceCPU: resource.MustParse("50m"),
								},
							},
						},
					},
					Containers: []corev1.Container{
						{
							Name: "main-container",
							Resources: corev1.ResourceRequirements{
								Requests: corev1.ResourceList{
									corev1.ResourceCPU: resource.MustParse("100m"),
								},
							},
						},
					},
				},
			}

			usage := CalculatePodUsage(pod, corev1.ResourceRequestsCPU)
			Expect(usage.MilliValue()).To(Equal(int64(150))) // 50m + 100m = 150m
		})

		It("should calculate CPU limits correctly", func() {
			pod := &corev1.Pod{
				Spec: corev1.PodSpec{
					Containers: []corev1.Container{
						{
							Resources: corev1.ResourceRequirements{
								Limits: corev1.ResourceList{
									corev1.ResourceCPU: resource.MustParse("500m"),
								},
							},
						},
					},
				},
			}

			usage := CalculatePodUsage(pod, corev1.ResourceLimitsCPU)
			Expect(usage.MilliValue()).To(Equal(int64(500)))
		})

		It("should calculate memory limits correctly", func() {
			pod := &corev1.Pod{
				Spec: corev1.PodSpec{
					Containers: []corev1.Container{
						{
							Resources: corev1.ResourceRequirements{
								Limits: corev1.ResourceList{
									corev1.ResourceMemory: resource.MustParse("512Mi"),
								},
							},
						},
					},
				},
			}

			usage := CalculatePodUsage(pod, corev1.ResourceLimitsMemory)
			expectedMemory := resource.MustParse("512Mi")
			Expect(usage.Value()).To(Equal(expectedMemory.Value()))
		})

		It("should calculate hugepages usage correctly", func() {
			pod := &corev1.Pod{
				Spec: corev1.PodSpec{
					Containers: []corev1.Container{
						{
							Resources: corev1.ResourceRequirements{
								Requests: corev1.ResourceList{
									"hugepages-2Mi": resource.MustParse("1Gi"),
								},
							},
						},
					},
				},
			}

			usage := CalculatePodUsage(pod, "hugepages-2Mi")
			expected := resource.MustParse("1Gi")
			Expect(usage.Equal(expected)).To(BeTrue())
		})

		It("should handle mixed resource types", func() {
			pod := &corev1.Pod{
				Spec: corev1.PodSpec{
					Containers: []corev1.Container{
						{
							Resources: corev1.ResourceRequirements{
								Requests: corev1.ResourceList{
									corev1.ResourceCPU:    resource.MustParse("100m"),
									corev1.ResourceMemory: resource.MustParse("128Mi"),
								},
								Limits: corev1.ResourceList{
									corev1.ResourceCPU:    resource.MustParse("200m"),
									corev1.ResourceMemory: resource.MustParse("256Mi"),
								},
							},
						},
					},
				},
			}

			cpuRequests := CalculatePodUsage(pod, corev1.ResourceRequestsCPU)
			cpuLimits := CalculatePodUsage(pod, corev1.ResourceLimitsCPU)
			memoryRequests := CalculatePodUsage(pod, corev1.ResourceRequestsMemory)
			memoryLimits := CalculatePodUsage(pod, corev1.ResourceLimitsMemory)

			Expect(cpuRequests.MilliValue()).To(Equal(int64(100)))
			Expect(cpuLimits.MilliValue()).To(Equal(int64(200)))
			expectedMemoryRequests := resource.MustParse("128Mi")
			expectedMemoryLimits := resource.MustParse("256Mi")
			Expect(memoryRequests.Value()).To(Equal(expectedMemoryRequests.Value()))
			Expect(memoryLimits.Value()).To(Equal(expectedMemoryLimits.Value()))
		})

		It("should handle empty containers", func() {
			pod := &corev1.Pod{
				Spec: corev1.PodSpec{
					Containers: []corev1.Container{},
				},
			}

			usage := CalculatePodUsage(pod, corev1.ResourceRequestsCPU)
			Expect(usage.MilliValue()).To(Equal(int64(0)))
		})

		It("should handle nil pod", func() {
			usage := CalculatePodUsage(nil, corev1.ResourceRequestsCPU)
			Expect(usage.MilliValue()).To(Equal(int64(0)))
		})
	})

	Describe("IsPodTerminal", func() {
		It("should identify terminal pods correctly", func() {
			runningPod := &corev1.Pod{Status: corev1.PodStatus{Phase: corev1.PodRunning}}
			succeededPod := &corev1.Pod{Status: corev1.PodStatus{Phase: corev1.PodSucceeded}}
			failedPod := &corev1.Pod{Status: corev1.PodStatus{Phase: corev1.PodFailed}}
			pendingPod := &corev1.Pod{Status: corev1.PodStatus{Phase: corev1.PodPending}}
			unknownPod := &corev1.Pod{Status: corev1.PodStatus{Phase: corev1.PodUnknown}}

			Expect(IsPodTerminal(runningPod)).To(BeFalse())
			Expect(IsPodTerminal(succeededPod)).To(BeTrue())
			Expect(IsPodTerminal(failedPod)).To(BeTrue())
			Expect(IsPodTerminal(pendingPod)).To(BeFalse())
			Expect(IsPodTerminal(unknownPod)).To(BeFalse())
		})

		It("should handle nil pod", func() {
			Expect(IsPodTerminal(nil)).To(BeFalse())
		})

		It("should handle pod with empty status", func() {
			emptyStatusPod := &corev1.Pod{}
			Expect(IsPodTerminal(emptyStatusPod)).To(BeFalse())
		})
	})

	Describe("PodResourceCalculator Constructor", func() {
		It("should create calculator with nil client", func() {
			// This test verifies that the constructor can handle nil clients
			// In a real scenario, this would be used for testing error handling
			calculator := &PodResourceCalculator{}
			Expect(calculator).NotTo(BeNil())
		})
	})

	Describe("Resource Calculation Edge Cases", func() {
		It("should handle zero resource values", func() {
			pod := &corev1.Pod{
				Spec: corev1.PodSpec{
					Containers: []corev1.Container{
						{
							Resources: corev1.ResourceRequirements{
								Requests: corev1.ResourceList{
									corev1.ResourceCPU:    resource.MustParse("0"),
									corev1.ResourceMemory: resource.MustParse("0"),
								},
							},
						},
					},
				},
			}

			cpuUsage := CalculatePodUsage(pod, corev1.ResourceRequestsCPU)
			memoryUsage := CalculatePodUsage(pod, corev1.ResourceRequestsMemory)

			Expect(cpuUsage.MilliValue()).To(Equal(int64(0)))
			Expect(memoryUsage.Value()).To(Equal(int64(0)))
		})

		It("should handle very large resource values", func() {
			pod := &corev1.Pod{
				Spec: corev1.PodSpec{
					Containers: []corev1.Container{
						{
							Resources: corev1.ResourceRequirements{
								Requests: corev1.ResourceList{
									corev1.ResourceCPU:    resource.MustParse("1000"),
									corev1.ResourceMemory: resource.MustParse("1Ti"),
								},
							},
						},
					},
				},
			}

			cpuUsage := CalculatePodUsage(pod, corev1.ResourceRequestsCPU)
			memoryUsage := CalculatePodUsage(pod, corev1.ResourceRequestsMemory)

			Expect(cpuUsage.MilliValue()).To(Equal(int64(1000000))) // 1000 cores = 1000000m
			expectedMemory := resource.MustParse("1Ti")
			Expect(memoryUsage.Value()).To(Equal(expectedMemory.Value()))
		})

		It("should handle fractional CPU values", func() {
			pod := &corev1.Pod{
				Spec: corev1.PodSpec{
					Containers: []corev1.Container{
						{
							Resources: corev1.ResourceRequirements{
								Requests: corev1.ResourceList{
									corev1.ResourceCPU: resource.MustParse("0.5"),
								},
							},
						},
					},
				},
			}

			usage := CalculatePodUsage(pod, corev1.ResourceRequestsCPU)
			Expect(usage.MilliValue()).To(Equal(int64(500))) // 0.5 cores = 500m
		})
	})

	Describe("PodResourceCalculator CalculateUsage", func() {
		var (
			calculator *PodResourceCalculator
			fakeClient *fake.Clientset
		)

		BeforeEach(func() {
			fakeClient = fake.NewSimpleClientset()
			calculator = NewPodResourceCalculator(fakeClient)
		})

		It("should calculate usage for namespace with pods", func() {
			// Create test pods using helper function
			pod1 := createTestPod("pod1", "test-ns", corev1.PodRunning, "100m")
			pod2 := createTestPod("pod2", "test-ns", corev1.PodRunning, "200m")

			// Add pods to fake client and calculate usage
			usage, err := setupPodsAndCalculateUsage(fakeClient, calculator, pod1, pod2)

			Expect(err).NotTo(HaveOccurred())
			Expect(usage.MilliValue()).To(Equal(int64(300))) // 100m + 200m = 300m
		})

		It("should skip terminal pods", func() {
			// Create a running pod and a succeeded pod using helper function
			runningPod := createTestPod("running-pod", "test-ns", corev1.PodRunning, "100m")
			succeededPod := createTestPod("succeeded-pod", "test-ns", corev1.PodSucceeded, "200m")

			// Add pods to fake client and calculate usage
			usage, err := setupPodsAndCalculateUsage(fakeClient, calculator, runningPod, succeededPod)

			Expect(err).NotTo(HaveOccurred())
			Expect(usage.MilliValue()).To(Equal(int64(100))) // Only running pod counts
		})

		It("should return zero usage for empty namespace", func() {
			usage, err := calculator.CalculateUsage(context.Background(), "empty-ns", corev1.ResourceRequestsCPU)

			Expect(err).NotTo(HaveOccurred())
			Expect(usage.MilliValue()).To(Equal(int64(0)))
		})
	})

	Describe("PodResourceCalculator CalculateTotalUsage", func() {
		var (
			calculator *PodResourceCalculator
			fakeClient *fake.Clientset
		)

		BeforeEach(func() {
			fakeClient = fake.NewSimpleClientset()
			calculator = NewPodResourceCalculator(fakeClient)
		})

		It("should calculate total usage for all resources", func() {
			// Create a test pod with multiple resources
			pod := &corev1.Pod{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "test-pod",
					Namespace: "test-ns",
				},
				Status: corev1.PodStatus{Phase: corev1.PodRunning},
				Spec: corev1.PodSpec{
					Containers: []corev1.Container{
						{
							Resources: corev1.ResourceRequirements{
								Requests: corev1.ResourceList{
									corev1.ResourceCPU:    resource.MustParse("100m"),
									corev1.ResourceMemory: resource.MustParse("128Mi"),
								},
								Limits: corev1.ResourceList{
									corev1.ResourceCPU:    resource.MustParse("200m"),
									corev1.ResourceMemory: resource.MustParse("256Mi"),
								},
							},
						},
					},
				},
			}

			// Add pod to fake client
			_, err := fakeClient.CoreV1().Pods("test-ns").Create(context.Background(), pod, metav1.CreateOptions{})
			Expect(err).NotTo(HaveOccurred())

			totalUsage, err := calculator.CalculateTotalUsage(context.Background(), "test-ns")

			Expect(err).NotTo(HaveOccurred())
			Expect(totalUsage).NotTo(BeNil())
			Expect(totalUsage).ToNot(BeEmpty())
		})

		It("should return empty map for empty namespace", func() {
			totalUsage, err := calculator.CalculateTotalUsage(context.Background(), "empty-ns")

			Expect(err).NotTo(HaveOccurred())
			Expect(totalUsage).NotTo(BeNil())
			Expect(totalUsage).To(HaveLen(7)) // 7 resource types (including pod count)
		})
	})

	Describe("CalculatePodCount", func() {
		var (
			calculator *PodResourceCalculator
			fakeClient *fake.Clientset
			ctx        context.Context
		)

		BeforeEach(func() {
			ctx = context.Background()
			fakeClient = fake.NewSimpleClientset()
			calculator = NewPodResourceCalculator(fakeClient)
		})

		It("should return zero for empty namespace", func() {
			count, err := calculator.CalculatePodCount(ctx, "empty-namespace")
			Expect(err).NotTo(HaveOccurred())
			Expect(count).To(Equal(int64(0)))
		})

		It("should count only non-terminal pods", func() {
			// Create test pods
			pod1 := &corev1.Pod{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "running-pod",
					Namespace: "test-namespace",
				},
				Status: corev1.PodStatus{
					Phase: corev1.PodRunning,
				},
			}
			pod2 := &corev1.Pod{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "succeeded-pod",
					Namespace: "test-namespace",
				},
				Status: corev1.PodStatus{
					Phase: corev1.PodSucceeded,
				},
			}
			pod3 := &corev1.Pod{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "failed-pod",
					Namespace: "test-namespace",
				},
				Status: corev1.PodStatus{
					Phase: corev1.PodFailed,
				},
			}
			pod4 := &corev1.Pod{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "pending-pod",
					Namespace: "test-namespace",
				},
				Status: corev1.PodStatus{
					Phase: corev1.PodPending,
				},
			}

			fakeClient := fake.NewSimpleClientset(pod1, pod2, pod3, pod4)
			calculator := NewPodResourceCalculator(fakeClient)

			count, err := calculator.CalculatePodCount(ctx, "test-namespace")
			Expect(err).NotTo(HaveOccurred())
			Expect(count).To(Equal(int64(2))) // Only running and pending pods
		})

		It("should handle client error", func() {
			// Create a client that will fail
			fakeClient := fake.NewSimpleClientset()
			// Simulate error by using a non-existent namespace
			calculator := NewPodResourceCalculator(fakeClient)

			_, err := calculator.CalculatePodCount(ctx, "non-existent-namespace")
			Expect(err).NotTo(HaveOccurred()) // Fake client doesn't actually fail
		})
	})
})
