package pod

import (
	"context"
	"testing"

	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/resource"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/client/fake"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
)

func TestPod(t *testing.T) {
	RegisterFailHandler(Fail)
	RunSpecs(t, "Pod Package Suite")
}

var _ = Describe("ComputeResourceCalculator", func() {
	var (
		calculator *ComputeResourceCalculator
		fakeClient client.Client
		ctx        context.Context
		scheme     *runtime.Scheme
	)

	BeforeEach(func() {
		scheme = runtime.NewScheme()
		Expect(corev1.AddToScheme(scheme)).To(Succeed())

		fakeClient = fake.NewClientBuilder().WithScheme(scheme).Build()
		calculator = NewComputeResourceCalculator(fakeClient)
		ctx = context.Background()
	})

	Context("CalculateComputeUsage", func() {
		It("should calculate CPU requests correctly", func() {
			pod := &corev1.Pod{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "test-pod",
					Namespace: "test-namespace",
				},
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
						{
							Name: "container2",
							Resources: corev1.ResourceRequirements{
								Requests: corev1.ResourceList{
									corev1.ResourceCPU: resource.MustParse("200m"),
								},
							},
						},
					},
				},
				Status: corev1.PodStatus{
					Phase: corev1.PodRunning,
				},
			}

			Expect(fakeClient.Create(ctx, pod)).To(Succeed())

			usage, err := calculator.CalculateComputeUsage(ctx, "test-namespace", corev1.ResourceRequestsCPU)
			Expect(err).NotTo(HaveOccurred())
			Expect(usage.MilliValue()).To(Equal(int64(300))) // 100m + 200m
		})

		It("should calculate memory requests correctly", func() {
			pod := &corev1.Pod{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "test-pod",
					Namespace: "test-namespace",
				},
				Spec: corev1.PodSpec{
					Containers: []corev1.Container{
						{
							Name: "container1",
							Resources: corev1.ResourceRequirements{
								Requests: corev1.ResourceList{
									corev1.ResourceMemory: resource.MustParse("128Mi"),
								},
							},
						},
					},
				},
				Status: corev1.PodStatus{
					Phase: corev1.PodRunning,
				},
			}

			Expect(fakeClient.Create(ctx, pod)).To(Succeed())

			usage, err := calculator.CalculateComputeUsage(ctx, "test-namespace", corev1.ResourceRequestsMemory)
			Expect(err).NotTo(HaveOccurred())
			expectedMemory := resource.MustParse("128Mi")
			Expect(usage.Value()).To(Equal(expectedMemory.Value()))
		})

		It("should include init containers in calculations", func() {
			pod := &corev1.Pod{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "test-pod",
					Namespace: "test-namespace",
				},
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
				Status: corev1.PodStatus{
					Phase: corev1.PodRunning,
				},
			}

			Expect(fakeClient.Create(ctx, pod)).To(Succeed())

			usage, err := calculator.CalculateComputeUsage(ctx, "test-namespace", corev1.ResourceRequestsCPU)
			Expect(err).NotTo(HaveOccurred())
			Expect(usage.MilliValue()).To(Equal(int64(150))) // 50m + 100m = 150m
		})

		It("should exclude terminal pods", func() {
			runningPod := &corev1.Pod{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "running-pod",
					Namespace: "test-namespace",
				},
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
				Status: corev1.PodStatus{
					Phase: corev1.PodRunning,
				},
			}

			succeededPod := &corev1.Pod{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "succeeded-pod",
					Namespace: "test-namespace",
				},
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
				Status: corev1.PodStatus{
					Phase: corev1.PodSucceeded,
				},
			}

			failedPod := &corev1.Pod{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "failed-pod",
					Namespace: "test-namespace",
				},
				Spec: corev1.PodSpec{
					Containers: []corev1.Container{
						{
							Name: "container1",
							Resources: corev1.ResourceRequirements{
								Requests: corev1.ResourceList{
									corev1.ResourceCPU: resource.MustParse("300m"),
								},
							},
						},
					},
				},
				Status: corev1.PodStatus{
					Phase: corev1.PodFailed,
				},
			}

			Expect(fakeClient.Create(ctx, runningPod)).To(Succeed())
			Expect(fakeClient.Create(ctx, succeededPod)).To(Succeed())
			Expect(fakeClient.Create(ctx, failedPod)).To(Succeed())

			usage, err := calculator.CalculateComputeUsage(ctx, "test-namespace", corev1.ResourceRequestsCPU)
			Expect(err).NotTo(HaveOccurred())
			// Only the running pod should be counted
			Expect(usage.MilliValue()).To(Equal(int64(100)))
		})

		It("should handle pods without resource requests", func() {
			pod := &corev1.Pod{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "no-resources-pod",
					Namespace: "test-namespace",
				},
				Spec: corev1.PodSpec{
					Containers: []corev1.Container{
						{
							Name: "container1",
							// No resource requests specified
						},
					},
				},
				Status: corev1.PodStatus{
					Phase: corev1.PodRunning,
				},
			}

			Expect(fakeClient.Create(ctx, pod)).To(Succeed())

			usage, err := calculator.CalculateComputeUsage(ctx, "test-namespace", corev1.ResourceRequestsCPU)
			Expect(err).NotTo(HaveOccurred())
			Expect(usage.MilliValue()).To(Equal(int64(0)))
		})

		It("should calculate CPU limits correctly", func() {
			pod := &corev1.Pod{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "test-pod",
					Namespace: "test-namespace",
				},
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
				Status: corev1.PodStatus{
					Phase: corev1.PodRunning,
				},
			}

			Expect(fakeClient.Create(ctx, pod)).To(Succeed())

			usage, err := calculator.CalculateComputeUsage(ctx, "test-namespace", corev1.ResourceLimitsCPU)
			Expect(err).NotTo(HaveOccurred())
			Expect(usage.MilliValue()).To(Equal(int64(500)))
		})

		It("should return zero for empty namespace", func() {
			usage, err := calculator.CalculateComputeUsage(ctx, "empty-namespace", corev1.ResourceRequestsCPU)
			Expect(err).NotTo(HaveOccurred())
			Expect(usage.MilliValue()).To(Equal(int64(0)))
		})

		It("should calculate hugepages usage correctly", func() {
			pod := &corev1.Pod{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "hugepages-pod",
					Namespace: "test-namespace",
				},
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
				Status: corev1.PodStatus{
					Phase: corev1.PodRunning,
				},
			}

			Expect(fakeClient.Create(ctx, pod)).To(Succeed())

			usage, err := calculator.CalculateComputeUsage(ctx, "test-namespace", "hugepages-2Mi")
			Expect(err).NotTo(HaveOccurred())
			expected := resource.MustParse("1Gi")
			Expect(usage.Equal(expected)).To(BeTrue())
		})
	})
})
