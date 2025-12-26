package e2e

import (
	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"

	"time"

	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/api/resource"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"

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
		It("should deny pod updates that would exceed limits", func() {
			pod, err := testutils.CreatePod(
				ctx,
				k8sClient,
				testNamespace,
				"test-pod-"+testSuffix,
				corev1.ResourceList{
					corev1.ResourceCPU: resource.MustParse("10m"),
				}, nil)
			Expect(err).NotTo(HaveOccurred())
			DeferCleanup(func() {
				_ = k8sClient.Delete(ctx, pod)
			})

			pod.Spec.Containers[0].Resources.Requests[corev1.ResourceCPU] = resource.MustParse("200m") // Exceeds limit
			Expect(k8sClient.Update(ctx, pod)).ToNot(Succeed())
		})

		It("should deny updates to pods with multiple containers exceeding limits", func() {
			pod, err := testutils.CreatePod(
				ctx,
				k8sClient,
				testNamespace,
				"test-pod-update-multi-container-"+testSuffix,
				corev1.ResourceList{
					corev1.ResourceCPU: resource.MustParse("10m"),
				},
				corev1.ResourceList{
					corev1.ResourceCPU: resource.MustParse("10m"),
				})
			Expect(err).NotTo(HaveOccurred())
			DeferCleanup(func() {
				_ = k8sClient.Delete(ctx, pod)
			})

			pod.Spec.Containers[0].Resources.Requests[corev1.ResourceCPU] = resource.MustParse("200m") // Exceeds limit
			Expect(k8sClient.Update(ctx, pod)).ToNot(Succeed())
		})

		It("should deny updates to pods with init containers exceeding limits", func() {
			pod, err := testutils.CreatePodWithContainers(
				ctx, k8sClient, testNamespace, "test-pod-update-init-container-"+testSuffix,
				[]corev1.Container{
					{
						Name:  "main-container",
						Image: "nginx:latest",
						Resources: corev1.ResourceRequirements{
							Requests: corev1.ResourceList{
								corev1.ResourceCPU: resource.MustParse("30m"),
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

			pod.Spec.InitContainers[0].Resources.Requests[corev1.ResourceCPU] = resource.MustParse("300m") // Exceeds limit
			Expect(k8sClient.Update(ctx, pod)).ToNot(Succeed())
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
