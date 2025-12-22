package controller

import (
	"strings"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	"go.uber.org/zap"
	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/resource"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/client-go/tools/record"

	quotav1alpha1 "github.com/powerhome/pac-quota-controller/api/v1alpha1"
	"github.com/powerhome/pac-quota-controller/pkg/events"
)

var _ = Describe("ClusterResourceQuota Helpers", func() {
	var (
		reconciler   *ClusterResourceQuotaReconciler
		fakeRecorder *record.FakeRecorder
		testCRQ      *quotav1alpha1.ClusterResourceQuota
		logger       *zap.Logger
	)

	BeforeEach(func() {
		logger = zap.NewNop()
		fakeRecorder = record.NewFakeRecorder(100)

		// Create reconciler with mock event recorder
		reconciler = &ClusterResourceQuotaReconciler{
			EventRecorder: events.NewEventRecorder(fakeRecorder, "test-namespace", logger),
		}

		// Create test CRQ
		testCRQ = &quotav1alpha1.ClusterResourceQuota{
			ObjectMeta: metav1.ObjectMeta{
				Name: "test-crq",
			},
			Spec: quotav1alpha1.ClusterResourceQuotaSpec{
				NamespaceSelector: &metav1.LabelSelector{
					MatchLabels: map[string]string{
						"team": "test",
					},
				},
				Hard: quotav1alpha1.ResourceList{
					corev1.ResourceRequestsCPU:    resource.MustParse("2"),
					corev1.ResourceRequestsMemory: resource.MustParse("4Gi"),
					corev1.ResourcePods:           resource.MustParse("10"),
				},
			},
		}
	})

	Describe("checkQuotaThresholds", func() {
		Context("with fractional CPU resources", func() {
			It("should trigger violation when usage exceeds limit with fractional values", func() {
				usage := quotav1alpha1.ResourceList{
					corev1.ResourceRequestsCPU: resource.MustParse("2500m"), // 2.5 CPU > 2 CPU
				}

				reconciler.checkQuotaThresholds(testCRQ, usage)

				Expect(fakeRecorder.Events).To(HaveLen(1))
				event := <-fakeRecorder.Events
				Expect(event).To(ContainSubstring("QuotaExceeded"))
				Expect(event).To(ContainSubstring("requests.cpu"))
				Expect(event).To(ContainSubstring("2500m"))
				Expect(event).To(ContainSubstring("2"))
			})

			It("should not trigger violation when usage is within limits with fractional values", func() {
				usage := quotav1alpha1.ResourceList{
					corev1.ResourceRequestsCPU: resource.MustParse("1500m"), // 1.5 CPU < 2 CPU
				}

				reconciler.checkQuotaThresholds(testCRQ, usage)

				Expect(fakeRecorder.Events).To(BeEmpty())
			})

			It("should handle very small CPU violations correctly", func() {
				usage := quotav1alpha1.ResourceList{
					corev1.ResourceRequestsCPU: resource.MustParse("2001m"), // 2.001 CPU > 2 CPU
				}

				reconciler.checkQuotaThresholds(testCRQ, usage)

				Expect(fakeRecorder.Events).To(HaveLen(1))
				event := <-fakeRecorder.Events
				Expect(event).To(ContainSubstring("QuotaExceeded"))
				Expect(event).To(ContainSubstring("2001m"))
			})

			It("should not trigger violation for exact match", func() {
				usage := quotav1alpha1.ResourceList{
					corev1.ResourceRequestsCPU: resource.MustParse("2000m"), // Exactly 2 CPU
				}

				reconciler.checkQuotaThresholds(testCRQ, usage)

				Expect(fakeRecorder.Events).To(BeEmpty())
			})
		})

		Context("with binary memory units", func() {
			It("should trigger violation when memory usage exceeds limit", func() {
				usage := quotav1alpha1.ResourceList{
					corev1.ResourceRequestsMemory: resource.MustParse("5Gi"), // 5Gi > 4Gi
				}

				reconciler.checkQuotaThresholds(testCRQ, usage)

				Expect(fakeRecorder.Events).To(HaveLen(1))
				event := <-fakeRecorder.Events
				Expect(event).To(ContainSubstring("QuotaExceeded"))
				Expect(event).To(ContainSubstring("requests.memory"))
				Expect(event).To(ContainSubstring("5Gi"))
				Expect(event).To(ContainSubstring("4Gi"))
			})

			It("should not trigger violation when memory usage is within limits", func() {
				usage := quotav1alpha1.ResourceList{
					corev1.ResourceRequestsMemory: resource.MustParse("3Gi"), // 3Gi < 4Gi
				}

				reconciler.checkQuotaThresholds(testCRQ, usage)

				Expect(fakeRecorder.Events).To(BeEmpty())
			})

			It("should handle mixed binary and decimal memory units correctly", func() {
				usage := quotav1alpha1.ResourceList{
					corev1.ResourceRequestsMemory: resource.MustParse("4300Mi"), // ~4.3GB > 4Gi (~4.29GB)
				}

				reconciler.checkQuotaThresholds(testCRQ, usage)

				Expect(fakeRecorder.Events).To(HaveLen(1))
				event := <-fakeRecorder.Events
				Expect(event).To(ContainSubstring("QuotaExceeded"))
				Expect(event).To(ContainSubstring("4300Mi"))
			})
		})

		Context("with object count resources", func() {
			It("should trigger violation when pod count exceeds limit", func() {
				usage := quotav1alpha1.ResourceList{
					corev1.ResourcePods: resource.MustParse("12"), // 12 > 10
				}

				reconciler.checkQuotaThresholds(testCRQ, usage)

				Expect(fakeRecorder.Events).To(HaveLen(1))
				event := <-fakeRecorder.Events
				Expect(event).To(ContainSubstring("QuotaExceeded"))
				Expect(event).To(ContainSubstring("pods"))
				Expect(event).To(ContainSubstring("12"))
				Expect(event).To(ContainSubstring("10"))
			})

			It("should not trigger violation when pod count is within limits", func() {
				usage := quotav1alpha1.ResourceList{
					corev1.ResourcePods: resource.MustParse("8"), // 8 < 10
				}

				reconciler.checkQuotaThresholds(testCRQ, usage)

				Expect(fakeRecorder.Events).To(BeEmpty())
			})
		})

		Context("with multiple resource violations", func() {
			It("should trigger events for all violated resources", func() {
				usage := quotav1alpha1.ResourceList{
					corev1.ResourceRequestsCPU:    resource.MustParse("3"),   // 3 > 2
					corev1.ResourceRequestsMemory: resource.MustParse("5Gi"), // 5Gi > 4Gi
					corev1.ResourcePods:           resource.MustParse("15"),  // 15 > 10
				}

				reconciler.checkQuotaThresholds(testCRQ, usage)

				Expect(fakeRecorder.Events).To(HaveLen(3))

				// Collect all events
				var events []string
				for len(fakeRecorder.Events) > 0 {
					events = append(events, <-fakeRecorder.Events)
				}

				// Verify each resource has a violation event
				cpuFound := false
				memoryFound := false
				podsFound := false

				for _, event := range events {
					Expect(event).To(ContainSubstring("QuotaExceeded"))
					if strings.Contains(event, "requests.cpu") {
						cpuFound = true
						Expect(event).To(ContainSubstring("3"))
						Expect(event).To(ContainSubstring("2"))
					}
					if strings.Contains(event, "requests.memory") {
						memoryFound = true
						Expect(event).To(ContainSubstring("5Gi"))
						Expect(event).To(ContainSubstring("4Gi"))
					}
					if strings.Contains(event, "pods") {
						podsFound = true
						Expect(event).To(ContainSubstring("15"))
						Expect(event).To(ContainSubstring("10"))
					}
				}

				Expect(cpuFound).To(BeTrue(), "CPU violation event should be recorded")
				Expect(memoryFound).To(BeTrue(), "Memory violation event should be recorded")
				Expect(podsFound).To(BeTrue(), "Pods violation event should be recorded")
			})

			It("should only trigger events for violated resources, not all resources", func() {
				usage := quotav1alpha1.ResourceList{
					corev1.ResourceRequestsCPU:    resource.MustParse("1"),   // 1 < 2 (OK)
					corev1.ResourceRequestsMemory: resource.MustParse("5Gi"), // 5Gi > 4Gi (VIOLATION)
					corev1.ResourcePods:           resource.MustParse("5"),   // 5 < 10 (OK)
				}

				reconciler.checkQuotaThresholds(testCRQ, usage)

				Expect(fakeRecorder.Events).To(HaveLen(1))
				event := <-fakeRecorder.Events
				Expect(event).To(ContainSubstring("QuotaExceeded"))
				Expect(event).To(ContainSubstring("requests.memory"))
				Expect(event).To(ContainSubstring("5Gi"))
				Expect(event).To(ContainSubstring("4Gi"))
			})
		})

		Context("with edge cases", func() {
			It("should handle zero usage correctly", func() {
				usage := quotav1alpha1.ResourceList{
					corev1.ResourceRequestsCPU:    resource.MustParse("0"),
					corev1.ResourceRequestsMemory: resource.MustParse("0"),
					corev1.ResourcePods:           resource.MustParse("0"),
				}

				reconciler.checkQuotaThresholds(testCRQ, usage)

				Expect(fakeRecorder.Events).To(BeEmpty())
			})

			It("should handle zero limits correctly (no violations possible)", func() {
				crqWithZeroLimits := testCRQ.DeepCopy()
				crqWithZeroLimits.Spec.Hard = quotav1alpha1.ResourceList{
					corev1.ResourceRequestsCPU: resource.MustParse("0"),
				}

				usage := quotav1alpha1.ResourceList{
					corev1.ResourceRequestsCPU: resource.MustParse("1"), // 1 > 0, but zero limits are ignored
				}

				reconciler.checkQuotaThresholds(crqWithZeroLimits, usage)

				// Zero limits should be ignored (IsZero() check)
				Expect(fakeRecorder.Events).To(BeEmpty())
			})

			It("should handle missing resources in usage (treats as zero)", func() {
				usage := quotav1alpha1.ResourceList{
					// Missing CPU resource, should be treated as zero
					corev1.ResourceRequestsMemory: resource.MustParse("3Gi"), // Within limits
				}

				reconciler.checkQuotaThresholds(testCRQ, usage)

				// No violations since missing resources are treated as zero
				Expect(fakeRecorder.Events).To(BeEmpty())
			})

			It("should preserve original unit format in events", func() {
				usage := quotav1alpha1.ResourceList{
					corev1.ResourceRequestsCPU: resource.MustParse("2500m"), // Using millicores
				}

				reconciler.checkQuotaThresholds(testCRQ, usage)

				Expect(fakeRecorder.Events).To(HaveLen(1))
				event := <-fakeRecorder.Events
				// Should preserve the millicore format, not convert to decimal
				Expect(event).To(ContainSubstring("2500m"))
				// Limit should be displayed as originally specified (integer "2")
				Expect(event).To(ContainSubstring("2"))
			})
		})

		Context("with extended resources", func() {
			It("should handle GPU resources correctly", func() {
				crqWithGPU := testCRQ.DeepCopy()
				crqWithGPU.Spec.Hard[corev1.ResourceName("nvidia.com/gpu")] = resource.MustParse("2")

				usage := quotav1alpha1.ResourceList{
					corev1.ResourceName("nvidia.com/gpu"): resource.MustParse("3"), // 3 > 2
				}

				reconciler.checkQuotaThresholds(crqWithGPU, usage)

				Expect(fakeRecorder.Events).To(HaveLen(1))
				event := <-fakeRecorder.Events
				Expect(event).To(ContainSubstring("QuotaExceeded"))
				Expect(event).To(ContainSubstring("nvidia.com/gpu"))
				Expect(event).To(ContainSubstring("3"))
				Expect(event).To(ContainSubstring("2"))
			})
		})
	})

	Describe("containerTerminated", func() {
		Context("when handling container state transitions", func() {
			It("should return true when oldStatuses is empty and newStatuses has a terminated container", func() {
				oldStatuses := []corev1.ContainerStatus{}
				newStatuses := []corev1.ContainerStatus{
					{
						Name:  "new-container",
						State: corev1.ContainerState{Terminated: &corev1.ContainerStateTerminated{ExitCode: 0}},
					},
				}
				Expect(containerTerminated(oldStatuses, newStatuses)).To(BeTrue())
			})

			It("should return true when oldStatuses has fewer containers and new container is terminated", func() {
				oldStatuses := []corev1.ContainerStatus{
					{
						Name:  "existing-container",
						State: corev1.ContainerState{Running: &corev1.ContainerStateRunning{}},
					},
				}
				newStatuses := []corev1.ContainerStatus{
					{
						Name:  "existing-container",
						State: corev1.ContainerState{Running: &corev1.ContainerStateRunning{}},
					},
					{
						Name:  "new-container",
						State: corev1.ContainerState{Terminated: &corev1.ContainerStateTerminated{ExitCode: 0}},
					},
				}
				Expect(containerTerminated(oldStatuses, newStatuses)).To(BeTrue())
			})

			It("should return true when a container transitions from running to terminated", func() {
				oldStatuses := []corev1.ContainerStatus{
					{
						Name:  "app-container",
						State: corev1.ContainerState{Running: &corev1.ContainerStateRunning{}},
					},
				}
				newStatuses := []corev1.ContainerStatus{
					{
						Name:  "app-container",
						State: corev1.ContainerState{Terminated: &corev1.ContainerStateTerminated{ExitCode: 0}},
					},
				}
				Expect(containerTerminated(oldStatuses, newStatuses)).To(BeTrue())
			})

			It("should return true when a container transitions from waiting to terminated", func() {
				oldStatuses := []corev1.ContainerStatus{
					{
						Name:  "init-container",
						State: corev1.ContainerState{Waiting: &corev1.ContainerStateWaiting{Reason: "PodInitializing"}},
					},
				}
				newStatuses := []corev1.ContainerStatus{
					{
						Name:  "init-container",
						State: corev1.ContainerState{Terminated: &corev1.ContainerStateTerminated{ExitCode: 0}},
					},
				}
				Expect(containerTerminated(oldStatuses, newStatuses)).To(BeTrue())
			})

			It("should return false when container is already terminated in both old and new statuses", func() {
				oldStatuses := []corev1.ContainerStatus{
					{
						Name:  "app-container",
						State: corev1.ContainerState{Terminated: &corev1.ContainerStateTerminated{ExitCode: 0}},
					},
				}
				newStatuses := []corev1.ContainerStatus{
					{
						Name:  "app-container",
						State: corev1.ContainerState{Terminated: &corev1.ContainerStateTerminated{ExitCode: 0}},
					},
				}
				Expect(containerTerminated(oldStatuses, newStatuses)).To(BeFalse())
			})

			It("should return false when no containers are terminated", func() {
				oldStatuses := []corev1.ContainerStatus{
					{
						Name:  "app-container",
						State: corev1.ContainerState{Running: &corev1.ContainerStateRunning{}},
					},
				}
				newStatuses := []corev1.ContainerStatus{
					{
						Name:  "app-container",
						State: corev1.ContainerState{Running: &corev1.ContainerStateRunning{}},
					},
				}
				Expect(containerTerminated(oldStatuses, newStatuses)).To(BeFalse())
			})

			It("should return true when one of multiple containers transitions to terminated", func() {
				oldStatuses := []corev1.ContainerStatus{
					{
						Name:  "app-container-1",
						State: corev1.ContainerState{Running: &corev1.ContainerStateRunning{}},
					},
					{
						Name:  "app-container-2",
						State: corev1.ContainerState{Running: &corev1.ContainerStateRunning{}},
					},
					{
						Name:  "app-container-3",
						State: corev1.ContainerState{Running: &corev1.ContainerStateRunning{}},
					},
				}
				newStatuses := []corev1.ContainerStatus{
					{
						Name:  "app-container-1",
						State: corev1.ContainerState{Running: &corev1.ContainerStateRunning{}},
					},
					{
						Name:  "app-container-2",
						State: corev1.ContainerState{Terminated: &corev1.ContainerStateTerminated{ExitCode: 0}},
					},
					{
						Name:  "app-container-3",
						State: corev1.ContainerState{Running: &corev1.ContainerStateRunning{}},
					},
				}
				Expect(containerTerminated(oldStatuses, newStatuses)).To(BeTrue())
			})

			It("should return false when multiple containers are terminated but were already terminated", func() {
				oldStatuses := []corev1.ContainerStatus{
					{
						Name:  "app-container-1",
						State: corev1.ContainerState{Terminated: &corev1.ContainerStateTerminated{ExitCode: 0}},
					},
					{
						Name:  "app-container-2",
						State: corev1.ContainerState{Terminated: &corev1.ContainerStateTerminated{ExitCode: 1}},
					},
				}
				newStatuses := []corev1.ContainerStatus{
					{
						Name:  "app-container-1",
						State: corev1.ContainerState{Terminated: &corev1.ContainerStateTerminated{ExitCode: 0}},
					},
					{
						Name:  "app-container-2",
						State: corev1.ContainerState{Terminated: &corev1.ContainerStateTerminated{ExitCode: 1}},
					},
				}
				Expect(containerTerminated(oldStatuses, newStatuses)).To(BeFalse())
			})

			It("should return false when both oldStatuses and newStatuses are empty", func() {
				oldStatuses := []corev1.ContainerStatus{}
				newStatuses := []corev1.ContainerStatus{}
				Expect(containerTerminated(oldStatuses, newStatuses)).To(BeFalse())
			})

			It("should return false when newStatuses is empty", func() {
				oldStatuses := []corev1.ContainerStatus{
					{
						Name:  "app-container",
						State: corev1.ContainerState{Running: &corev1.ContainerStateRunning{}},
					},
				}
				newStatuses := []corev1.ContainerStatus{}
				Expect(containerTerminated(oldStatuses, newStatuses)).To(BeFalse())
			})

			It("should handle containers with error exit codes correctly", func() {
				oldStatuses := []corev1.ContainerStatus{
					{
						Name:  "failing-container",
						State: corev1.ContainerState{Running: &corev1.ContainerStateRunning{}},
					},
				}
				newStatuses := []corev1.ContainerStatus{
					{
						Name:  "failing-container",
						State: corev1.ContainerState{Terminated: &corev1.ContainerStateTerminated{ExitCode: 1, Reason: "Error"}},
					},
				}
				Expect(containerTerminated(oldStatuses, newStatuses)).To(BeTrue())
			})

			It("should correctly handle mixed scenarios with some containers terminated and some new", func() {
				oldStatuses := []corev1.ContainerStatus{
					{
						Name:  "already-terminated",
						State: corev1.ContainerState{Terminated: &corev1.ContainerStateTerminated{ExitCode: 0}},
					},
					{
						Name:  "running-container",
						State: corev1.ContainerState{Running: &corev1.ContainerStateRunning{}},
					},
				}
				newStatuses := []corev1.ContainerStatus{
					{
						Name:  "already-terminated",
						State: corev1.ContainerState{Terminated: &corev1.ContainerStateTerminated{ExitCode: 0}},
					},
					{
						Name:  "running-container",
						State: corev1.ContainerState{Running: &corev1.ContainerStateRunning{}},
					},
					{
						Name:  "newly-terminated",
						State: corev1.ContainerState{Terminated: &corev1.ContainerStateTerminated{ExitCode: 0}},
					},
				}
				Expect(containerTerminated(oldStatuses, newStatuses)).To(BeTrue())
			})
		})
	})
})
