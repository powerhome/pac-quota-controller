package e2e

import (
	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"

	"time"

	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/api/resource"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"

	quotav1alpha1 "github.com/powerhome/pac-quota-controller/api/v1alpha1"
	testutils "github.com/powerhome/pac-quota-controller/test/utils"
)

var _ = Describe("Pod Admission Webhook Tests", func() {
	var (
		testNamespace string
		testCRQName   string
		testSuffix    string
		ns            *corev1.Namespace
		crq           *quotav1alpha1.ClusterResourceQuota
	)

	BeforeEach(func() {
		testSuffix = testutils.GenerateTestSuffix()
		testNamespace = testutils.GenerateResourceName("pod-webhook-ns-" + testSuffix)
		testCRQName = testutils.GenerateResourceName("pod-webhook-crq-" + testSuffix)

		var err error
		ns, err = testutils.CreateNamespace(ctx, k8sClient, testNamespace, map[string]string{
			"pod-webhook-test": "test-label-" + testSuffix,
		})
		Expect(err).NotTo(HaveOccurred())

		// Ensure no other CRQs target the namespace
		existingCRQs, err := testutils.ListClusterResourceQuotas(ctx, k8sClient, &metav1.LabelSelector{
			MatchLabels: map[string]string{
				"pod-webhook-test": "test-label-" + testSuffix,
			},
		})
		Expect(err).NotTo(HaveOccurred())
		Expect(existingCRQs.Items).To(BeEmpty(), "Namespace already targeted by another CRQ")

		crq, err = testutils.CreateClusterResourceQuota(ctx, k8sClient, testCRQName, &metav1.LabelSelector{
			MatchLabels: map[string]string{
				"pod-webhook-test": "test-label-" + testSuffix,
			},
		}, quotav1alpha1.ResourceList{
			corev1.ResourceRequestsCPU: resource.MustParse("100m"),
			corev1.ResourcePods:        resource.MustParse("2"),
		})
		Expect(err).NotTo(HaveOccurred())
	})

	AfterEach(func() {
		DeferCleanup(func() {
			_ = k8sClient.Delete(ctx, ns)
		})
		DeferCleanup(func() {
			_ = k8sClient.Delete(ctx, crq)
		})
	})

	Context("Pod Creation Webhook", func() {
		It("should allow pod creation when within limits", func() {
			pod, err := testutils.CreatePod(
				ctx,
				k8sClient,
				testNamespace,
				"test-pod-"+testSuffix,
				corev1.ResourceList{
					corev1.ResourceCPU: resource.MustParse("50m"),
				},
				nil) // Within 100m limit
			Expect(err).NotTo(HaveOccurred())
			DeferCleanup(func() {
				_ = k8sClient.Delete(ctx, pod)
			})
		})

		It("should deny pod creation when it would exceed CPU limits", func() {
			Expect(testutils.WaitForCRQResourceUsage(
				ctx, k8sClient, testCRQName, corev1.ResourceRequestsCPU, resource.MustParse("0"),
			)).To(Succeed())
			_, err := testutils.CreatePod(
				ctx,
				k8sClient,
				testNamespace,
				"test-pod-"+testSuffix,
				corev1.ResourceList{
					corev1.ResourceCPU: resource.MustParse("200m"),
				},
				nil) // Exceeds 100m limit
			Expect(err).To(HaveOccurred())
			Expect(err.Error()).To(ContainSubstring("ClusterResourceQuota CPU requests validation failed"))
		})

		It("should allow pod creation with multiple containers within limits", func() {
			pod, err := testutils.CreatePodWithContainers(
				ctx, k8sClient, testNamespace, "test-pod-multi-container-"+testSuffix,
				[]corev1.Container{
					{
						Name:  "container-1",
						Image: "nginx:latest",
						Resources: corev1.ResourceRequirements{
							Requests: corev1.ResourceList{
								corev1.ResourceCPU: resource.MustParse("50m"),
							},
						},
					},
					{
						Name:  "container-2",
						Image: "nginx:latest",
						Resources: corev1.ResourceRequirements{
							Requests: corev1.ResourceList{
								corev1.ResourceCPU: resource.MustParse("50m"),
							},
						},
					},
				}, nil)
			Expect(err).NotTo(HaveOccurred())
			DeferCleanup(func() {
				_ = k8sClient.Delete(ctx, pod)
			})
		})

		It("should deny pod creation with init containers exceeding limits", func() {
			Expect(testutils.WaitForCRQResourceUsage(
				ctx, k8sClient, testCRQName, corev1.ResourceRequestsCPU, resource.MustParse("0"),
			)).To(Succeed())
			_, err := testutils.CreatePodWithContainers(
				ctx, k8sClient, testNamespace, "test-pod-init-container-"+testSuffix,
				[]corev1.Container{
					{
						Name:  "main-container",
						Image: "nginx:latest",
						Resources: corev1.ResourceRequirements{
							Requests: corev1.ResourceList{
								corev1.ResourceCPU: resource.MustParse("50m"),
							},
						},
					},
				}, []corev1.Container{
					{
						Name:  "init-container",
						Image: "nginx:latest",
						Resources: corev1.ResourceRequirements{
							Requests: corev1.ResourceList{
								corev1.ResourceCPU: resource.MustParse("200m"),
							},
						},
					},
				})
			// Should fail due to init container exceeding limit
			Expect(err).To(HaveOccurred())
		})

		It("should allow pod creation with both main and init containers within limits", func() {
			pod, err := testutils.CreatePodWithContainers(
				ctx, k8sClient, testNamespace, "test-pod-init-container-within-limits-"+testSuffix,
				[]corev1.Container{
					{
						Name:  "main-container",
						Image: "nginx:latest",
						Resources: corev1.ResourceRequirements{
							Requests: corev1.ResourceList{
								corev1.ResourceCPU: resource.MustParse("50m"),
							},
						},
					},
				}, []corev1.Container{
					{
						Name:  "init-container",
						Image: "nginx:latest",
						Resources: corev1.ResourceRequirements{
							Requests: corev1.ResourceList{
								corev1.ResourceCPU: resource.MustParse("50m"),
							},
						},
					},
				})
			Expect(err).NotTo(HaveOccurred())
			DeferCleanup(func() {
				_ = k8sClient.Delete(ctx, pod)
			})
		})

		It("should allow pod creation when init + app > limit but max(init, app) <= limit", func() {
			// Limit is 100m.
			// init=60m, app=70m. sum=130m (would have been denied before).
			// max(60, 70)=70m. Within 100m limit.
			pod, err := testutils.CreatePodWithContainers(
				ctx, k8sClient, testNamespace, "test-pod-max-logic-"+testSuffix,
				[]corev1.Container{
					{
						Name:  "main-container",
						Image: "nginx:latest",
						Resources: corev1.ResourceRequirements{
							Requests: corev1.ResourceList{
								corev1.ResourceCPU: resource.MustParse("70m"),
							},
						},
					},
				}, []corev1.Container{
					{
						Name:  "init-container",
						Image: "nginx:latest",
						Resources: corev1.ResourceRequirements{
							Requests: corev1.ResourceList{
								corev1.ResourceCPU: resource.MustParse("60m"),
							},
						},
					},
				})
			Expect(err).NotTo(HaveOccurred())
			DeferCleanup(func() {
				_ = k8sClient.Delete(ctx, pod)
			})
		})
	})

	Context("Pod Update Webhook", func() {
		It("should allow metadata updates when pod count quota is at the limit", func() {
			// Fill pod count quota (2).
			pod1, err := testutils.CreatePod(
				ctx, k8sClient, testNamespace, "test-pod-update-1-"+testSuffix,
				corev1.ResourceList{corev1.ResourceCPU: resource.MustParse("10m")}, nil)
			Expect(err).NotTo(HaveOccurred())
			DeferCleanup(func() { _ = k8sClient.Delete(ctx, pod1) })

			pod2, err := testutils.CreatePod(
				ctx, k8sClient, testNamespace, "test-pod-update-2-"+testSuffix,
				corev1.ResourceList{corev1.ResourceCPU: resource.MustParse("10m")}, nil)
			Expect(err).NotTo(HaveOccurred())
			DeferCleanup(func() { _ = k8sClient.Delete(ctx, pod2) })

			Expect(testutils.WaitForCRQResourceUsage(
				ctx, k8sClient, testCRQName, corev1.ResourcePods, resource.MustParse("2"),
			)).To(Succeed())

			// Re-fetch and retry on conflict; kubelet keeps updating pod status concurrently.
			Eventually(func() error {
				var fresh corev1.Pod
				if err := k8sClient.Get(ctx, types.NamespacedName{Name: pod1.Name, Namespace: pod1.Namespace}, &fresh); err != nil {
					return err
				}
				if fresh.Labels == nil {
					fresh.Labels = map[string]string{}
				}
				fresh.Labels["updated"] = "yes"
				return k8sClient.Update(ctx, &fresh)
			}, 30*time.Second, 500*time.Millisecond).Should(Succeed())
		})
	})

	Context("Pod Resize Subresource Webhook", func() {
		// These specs exercise the pods/resize verb directly (K8s 1.33+ GA).
		// They verify that the webhook charges only the positive delta on UPDATE
		// instead of the full new request -- otherwise resizing a pod that
		// currently fits inside the quota would be rejected as if it were a new
		// allocation.

		// waitForPodRunning blocks until the pod reaches the Running phase. The
		// apiserver requires Running before accepting an in-place resize, so we
		// must reach this state before exercising the webhook.
		waitForPodRunning := func(name string) {
			Eventually(func() corev1.PodPhase {
				p, err := testutils.GetPod(ctx, k8sClient, testNamespace, name)
				if err != nil {
					return ""
				}
				return p.Status.Phase
			}, 2*time.Minute, 2*time.Second).Should(Equal(corev1.PodRunning))
		}

		// resizePodCPU sends an UPDATE on the pods/resize subresource with new
		// CPU requests on container[0] and returns the apiserver error.
		resizePodCPU := func(pod *corev1.Pod, cpu resource.Quantity) error {
			patch := []byte(`{"spec":{"containers":[{"name":"` +
				pod.Spec.Containers[0].Name +
				`","resources":{"requests":{"cpu":"` + cpu.String() + `"}}}]}}`)
			_, err := clientSet.CoreV1().Pods(pod.Namespace).Patch(
				ctx, pod.Name, types.StrategicMergePatchType, patch,
				metav1.PatchOptions{}, "resize",
			)
			return err
		}

		It("allows a resize-up whose delta fits within the remaining quota", func() {
			pod, err := testutils.CreatePod(ctx, k8sClient, testNamespace,
				"resize-fit-"+testSuffix,
				corev1.ResourceList{corev1.ResourceCPU: resource.MustParse("40m")},
				nil)
			Expect(err).NotTo(HaveOccurred())
			DeferCleanup(func() { _ = k8sClient.Delete(ctx, pod) })

			waitForPodRunning(pod.Name)
			Expect(resizePodCPU(pod, resource.MustParse("90m"))).To(Succeed(),
				"resize within quota (delta 50m, total 90m <= 100m) should be allowed")
		})

		It("rejects a resize-up whose delta would exceed the quota", func() {
			pod, err := testutils.CreatePod(ctx, k8sClient, testNamespace,
				"resize-deny-"+testSuffix,
				corev1.ResourceList{corev1.ResourceCPU: resource.MustParse("60m")},
				nil)
			Expect(err).NotTo(HaveOccurred())
			DeferCleanup(func() { _ = k8sClient.Delete(ctx, pod) })

			waitForPodRunning(pod.Name)
			err = resizePodCPU(pod, resource.MustParse("200m"))
			Expect(err).To(HaveOccurred())
			Expect(err.Error()).To(ContainSubstring("CPU requests validation failed"))
		})

		It("allows a resize-up when pod count is at the limit (no +1 charge on UPDATE)", func() {
			// CRQ has pods=2. Create both pods so the count is at the limit; a
			// resize on either pod must not be rejected by the pod-count rule
			// because UPDATE only charges resource deltas, not +1 pod.
			pod1, err := testutils.CreatePod(ctx, k8sClient, testNamespace,
				"resize-count-1-"+testSuffix,
				corev1.ResourceList{corev1.ResourceCPU: resource.MustParse("20m")},
				nil)
			Expect(err).NotTo(HaveOccurred())
			DeferCleanup(func() { _ = k8sClient.Delete(ctx, pod1) })

			pod2, err := testutils.CreatePod(ctx, k8sClient, testNamespace,
				"resize-count-2-"+testSuffix,
				corev1.ResourceList{corev1.ResourceCPU: resource.MustParse("20m")},
				nil)
			Expect(err).NotTo(HaveOccurred())
			DeferCleanup(func() { _ = k8sClient.Delete(ctx, pod2) })

			waitForPodRunning(pod1.Name)
			Expect(resizePodCPU(pod1, resource.MustParse("40m"))).To(Succeed(),
				"resize at pod-count limit should still be allowed on UPDATE")
		})
	})

	Context("Pod Deletion Webhook", func() {
		It("should always allow pod deletion", func() {
			pod, err := testutils.CreatePod(
				ctx,
				k8sClient,
				testNamespace,
				"test-pod-"+testSuffix,
				corev1.ResourceList{
					corev1.ResourceCPU: resource.MustParse("50m"),
				},
				nil)
			Expect(err).NotTo(HaveOccurred())
			Expect(k8sClient.Delete(ctx, pod)).To(Succeed())

			// Verify deletion
			Eventually(func() bool {
				_, getErr := testutils.GetPod(ctx, k8sClient, testNamespace, pod.Name)
				return errors.IsNotFound(getErr)
			}, time.Minute, 5*time.Second).Should(BeTrue())
		})

		It("should always allow deletion of pods with init containers", func() {
			pod, err := testutils.CreatePod(
				ctx,
				k8sClient,
				testNamespace,
				"test-pod-delete-init-container-"+testSuffix,
				corev1.ResourceList{
					corev1.ResourceCPU: resource.MustParse("50m"),
				}, nil)
			Expect(err).NotTo(HaveOccurred())
			Expect(k8sClient.Delete(ctx, pod)).To(Succeed())

			// Verify deletion
			Eventually(func() bool {
				_, getErr := testutils.GetPod(ctx, k8sClient, testNamespace, pod.Name)
				return errors.IsNotFound(getErr)
			}, time.Minute, 5*time.Second).Should(BeTrue())
		})

		It("should always allow deletion of pods with multiple containers", func() {
			pod, err := testutils.CreatePodWithContainers(
				ctx, k8sClient, testNamespace, "test-pod-delete-multi-container-"+testSuffix,
				[]corev1.Container{
					{
						Name:  "container-1",
						Image: "nginx:latest",
						Resources: corev1.ResourceRequirements{
							Requests: corev1.ResourceList{
								corev1.ResourceCPU: resource.MustParse("50m"),
							},
						},
					},
					{
						Name:  "container-2",
						Image: "nginx:latest",
						Resources: corev1.ResourceRequirements{
							Requests: corev1.ResourceList{
								corev1.ResourceCPU: resource.MustParse("50m"),
							},
						},
					},
				}, nil)
			Expect(err).NotTo(HaveOccurred())
			Expect(k8sClient.Delete(ctx, pod)).To(Succeed())

			// Verify deletion
			Eventually(func() bool {
				_, getErr := testutils.GetPod(ctx, k8sClient, testNamespace, pod.Name)
				return errors.IsNotFound(getErr)
			}, time.Minute, 5*time.Second).Should(BeTrue())
		})
	})

	Context("Pod Count Quota Tests", func() {
		It("should allow pod creation when within pod count limits", func() {
			// Create first pod
			pod1, err := testutils.CreatePodWithContainers(
				ctx, k8sClient, testNamespace, testutils.GenerateResourceName("test-pod-1"),
				[]corev1.Container{
					{
						Name:  "test-container",
						Image: "nginx:latest",
						Resources: corev1.ResourceRequirements{
							Requests: corev1.ResourceList{
								corev1.ResourceCPU:    resource.MustParse("10m"),
								corev1.ResourceMemory: resource.MustParse("128Mi"),
							},
						},
					},
				}, nil)
			Expect(err).NotTo(HaveOccurred())

			// Create second pod - should succeed
			pod2, err := testutils.CreatePodWithContainers(
				ctx, k8sClient, testNamespace, testutils.GenerateResourceName("test-pod-2"),
				[]corev1.Container{
					{
						Name:  "test-container",
						Image: "nginx:latest",
						Resources: corev1.ResourceRequirements{
							Requests: corev1.ResourceList{
								corev1.ResourceCPU:    resource.MustParse("10m"),
								corev1.ResourceMemory: resource.MustParse("128Mi"),
							},
						},
					},
				}, nil)
			Expect(err).NotTo(HaveOccurred())

			// Cleanup
			Expect(k8sClient.Delete(ctx, pod1)).To(Succeed())
			Expect(k8sClient.Delete(ctx, pod2)).To(Succeed())
		})

		It("should deny pod creation when it would exceed pod count limits", func() {
			// Create first pod - should succeed
			pod1, err := testutils.CreatePodWithContainers(
				ctx, k8sClient, testNamespace, testutils.GenerateResourceName("test-pod-1"),
				[]corev1.Container{
					{
						Name:  "test-container",
						Image: "nginx:latest",
						Resources: corev1.ResourceRequirements{
							Requests: corev1.ResourceList{
								corev1.ResourceCPU:    resource.MustParse("10m"),
								corev1.ResourceMemory: resource.MustParse("128Mi"),
							},
						},
					},
				},
				nil,
			)
			Expect(err).NotTo(HaveOccurred())

			// Create second pod - should succeed
			pod2, err := testutils.CreatePodWithContainers(
				ctx, k8sClient, testNamespace, testutils.GenerateResourceName("test-pod-2"),
				[]corev1.Container{
					{
						Name:  "test-container",
						Image: "nginx:latest",
						Resources: corev1.ResourceRequirements{
							Requests: corev1.ResourceList{
								corev1.ResourceCPU:    resource.MustParse("10m"),
								corev1.ResourceMemory: resource.MustParse("128Mi"),
							},
						},
					},
				}, nil)
			Expect(err).NotTo(HaveOccurred())
			Expect(testutils.WaitForCRQResourceUsage(
				ctx, k8sClient, testCRQName, corev1.ResourcePods, resource.MustParse("2"),
			)).To(Succeed())
			// Create third pod - should fail
			_, err = testutils.CreatePodWithContainers(
				ctx, k8sClient, testNamespace, testutils.GenerateResourceName("test-pod-3"),
				[]corev1.Container{
					{
						Name:  "test-container",
						Image: "nginx:latest",
						Resources: corev1.ResourceRequirements{
							Requests: corev1.ResourceList{
								corev1.ResourceCPU:    resource.MustParse("10m"),
								corev1.ResourceMemory: resource.MustParse("128Mi"),
							},
						},
					},
				},
				nil,
			)
			Expect(err).To(HaveOccurred())
			Expect(err.Error()).To(ContainSubstring("pods limit exceeded"))

			// Cleanup
			Expect(k8sClient.Delete(ctx, pod1)).To(Succeed())
			// Cleanup
			Expect(k8sClient.Delete(ctx, pod2)).To(Succeed())
		})
	})
})
