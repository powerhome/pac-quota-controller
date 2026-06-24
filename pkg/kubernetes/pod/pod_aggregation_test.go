package pod

import (
	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"

	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/resource"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"

	"github.com/powerhome/pac-quota-controller/pkg/kubernetes/usage"
)

// podWithCPU builds a single-container pod requesting the given CPU in the given phase.
func podWithCPU(name, cpu string, phase corev1.PodPhase) corev1.Pod {
	return corev1.Pod{
		ObjectMeta: metav1.ObjectMeta{Name: name},
		Spec: corev1.PodSpec{
			Containers: []corev1.Container{{
				Name: "c",
				Resources: corev1.ResourceRequirements{
					Requests: corev1.ResourceList{corev1.ResourceCPU: resource.MustParse(cpu)},
				},
			}},
		},
		Status: corev1.PodStatus{Phase: phase},
	}
}

var _ = Describe("CalculateUsageFromPods", func() {
	Describe("pod count", func() {
		It("counts only non-terminal pods", func() {
			pods := []corev1.Pod{
				podWithCPU("running", "100m", corev1.PodRunning),
				podWithCPU("pending", "100m", corev1.PodPending),
				podWithCPU("succeeded", "100m", corev1.PodSucceeded),
				podWithCPU("failed", "100m", corev1.PodFailed),
			}
			result := CalculateUsageFromPods(pods, usage.ResourcePods)
			Expect(result.Value()).To(Equal(int64(2)))
		})

		It("returns zero for an empty list", func() {
			empty := CalculateUsageFromPods([]corev1.Pod{}, usage.ResourcePods)
			Expect(empty.Value()).To(Equal(int64(0)))
		})

		It("returns zero for a nil list", func() {
			nilList := CalculateUsageFromPods(nil, usage.ResourcePods)
			Expect(nilList.Value()).To(Equal(int64(0)))
		})
	})

	Describe("resource sum", func() {
		It("sums CPU across non-terminal pods and excludes terminal ones", func() {
			pods := []corev1.Pod{
				podWithCPU("running", "100m", corev1.PodRunning),
				podWithCPU("pending", "250m", corev1.PodPending),
				podWithCPU("succeeded", "500m", corev1.PodSucceeded),
				podWithCPU("failed", "999m", corev1.PodFailed),
			}
			result := CalculateUsageFromPods(pods, corev1.ResourceRequestsCPU)
			Expect(result.Equal(resource.MustParse("350m"))).To(BeTrue())
		})

		It("returns zero for an empty list", func() {
			empty := CalculateUsageFromPods([]corev1.Pod{}, corev1.ResourceRequestsCPU)
			Expect(empty.IsZero()).To(BeTrue())
		})

		It("returns zero for a nil list", func() {
			nilList := CalculateUsageFromPods(nil, corev1.ResourceRequestsCPU)
			Expect(nilList.IsZero()).To(BeTrue())
		})

		It("returns zero when every pod is terminal", func() {
			pods := []corev1.Pod{
				podWithCPU("succeeded", "500m", corev1.PodSucceeded),
				podWithCPU("failed", "500m", corev1.PodFailed),
			}
			allTerminal := CalculateUsageFromPods(pods, corev1.ResourceRequestsCPU)
			Expect(allTerminal.IsZero()).To(BeTrue())
		})
	})
})

var _ = Describe("CalculatePodUsage extra branches", func() {
	It("returns an empty quantity for a nil pod", func() {
		nilUsage := CalculatePodUsage(nil, corev1.ResourceRequestsCPU)
		Expect(nilUsage.IsZero()).To(BeTrue())
	})

	It("uses the app-container sum when it exceeds the max init container", func() {
		pod := &corev1.Pod{
			Spec: corev1.PodSpec{
				InitContainers: []corev1.Container{{
					Name: "init",
					Resources: corev1.ResourceRequirements{
						Requests: corev1.ResourceList{corev1.ResourceCPU: resource.MustParse("150m")},
					},
				}},
				Containers: []corev1.Container{
					{Name: "a", Resources: corev1.ResourceRequirements{
						Requests: corev1.ResourceList{corev1.ResourceCPU: resource.MustParse("200m")},
					}},
					{Name: "b", Resources: corev1.ResourceRequirements{
						Requests: corev1.ResourceList{corev1.ResourceCPU: resource.MustParse("300m")},
					}},
				},
			},
		}
		// max(sum(app)=500m, maxInit=150m) = 500m
		Expect(CalculatePodUsage(pod, corev1.ResourceRequestsCPU).Equal(resource.MustParse("500m"))).To(BeTrue())
	})

	It("adds overhead keyed by the exact resource name", func() {
		pod := &corev1.Pod{
			Spec: corev1.PodSpec{
				Overhead: corev1.ResourceList{corev1.ResourceRequestsCPU: resource.MustParse("50m")},
				Containers: []corev1.Container{{
					Name: "c",
					Resources: corev1.ResourceRequirements{
						Requests: corev1.ResourceList{corev1.ResourceCPU: resource.MustParse("100m")},
					},
				}},
			},
		}
		// overhead(50m, exact key) + app(100m) = 150m
		Expect(CalculatePodUsage(pod, corev1.ResourceRequestsCPU).Equal(resource.MustParse("150m"))).To(BeTrue())
	})
})
