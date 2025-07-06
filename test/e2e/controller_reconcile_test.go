// Removed unused import
// Updated calls to `GetCRQStatusNamespaces` and `GetCRQStatusUsage` to use the correct arguments.

package e2e

import (
	"context"
	"fmt"
	"time"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	"sigs.k8s.io/controller-runtime/pkg/client"

	quotav1alpha1 "github.com/powerhome/pac-quota-controller/api/v1alpha1"
	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/resource"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"

	testutils "github.com/powerhome/pac-quota-controller/test/utils"
)

const (
	Timeout  = time.Second * 30
	Interval = time.Second * 5
)

var _ = Describe("ClusterResourceQuota Controller E2E Tests", func() {
	var (
		ctx    context.Context
		suffix string
		ns     *corev1.Namespace
		crq    *quotav1alpha1.ClusterResourceQuota
		nsName string
	)

	BeforeEach(func() {
		ctx = context.Background()

		// Generate unique suffix for each test to avoid collisions
		suffix = testutils.GenerateTestSuffix()
		nsName = "test-ns-" + suffix

		var err error
		ns, err = testutils.CreateNamespace(
			ctx,
			k8sClient,
			nsName,
			map[string]string{"team": "test-" + suffix}, // Use unique label per test
		)
		Expect(err).NotTo(HaveOccurred())
		Expect(ns).NotTo(BeNil())

		crq, err = testutils.CreateClusterResourceQuota(
			ctx,
			k8sClient,
			"crq-"+suffix,
			&metav1.LabelSelector{
				MatchLabels: map[string]string{"team": "test-" + suffix}, // Use unique selector per test
			}, quotav1alpha1.ResourceList{
				corev1.ResourceRequestsCPU:    resource.MustParse("2"),   // 2 CPU cores request limit
				corev1.ResourceRequestsMemory: resource.MustParse("4Gi"), // 4GB memory request limit
				corev1.ResourceLimitsCPU:      resource.MustParse("4"),   // 4 CPU cores limit
				corev1.ResourceLimitsMemory:   resource.MustParse("8Gi"), // 8GB memory limit
				"example.com/gpu":             resource.MustParse("2"),   // 2 GPU units
				"hugepages-2Mi":               resource.MustParse("4Gi"), // 4GB hugepages
			},
		)
		Expect(err).NotTo(HaveOccurred())
		Expect(crq).NotTo(BeNil())
	})

	AfterEach(func() {
		if crq != nil {
			_ = k8sClient.Delete(ctx, crq)
		}
		if ns != nil {
			_ = k8sClient.Delete(ctx, ns)
		}
	})

	Context("Namespace Selection", func() {
		It("should exclude namespaces with the exclusion label", func() {
			ns := &corev1.Namespace{
				ObjectMeta: metav1.ObjectMeta{
					Name:   "exluded-ns-" + suffix,
					Labels: map[string]string{"pac-quota-controller.powerapp.cloud/exclude": "true"},
				},
			}
			Expect(k8sClient.Create(ctx, ns)).To(Succeed())
			DeferCleanup(func() {
				_ = k8sClient.Delete(ctx, ns)
			})

			Eventually(func() []string {
				return testutils.GetRefreshedCRQStatusNamespaces(ctx, k8sClient, crq.Name)
			}, Timeout, Interval).ShouldNot(ContainElement("exluded-ns-" + suffix))
		})
		It("should include namespaces matching the selector", func() {
			ns1, err := testutils.CreateNamespace(
				ctx,
				k8sClient,
				"included-ns1-"+suffix,
				map[string]string{"team": "test-" + suffix},
			)
			Expect(err).NotTo(HaveOccurred())
			DeferCleanup(func() {
				_ = k8sClient.Delete(ctx, ns1)
			})
			ns2, err := testutils.CreateNamespace(
				ctx,
				k8sClient,
				"included-ns2-"+suffix,
				map[string]string{"team": "test-" + suffix},
			)
			Expect(err).NotTo(HaveOccurred())
			DeferCleanup(func() {
				_ = k8sClient.Delete(ctx, ns2)
			})
			Eventually(func() []string {
				return testutils.GetRefreshedCRQStatusNamespaces(ctx, k8sClient, crq.Name)
			}, Timeout, Interval).Should(ContainElements(
				ns1.Name,
				ns2.Name,
			))
		})

		Context("Compute Resources", func() {
			It("should reconcile CPU and memory usage", func() {
				// Simulate resource usage
				pod, err := testutils.CreatePod(
					ctx,
					k8sClient,
					nsName,
					"test-pod-"+suffix,
					corev1.ResourceList{
						corev1.ResourceCPU:    resource.MustParse("500m"),
						corev1.ResourceMemory: resource.MustParse("1Gi"),
					},
					corev1.ResourceList{
						corev1.ResourceCPU:    resource.MustParse("1"),
						corev1.ResourceMemory: resource.MustParse("1Gi"),
					})
				Expect(err).NotTo(HaveOccurred())
				DeferCleanup(func() {
					_ = k8sClient.Delete(ctx, pod)
				})
				Eventually(func() error {
					usage := testutils.GetRefreshedCRQStatusUsage(ctx, k8sClient, crq.Name)
					return testutils.ExpectCRQUsageToMatch(usage, map[string]string{
						"requests.cpu":    "500m", // Uses request value (500m)
						"requests.memory": "1Gi",  // Uses request value (1Gi)
						"limits.cpu":      "1",    // Uses limit value (1)
						"limits.memory":   "1Gi",  // Uses limit value (1Gi)
					})
				}, Timeout, Interval).Should(Succeed())
			})

			It("should reconcile CPU and memory limits", func() {
				// Simulate resource usage
				pod, err := testutils.CreatePod(
					ctx,
					k8sClient,
					nsName,
					"test-pod-limits-"+suffix,
					corev1.ResourceList{
						corev1.ResourceCPU:    resource.MustParse("500m"),
						corev1.ResourceMemory: resource.MustParse("1Gi"),
					},
					corev1.ResourceList{
						corev1.ResourceCPU:    resource.MustParse("2"),
						corev1.ResourceMemory: resource.MustParse("2Gi"),
					})
				Expect(err).NotTo(HaveOccurred())
				DeferCleanup(func() {
					_ = k8sClient.Delete(ctx, pod)
				})
				Eventually(func() error {
					usage := testutils.GetRefreshedCRQStatusUsage(ctx, k8sClient, crq.Name)
					return testutils.ExpectCRQUsageToMatch(usage, map[string]string{
						"requests.cpu":    "500m", // Uses request value (500m)
						"requests.memory": "1Gi",  // Uses request value (1Gi)
						"limits.cpu":      "2",    // Uses limit value (2)
						"limits.memory":   "2Gi",  // Uses limit value (2Gi)
					})
				}, Timeout, Interval).Should(Succeed())
			})
		})

		Context("Extended Resources", func() {
			It("should reconcile extended resources usage", func() {
				// Simulate resource usage
				pod, err := testutils.CreatePod(
					ctx,
					k8sClient,
					nsName,
					"test-pod-"+suffix,
					corev1.ResourceList{
						"example.com/gpu": resource.MustParse("1"),
					},
					corev1.ResourceList{
						"example.com/gpu": resource.MustParse("1"),
					})
				Expect(err).NotTo(HaveOccurred())
				DeferCleanup(func() {
					_ = k8sClient.Delete(ctx, pod)
				})

				Eventually(func() error {
					usage := testutils.GetRefreshedCRQStatusUsage(ctx, k8sClient, crq.Name)
					return testutils.ExpectCRQUsageToMatch(usage, map[string]string{
						"example.com/gpu": "1", // GPU resource used
					})
				}, Timeout, Interval).Should(Succeed())
			})
		})

		Context("Hugepages", func() {
			It("should reconcile hugepages usage", func() {
				// Simulate resource usage - hugepages require CPU or memory to be specified
				pod, err := testutils.CreatePod(
					ctx,
					k8sClient,
					nsName,
					"test-pod-"+suffix,
					corev1.ResourceList{
						corev1.ResourceCPU:    resource.MustParse("100m"),  // Required for hugepages
						corev1.ResourceMemory: resource.MustParse("128Mi"), // Required for hugepages
						"hugepages-2Mi":       resource.MustParse("2Gi"),
					},
					corev1.ResourceList{
						corev1.ResourceCPU:    resource.MustParse("200m"),
						corev1.ResourceMemory: resource.MustParse("256Mi"),
						"hugepages-2Mi":       resource.MustParse("2Gi"),
					})
				Expect(err).NotTo(HaveOccurred())
				DeferCleanup(func() {
					_ = k8sClient.Delete(ctx, pod)
				})

				Eventually(func() error {
					usage := testutils.GetRefreshedCRQStatusUsage(ctx, k8sClient, crq.Name)
					return testutils.ExpectCRQUsageToMatch(usage, map[string]string{
						"requests.cpu":    "100m",  // CPU request used
						"requests.memory": "128Mi", // Memory request used
						"limits.cpu":      "200m",  // CPU limit used
						"limits.memory":   "256Mi", // Memory limit used
						"hugepages-2Mi":   "2Gi",   // Hugepages used
					})
				}, Timeout, Interval).Should(Succeed())
			})
		})

		Context("Edge Cases", func() {
			It("should handle no matching namespaces", func() {
				// Create a CRQ with a non-matching selector
				nonMatchingCRQ, err := testutils.CreateClusterResourceQuota(
					ctx,
					k8sClient,
					"non-matching-crq-"+suffix,
					&metav1.LabelSelector{
						MatchLabels: map[string]string{"team": "nonexistent"},
					},
					quotav1alpha1.ResourceList{
						corev1.ResourceRequestsCPU: resource.MustParse("1"),
					},
				)
				Expect(err).NotTo(HaveOccurred())
				DeferCleanup(func() {
					_ = k8sClient.Delete(ctx, nonMatchingCRQ)
				})

				Eventually(func() []string {
					return testutils.GetCRQStatusNamespaces(nonMatchingCRQ)
				}, Timeout, Interval).Should(BeEmpty())
			})
		})

		Context("Pod States", func() {
			It("should count resources from pods in error states (ImagePullBackOff)", func() {
				// Create a pod with a non-existent image to trigger ImagePullBackOff
				// This pod should count towards quota since it's not terminal
				pod := &corev1.Pod{
					ObjectMeta: metav1.ObjectMeta{
						Name:      "error-pod-" + suffix,
						Namespace: nsName,
					},
					Spec: corev1.PodSpec{
						Containers: []corev1.Container{
							{
								Name:  "error-container",
								Image: "non-existent-image:invalid-tag",
								Resources: corev1.ResourceRequirements{
									Requests: corev1.ResourceList{
										corev1.ResourceCPU:    resource.MustParse("250m"),
										corev1.ResourceMemory: resource.MustParse("256Mi"),
									},
									Limits: corev1.ResourceList{
										corev1.ResourceCPU:    resource.MustParse("500m"),
										corev1.ResourceMemory: resource.MustParse("512Mi"),
									},
								},
							},
						},
						RestartPolicy: corev1.RestartPolicyAlways,
					},
				}
				Expect(k8sClient.Create(ctx, pod)).To(Succeed())
				DeferCleanup(func() {
					_ = k8sClient.Delete(ctx, pod)
				})

				// Wait for pod to enter ImagePullBackOff state
				By("Waiting for pod to enter error state (ImagePullBackOff)")
				Eventually(func() bool {
					err := k8sClient.Get(ctx, client.ObjectKey{Name: pod.Name, Namespace: pod.Namespace}, pod)
					if err != nil {
						return false
					}
					// Check if pod is in ImagePullBackOff or similar error state
					if pod.Status.Phase == corev1.PodPending {
						for _, containerStatus := range pod.Status.ContainerStatuses {
							if containerStatus.State.Waiting != nil &&
								(containerStatus.State.Waiting.Reason == "ImagePullBackOff" ||
									containerStatus.State.Waiting.Reason == "ErrImagePull") {
								return true
							}
						}
					}
					return false
				}, time.Second*15, time.Second*2).Should(BeTrue(), "Pod should enter ImagePullBackOff state")

				// Error state pods should count towards quota usage (they're not terminal)
				Eventually(func() error {
					usage := testutils.GetRefreshedCRQStatusUsage(ctx, k8sClient, crq.Name)
					return testutils.ExpectCRQUsageToMatch(usage, map[string]string{
						"requests.cpu":    "250m",  // Pod in error state should count
						"requests.memory": "256Mi", // Pod in error state should count
						"limits.cpu":      "500m",  // Pod in error state should count
						"limits.memory":   "512Mi", // Pod in error state should count
					})
				}, Timeout, Interval).Should(Succeed(), "Pods in error states should count towards quota usage")
			})
			It("should not count CPU resources from failed jobs", func() {
				job, err := testutils.CreateJob(
					ctx,
					k8sClient,
					nsName,
					"failed-job-"+suffix,
					[]string{"sh", "-c", "sleep 1 && exit 1"},
					corev1.ResourceList{
						corev1.ResourceCPU: resource.MustParse("500m"),
					},
					nil)
				Expect(err).NotTo(HaveOccurred())
				DeferCleanup(func() {
					_ = k8sClient.Delete(ctx, job, client.PropagationPolicy(metav1.DeletePropagationBackground))
				})

				// Wait longer for job to fail and become terminal
				time.Sleep(5 * time.Second)

				// Terminal pods should not affect quota usage
				Eventually(func() error {
					usage := testutils.GetRefreshedCRQStatusUsage(ctx, k8sClient, crq.Name)
					return testutils.ExpectCRQUsageToMatch(usage, map[string]string{})
				}, Timeout, Interval).Should(Succeed())
			})

			It("should not count CPU resources from succeeded jobs", func() {
				// Create a CRQ that tracks CPU resources
				job, err := testutils.CreateJob(
					ctx,
					k8sClient,
					nsName,
					"succeeded-job-"+suffix,
					[]string{"sh", "-c", "sleep 1 && exit 0"},
					corev1.ResourceList{
						corev1.ResourceCPU: resource.MustParse("300m"),
					},
					nil)
				Expect(err).NotTo(HaveOccurred())
				DeferCleanup(func() {
					_ = k8sClient.Delete(ctx, job, client.PropagationPolicy(metav1.DeletePropagationBackground))
				})

				// Wait longer for job to succeed and become terminal
				time.Sleep(5 * time.Second)

				// Terminal pods should not affect quota usage
				Eventually(func() error {
					usage := testutils.GetRefreshedCRQStatusUsage(ctx, k8sClient, crq.Name)
					return testutils.ExpectCRQUsageToMatch(usage, map[string]string{})
				}, Timeout, Interval).Should(Succeed())
			})
		})

		Context("Namespace Dynamics", func() {
			It("should update status when namespace becomes excluded - comprehensive validation", func() {
				By("Creating a namespace that initially matches the CRQ selector")
				dynamicNS, err := testutils.CreateNamespace(
					ctx,
					k8sClient,
					"dynamic-ns-"+suffix,
					map[string]string{"team": "test-" + suffix},
				)
				Expect(err).NotTo(HaveOccurred())
				DeferCleanup(func() {
					_ = k8sClient.Delete(ctx, dynamicNS)
				})

				By("Creating multiple pods with different resource requirements in the namespace")
				// Pod 1: CPU and memory
				pod1, err := testutils.CreatePod(
					ctx,
					k8sClient,
					"dynamic-ns-"+suffix,
					"cpu-pod-"+suffix,
					corev1.ResourceList{
						corev1.ResourceCPU:    resource.MustParse("100m"),
						corev1.ResourceMemory: resource.MustParse("128Mi"),
					},
					corev1.ResourceList{
						corev1.ResourceCPU:    resource.MustParse("200m"),
						corev1.ResourceMemory: resource.MustParse("256Mi"),
					})
				Expect(err).NotTo(HaveOccurred())
				DeferCleanup(func() {
					_ = k8sClient.Delete(ctx, pod1)
				})

				// Pod 2: GPU resources
				pod2, err := testutils.CreatePod(
					ctx,
					k8sClient,
					"dynamic-ns-"+suffix,
					"gpu-pod-"+suffix,
					corev1.ResourceList{
						"example.com/gpu": resource.MustParse("1"),
					},
					corev1.ResourceList{
						"example.com/gpu": resource.MustParse("1"),
					})
				Expect(err).NotTo(HaveOccurred())
				DeferCleanup(func() {
					_ = k8sClient.Delete(ctx, pod2)
				})

				By("Verifying namespace is included in CRQ status")
				Eventually(func() []string {
					return testutils.GetRefreshedCRQStatusNamespaces(ctx, k8sClient, crq.Name)
				}, Timeout, Interval).Should(ContainElement("dynamic-ns-" + suffix))

				By("Verifying all pod resources are aggregated correctly in CRQ usage")
				Eventually(func() error {
					usage := testutils.GetRefreshedCRQStatusUsage(ctx, k8sClient, crq.Name)
					return testutils.ExpectCRQUsageToMatch(usage, map[string]string{
						"requests.cpu":    "100m",  // Pod 1 CPU
						"requests.memory": "128Mi", // Pod 1 Memory
						"limits.cpu":      "200m",  // Pod 1 CPU limits
						"limits.memory":   "256Mi", // Pod 1 Memory limits
						"example.com/gpu": "1",     // Pod 2 GPU
					})
				}, Timeout, Interval).Should(Succeed())

				By("Adding the exclusion label to the namespace")
				dynamicNS.Labels["pac-quota-controller.powerapp.cloud/exclude"] = "true"
				Expect(k8sClient.Update(ctx, dynamicNS)).To(Succeed())

				By("Verifying namespace is immediately excluded from CRQ status")
				Eventually(func() []string {
					return testutils.GetRefreshedCRQStatusNamespaces(ctx, k8sClient, crq.Name)
				}, Timeout, Interval).ShouldNot(ContainElement("dynamic-ns-" + suffix))

				By("Verifying all resource usage from excluded namespace is no longer counted")
				Eventually(func() error {
					usage := testutils.GetRefreshedCRQStatusUsage(ctx, k8sClient, crq.Name)
					return testutils.ExpectCRQUsageToMatch(usage, map[string]string{})
				}, Timeout, Interval).Should(Succeed())

				By("Verifying that pods still exist but are just not counted")
				// Pods should still exist in the excluded namespace
				Expect(k8sClient.Get(ctx, client.ObjectKey{Name: pod1.Name, Namespace: pod1.Namespace}, pod1)).To(Succeed())
				Expect(k8sClient.Get(ctx, client.ObjectKey{Name: pod2.Name, Namespace: pod2.Namespace}, pod2)).To(Succeed())
			})

			It("should handle label changes that affect namespace selection", func() {
				By("Creating a namespace that initially doesn't match")
				changingNS, err := testutils.CreateNamespace(
					ctx,
					k8sClient,
					"changing-ns-"+suffix,
					map[string]string{"environment": "staging"}, // Doesn't match "team": "test"
				)
				Expect(err).NotTo(HaveOccurred())
				DeferCleanup(func() {
					_ = k8sClient.Delete(ctx, changingNS)
				})

				By("Creating a pod in the non-matching namespace")
				pod, err := testutils.CreatePod(
					ctx,
					k8sClient,
					"changing-ns-"+suffix,
					"staging-pod-"+suffix,
					corev1.ResourceList{
						corev1.ResourceCPU:    resource.MustParse("250m"),
						corev1.ResourceMemory: resource.MustParse("512Mi"),
					},
					corev1.ResourceList{
						corev1.ResourceCPU:    resource.MustParse("500m"),
						corev1.ResourceMemory: resource.MustParse("1Gi"),
					})
				Expect(err).NotTo(HaveOccurred())
				DeferCleanup(func() {
					_ = k8sClient.Delete(ctx, pod)
				})

				By("Verifying namespace is initially not included")
				Eventually(func() []string {
					return testutils.GetCRQStatusNamespaces(crq)
				}, Timeout, Interval).ShouldNot(ContainElement("changing-ns-" + suffix))

				By("Changing labels to match the CRQ selector")
				changingNS.Labels["team"] = "test-" + suffix // Now matches selector
				Expect(k8sClient.Update(ctx, changingNS)).To(Succeed())

				By("Verifying namespace is now included and usage is counted")
				Eventually(func() []string {
					return testutils.GetRefreshedCRQStatusNamespaces(ctx, k8sClient, crq.Name)
				}, Timeout, Interval).Should(ContainElement("changing-ns-" + suffix))

				Eventually(func() error {
					usage := testutils.GetRefreshedCRQStatusUsage(ctx, k8sClient, crq.Name)
					return testutils.ExpectCRQUsageToMatch(usage, map[string]string{
						"requests.cpu":    "250m",  // Pod CPU request
						"requests.memory": "512Mi", // Pod Memory request
						"limits.cpu":      "500m",  // Pod CPU limit
						"limits.memory":   "1Gi",   // Pod Memory limit
					})
				}, Timeout, Interval).Should(Succeed())

				By("Changing labels back to not match")
				delete(changingNS.Labels, "team")
				changingNS.Labels["environment"] = "production" // Different label
				Expect(k8sClient.Update(ctx, changingNS)).To(Succeed())

				By("Verifying namespace is excluded again and usage is removed")
				Eventually(func() []string {
					return testutils.GetRefreshedCRQStatusNamespaces(ctx, k8sClient, crq.Name)
				}, Timeout, Interval).ShouldNot(ContainElement("changing-ns-" + suffix))

				Eventually(func() error {
					usage := testutils.GetRefreshedCRQStatusUsage(ctx, k8sClient, crq.Name)
					return testutils.ExpectCRQUsageToMatch(usage, map[string]string{})
				}, Timeout, Interval).Should(Succeed())
			})
		})

		Context("Mixed Pod and Job Scenarios", func() {
			It("should only count active pods when namespace has both pods and terminal jobs", func() {
				By("Creating multiple active pods with different resource patterns")
				// Active Pod 1
				pod1, err := testutils.CreatePod(
					ctx,
					k8sClient,
					nsName,
					"pod1-"+suffix,
					corev1.ResourceList{
						corev1.ResourceCPU:    resource.MustParse("100m"),
						corev1.ResourceMemory: resource.MustParse("128Mi"),
					},
					corev1.ResourceList{
						corev1.ResourceCPU:    resource.MustParse("200m"),
						corev1.ResourceMemory: resource.MustParse("256Mi"),
					})
				Expect(err).NotTo(HaveOccurred())
				DeferCleanup(func() {
					_ = k8sClient.Delete(ctx, pod1)
				})

				// Active Pod 2: Database pattern with higher resource requirements
				pod2, err := testutils.CreatePod(
					ctx,
					k8sClient,
					nsName,
					"pod2-"+suffix,
					corev1.ResourceList{
						corev1.ResourceCPU:    resource.MustParse("500m"),
						corev1.ResourceMemory: resource.MustParse("1Gi"),
					},
					corev1.ResourceList{
						corev1.ResourceCPU:    resource.MustParse("1"),
						corev1.ResourceMemory: resource.MustParse("2Gi"),
					})
				Expect(err).NotTo(HaveOccurred())
				DeferCleanup(func() {
					_ = k8sClient.Delete(ctx, pod2)
				})

				By("Creating jobs that will become terminal (both failed and succeeded)")
				// Failed Job: Data migration that fails
				failedJob, err := testutils.CreateJob(
					ctx,
					k8sClient,
					nsName,
					"migration-job-"+suffix,
					[]string{"sh", "-c", "sleep 1 && exit 1"},
					corev1.ResourceList{
						corev1.ResourceCPU:    resource.MustParse("200m"),
						corev1.ResourceMemory: resource.MustParse("256Mi"),
					},
					corev1.ResourceList{
						corev1.ResourceCPU:    resource.MustParse("400m"),
						corev1.ResourceMemory: resource.MustParse("512Mi"),
					})
				Expect(err).NotTo(HaveOccurred())
				DeferCleanup(func() {
					_ = k8sClient.Delete(ctx, failedJob, client.PropagationPolicy(metav1.DeletePropagationBackground))
				})

				// Succeeded Job: Backup job that completes successfully
				succeededJob, err := testutils.CreateJob(
					ctx,
					k8sClient,
					nsName,
					"backup-job-"+suffix,
					[]string{"sh", "-c", "sleep 1 && exit 0"},
					corev1.ResourceList{
						corev1.ResourceCPU:    resource.MustParse("300m"),
						corev1.ResourceMemory: resource.MustParse("384Mi"),
					},
					corev1.ResourceList{
						corev1.ResourceCPU:    resource.MustParse("600m"),
						corev1.ResourceMemory: resource.MustParse("768Mi"),
					})
				Expect(err).NotTo(HaveOccurred())
				DeferCleanup(func() {
					_ = k8sClient.Delete(ctx, succeededJob, client.PropagationPolicy(metav1.DeletePropagationBackground))
				})

				By("Waiting for jobs to reach terminal state")
				// Give more time for jobs to fail/succeed and become terminal
				time.Sleep(8 * time.Second)

				By("Verifying only active pods are counted in resource usage")
				// Expected: webPod (100m CPU, 128Mi memory) + dbPod (500m CPU, 1Gi memory) = 600m CPU, 1152Mi memory
				Eventually(func() error {
					usage := testutils.GetRefreshedCRQStatusUsage(ctx, k8sClient, crq.Name)
					return testutils.ExpectCRQUsageToMatch(
						usage,
						map[string]string{
							"requests.cpu":    "600m",   // webPod + dbPod only
							"requests.memory": "1152Mi", // webPod + dbPod only
							"limits.cpu":      "1200m",  // webPod + dbPod limits
							"limits.memory":   "2304Mi", // webPod + dbPod limits
						},
					)
				}, Timeout, Interval).Should(
					Succeed(),
					"Terminal jobs should not contribute to resource usage, "+
						"only active pods should be counted",
				)

				By("Verifying namespace appears in CRQ status with correct resource breakdown")
				Eventually(func() []string {
					return testutils.GetRefreshedCRQStatusNamespaces(ctx, k8sClient, crq.Name)
				}, Timeout, Interval).Should(ContainElement("test-ns-" + suffix))

				By("Creating an additional pod to verify incremental counting")
				pod3, err := testutils.CreatePod(
					ctx,
					k8sClient,
					nsName,
					"pod4-"+suffix,
					corev1.ResourceList{
						corev1.ResourceCPU:    resource.MustParse("150m"),
						corev1.ResourceMemory: resource.MustParse("192Mi"),
					},
					corev1.ResourceList{
						corev1.ResourceCPU:    resource.MustParse("300m"),
						corev1.ResourceMemory: resource.MustParse("384Mi"),
					})
				Expect(err).NotTo(HaveOccurred())
				DeferCleanup(func() {
					_ = k8sClient.Delete(ctx, pod3)
				})

				By("Verifying resource usage increases correctly with new active pod")
				// Expected: pod1 + pod2 + pod3 = 100m + 500m + 150m = 750m CPU, 128Mi + 1024Mi + 192Mi = 1344Mi memory
				Eventually(func() error {
					usage := testutils.GetRefreshedCRQStatusUsage(
						ctx,
						k8sClient,
						crq.Name,
					)
					return testutils.ExpectCRQUsageToMatch(usage, map[string]string{
						"requests.cpu":    "750m",   // All three pods
						"requests.memory": "1344Mi", // All three pods
						"limits.cpu":      "1500m",  // All three pods limits
						"limits.memory":   "2688Mi", // All three pods limits
					})
				}, Timeout, Interval).Should(
					Succeed(),
					"Adding new active pods should increase resource usage "+
						"while terminal jobs remain uncounted",
				)
			})

			It("should handle pod lifecycle transitions correctly", func() {
				By("Creating a long-running pod that we can monitor through lifecycle")
				pod4, err := testutils.CreatePod(
					ctx,
					k8sClient,
					nsName,
					"pod4-"+suffix,
					corev1.ResourceList{
						corev1.ResourceCPU:    resource.MustParse("250m"),
						corev1.ResourceMemory: resource.MustParse("256Mi"),
					},
					corev1.ResourceList{
						corev1.ResourceCPU:    resource.MustParse("500m"),
						corev1.ResourceMemory: resource.MustParse("512Mi"),
					})
				Expect(err).NotTo(HaveOccurred())

				By("Verifying pod resources are counted when active")
				Eventually(func() error {
					usage := testutils.GetRefreshedCRQStatusUsage(ctx, k8sClient, crq.Name)
					return testutils.ExpectCRQUsageToMatch(usage, map[string]string{
						"requests.cpu":    "250m",  // Pod CPU request
						"requests.memory": "256Mi", // Pod Memory request
						"limits.cpu":      "500m",  // Pod CPU limit
						"limits.memory":   "512Mi", // Pod Memory limit
					})
				}, Timeout, Interval).Should(Succeed())

				By("Simulating pod deletion and verifying resource usage is removed")
				Expect(k8sClient.Delete(ctx, pod4)).To(Succeed())

				Eventually(func() error {
					usage := testutils.GetRefreshedCRQStatusUsage(ctx, k8sClient, crq.Name)
					return testutils.ExpectCRQUsageToMatch(usage, map[string]string{})
				}, Timeout, Interval).Should(Succeed(), "Deleted pods should not contribute to resource usage")
			})
		})

		Context("Long-running Job Scenarios", func() {
			It("should track usage during job execution and remove after completion - detailed monitoring", func() {
				By("Creating baseline pods to establish initial resource usage")
				baselinePod, err := testutils.CreatePod(
					ctx,
					k8sClient,
					nsName,
					"baseline-pod-"+suffix,
					corev1.ResourceList{
						corev1.ResourceCPU:    resource.MustParse("50m"),
						corev1.ResourceMemory: resource.MustParse("64Mi"),
					},
					corev1.ResourceList{
						corev1.ResourceCPU:    resource.MustParse("100m"),
						corev1.ResourceMemory: resource.MustParse("128Mi"),
					})
				Expect(err).NotTo(HaveOccurred())
				DeferCleanup(func() {
					_ = k8sClient.Delete(ctx, baselinePod)
				})

				By("Verifying baseline resource usage")
				Eventually(func() error {
					usage := testutils.GetRefreshedCRQStatusUsage(ctx, k8sClient, crq.Name)
					return testutils.ExpectCRQUsageToMatch(usage, map[string]string{
						"requests.cpu":    "50m",   // Baseline pod CPU request
						"requests.memory": "64Mi",  // Baseline pod Memory request
						"limits.cpu":      "100m",  // Baseline pod CPU limit
						"limits.memory":   "128Mi", // Baseline pod Memory limit
					})
				}, Timeout, Interval).Should(Succeed())

				By("Creating a job that runs for 8 seconds to allow monitoring")
				longJob, err := testutils.CreateJob(
					ctx,
					k8sClient,
					nsName,
					"long-processing-job-"+suffix,
					[]string{"sh", "-c", "sleep 8 && exit 0"},
					corev1.ResourceList{
						corev1.ResourceCPU:    resource.MustParse("300m"),
						corev1.ResourceMemory: resource.MustParse("512Mi"),
					},
					corev1.ResourceList{
						corev1.ResourceCPU:    resource.MustParse("600m"),
						corev1.ResourceMemory: resource.MustParse("1Gi"),
					})
				Expect(err).NotTo(HaveOccurred())
				DeferCleanup(func() {
					_ = k8sClient.Delete(ctx, longJob, client.PropagationPolicy(metav1.DeletePropagationBackground))
				})

				By("Monitoring resource usage increases during job execution")
				// Expected: baseline (50m CPU, 64Mi memory) + job (300m CPU, 512Mi memory) = 350m CPU, 576Mi memory
				Eventually(func() error {
					usage := testutils.GetRefreshedCRQStatusUsage(ctx, k8sClient, crq.Name)
					if cpu, ok := usage[corev1.ResourceRequestsCPU]; ok {
						if mem, ok := usage[corev1.ResourceRequestsMemory]; ok {
							fmt.Printf("Current usage during job execution: CPU=%s, Memory=%s\n",
								cpu.String(), mem.String())
						}
					}
					return testutils.ExpectCRQUsageToMatch(usage, map[string]string{
						"requests.cpu":    "350m",   // Baseline + job
						"requests.memory": "576Mi",  // Baseline + job
						"limits.cpu":      "700m",   // Baseline + job limits
						"limits.memory":   "1152Mi", // Baseline + job limits
					})
				}, Timeout, Interval).Should(Succeed(), "Job resources should be counted while job is running")

				By("Waiting for job completion and monitoring resource usage decrease")
				// Wait for job to complete by polling the Job status and printing pod phase for debugging
				Eventually(func() bool {
					jobObj := &corev1.PodList{}
					err := k8sClient.List(ctx, jobObj, client.InNamespace(nsName))
					if err != nil {
						fmt.Printf("Error listing pods: %v\n", err)
						return false
					}
					found := false
					for _, pod := range jobObj.Items {
						if pod.Labels["job-name"] == "long-processing-job-"+suffix {
							fmt.Printf("Pod %s phase: %s\n", pod.Name, pod.Status.Phase)
							if pod.Status.Phase == corev1.PodSucceeded || pod.Status.Phase == corev1.PodFailed {
								found = true
							}
						}
					}
					return found
				}, time.Second*15, time.Second*2).Should(BeTrue(), "Job pod should reach terminal phase (Succeeded or Failed)")
				Eventually(func() error {
					usage := testutils.GetRefreshedCRQStatusUsage(ctx, k8sClient, crq.Name)
					if cpu, ok := usage[corev1.ResourceRequestsCPU]; ok {
						if mem, ok := usage[corev1.ResourceRequestsMemory]; ok {
							fmt.Printf(
								"Current usage after job completion: CPU=%s, Memory=%s\n",
								cpu.String(),
								mem.String(),
							)
						}
					}
					return testutils.ExpectCRQUsageToMatch(usage, map[string]string{
						"requests.cpu":    "50m",   // Back to baseline
						"requests.memory": "64Mi",  // Back to baseline
						"limits.cpu":      "100m",  // Back to baseline
						"limits.memory":   "128Mi", // Back to baseline
					})
				}, time.Second*15, time.Second*2).Should(
					Succeed(),
					"After job completion, only baseline pod resources should be counted",
				)
			})

			It("should handle multiple concurrent jobs with different durations", func() {
				By("Creating a short job (2 seconds)")
				shortJob, err := testutils.CreateJob(
					ctx,
					k8sClient,
					nsName, "short-job-"+suffix,
					[]string{"sh", "-c", "sleep 2 && exit 0"},
					corev1.ResourceList{
						corev1.ResourceCPU:    resource.MustParse("100m"),
						corev1.ResourceMemory: resource.MustParse("128Mi"),
					},
					corev1.ResourceList{
						corev1.ResourceCPU:    resource.MustParse("200m"),
						corev1.ResourceMemory: resource.MustParse("256Mi"),
					})
				Expect(err).NotTo(HaveOccurred())
				DeferCleanup(func() {
					_ = k8sClient.Delete(ctx, shortJob, client.PropagationPolicy(metav1.DeletePropagationBackground))
				})

				By("Creating a medium job (6 seconds)")
				mediumJob, err := testutils.CreateJob(
					ctx,
					k8sClient,
					nsName,
					"medium-job-"+suffix,
					[]string{"sh", "-c", "sleep 6 && exit 0"},
					corev1.ResourceList{
						corev1.ResourceCPU:    resource.MustParse("200m"),
						corev1.ResourceMemory: resource.MustParse("256Mi"),
					},
					corev1.ResourceList{
						corev1.ResourceCPU:    resource.MustParse("400m"),
						corev1.ResourceMemory: resource.MustParse("512Mi"),
					})
				Expect(err).NotTo(HaveOccurred())
				DeferCleanup(func() {
					_ = k8sClient.Delete(ctx, mediumJob, client.PropagationPolicy(metav1.DeletePropagationBackground))
				})

				By("Verifying both jobs are initially counted")
				// Expected: shortJob (100m, 128Mi) + mediumJob (200m, 256Mi) = 300m CPU, 384Mi memory
				Eventually(func() error {
					usage := testutils.GetRefreshedCRQStatusUsage(ctx, k8sClient, crq.Name)
					return testutils.ExpectCRQUsageToMatch(usage, map[string]string{
						"requests.cpu":    "300m",  // Both jobs
						"requests.memory": "384Mi", // Both jobs
						"limits.cpu":      "600m",  // Both jobs limits
						"limits.memory":   "768Mi", // Both jobs limits
					})
				}, Timeout, Interval).Should(Succeed())

				By("Waiting for short job to complete, medium job still running")
				time.Sleep(2 * time.Second) // Short job should be done, medium job still running

				Eventually(func() error {
					usage := testutils.GetRefreshedCRQStatusUsage(ctx, k8sClient, crq.Name)
					return testutils.ExpectCRQUsageToMatch(usage, map[string]string{
						"requests.cpu":    "200m",  // Only medium job
						"requests.memory": "256Mi", // Only medium job
						"limits.cpu":      "400m",  // Only medium job
						"limits.memory":   "512Mi", // Only medium job
					})
				}, Timeout, Interval).Should(Succeed(), "After short job completion, only medium job should be counted")

				By("Waiting for medium job to complete")
				Eventually(func() error {
					usage := testutils.GetRefreshedCRQStatusUsage(ctx, k8sClient, crq.Name)
					return testutils.ExpectCRQUsageToMatch(usage, map[string]string{})
				}, time.Second*10, Interval).Should(Succeed(), "After all jobs complete, no job resources should be counted")
			})
		})
	})
})
