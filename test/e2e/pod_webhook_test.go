package e2e

import (
	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	quotav1alpha1 "github.com/powerhome/pac-quota-controller/api/v1alpha1"
	batchv1 "k8s.io/api/batch/v1"
	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/resource"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"sigs.k8s.io/controller-runtime/pkg/client"
)

var _ = Describe("Pod Admission Webhook Tests", func() {
	var (
		testNamespace string
		testCRQName   string
		testSuffix    string
	)

	BeforeEach(func() {
		testSuffix = generateTestSuffix()
		testNamespace = generateTestName("pod-webhook-ns")
		testCRQName = generateTestName("pod-webhook-crq")
	})

	AfterEach(func() {
		// Clean up resources
		ensureNamespaceDeleted(testNamespace)
		ensureCRQDeleted(testCRQName)
	})

	Context("Pod creation validation", func() {
		It("should deny pod creation when it would exceed CPU limits", func() {
			// Create namespace with specific label
			labelKey := "pod-webhook-test-" + testSuffix
			labelValue := "validate-cpu-" + testSuffix

			ns := &corev1.Namespace{
				ObjectMeta: metav1.ObjectMeta{
					Name: testNamespace,
					Labels: map[string]string{
						labelKey: labelValue,
					},
				},
			}
			Expect(k8sClient.Create(ctx, ns)).To(Succeed())

			// Create CRQ with CPU limit
			crq := &quotav1alpha1.ClusterResourceQuota{
				ObjectMeta: metav1.ObjectMeta{
					Name: testCRQName,
				},
				Spec: quotav1alpha1.ClusterResourceQuotaSpec{
					NamespaceSelector: &metav1.LabelSelector{
						MatchLabels: map[string]string{
							labelKey: labelValue,
						},
					},
					Hard: quotav1alpha1.ResourceList{
						corev1.ResourceRequestsCPU: resource.MustParse("100m"),
					},
				},
			}
			Expect(k8sClient.Create(ctx, crq)).To(Succeed())

			// Wait for the CRQ to be processed
			Eventually(func() bool {
				err := k8sClient.Get(ctx, client.ObjectKey{Name: testCRQName}, crq)
				return err == nil && crq.Status.Total.Hard != nil
			}, "10s", "250ms").Should(BeTrue())

			// Try to create a pod that exceeds the limit
			pod := &corev1.Pod{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "test-pod-" + testSuffix,
					Namespace: testNamespace,
				},
				Spec: corev1.PodSpec{
					Containers: []corev1.Container{
						{
							Name:  "test-container",
							Image: "nginx:latest",
							Resources: corev1.ResourceRequirements{
								Requests: corev1.ResourceList{
									corev1.ResourceCPU: resource.MustParse("200m"), // Exceeds 100m limit
								},
							},
						},
					},
				},
			}

			// The webhook should deny this pod creation
			Expect(k8sClient.Create(ctx, pod)).ToNot(Succeed())
		})

		It("should allow pod creation when within limits", func() {
			// Create namespace with specific label
			labelKey := "pod-webhook-test-" + testSuffix
			labelValue := "allow-cpu-" + testSuffix

			ns := &corev1.Namespace{
				ObjectMeta: metav1.ObjectMeta{
					Name: testNamespace,
					Labels: map[string]string{
						labelKey: labelValue,
					},
				},
			}
			Expect(k8sClient.Create(ctx, ns)).To(Succeed())

			// Create CRQ with CPU limit
			crq := &quotav1alpha1.ClusterResourceQuota{
				ObjectMeta: metav1.ObjectMeta{
					Name: testCRQName,
				},
				Spec: quotav1alpha1.ClusterResourceQuotaSpec{
					NamespaceSelector: &metav1.LabelSelector{
						MatchLabels: map[string]string{
							labelKey: labelValue,
						},
					},
					Hard: quotav1alpha1.ResourceList{
						corev1.ResourceRequestsCPU: resource.MustParse("100m"),
					},
				},
			}
			Expect(k8sClient.Create(ctx, crq)).To(Succeed())

			// Wait for the CRQ to be processed
			Eventually(func() bool {
				err := k8sClient.Get(ctx, client.ObjectKey{Name: testCRQName}, crq)
				return err == nil && crq.Status.Total.Hard != nil
			}, "10s", "250ms").Should(BeTrue())

			// Create a pod that is within the limit
			pod := &corev1.Pod{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "test-pod-" + testSuffix,
					Namespace: testNamespace,
				},
				Spec: corev1.PodSpec{
					Containers: []corev1.Container{
						{
							Name:  "test-container",
							Image: "nginx:latest",
							Resources: corev1.ResourceRequirements{
								Requests: corev1.ResourceList{
									corev1.ResourceCPU: resource.MustParse("50m"), // Within 100m limit
								},
							},
						},
					},
				},
			}

			// The webhook should allow this pod creation
			Expect(k8sClient.Create(ctx, pod)).To(Succeed())

			// Clean up the pod
			Expect(k8sClient.Delete(ctx, pod)).To(Succeed())
		})

		It("should allow pod creation in namespaces not selected by any CRQ", func() {
			// Create namespace without any special labels
			ns := &corev1.Namespace{
				ObjectMeta: metav1.ObjectMeta{
					Name: testNamespace,
				},
			}
			Expect(k8sClient.Create(ctx, ns)).To(Succeed())

			// Create a pod with high resource usage
			pod := &corev1.Pod{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "test-pod-" + testSuffix,
					Namespace: testNamespace,
				},
				Spec: corev1.PodSpec{
					Containers: []corev1.Container{
						{
							Name:  "test-container",
							Image: "nginx:latest",
							Resources: corev1.ResourceRequirements{
								Requests: corev1.ResourceList{
									corev1.ResourceCPU:    resource.MustParse("1000m"),
									corev1.ResourceMemory: resource.MustParse("1Gi"),
								},
							},
						},
					},
				},
			}

			// The webhook should allow this pod creation since no CRQ applies
			Expect(k8sClient.Create(ctx, pod)).To(Succeed())

			// Clean up the pod
			Expect(k8sClient.Delete(ctx, pod)).To(Succeed())
		})
	})

	Context("Pod update validation", func() {
		It("should deny pod updates that would exceed limits", func() {
			// Create namespace with specific label
			labelKey := "pod-webhook-test-" + testSuffix
			labelValue := "update-test-" + testSuffix

			ns := &corev1.Namespace{
				ObjectMeta: metav1.ObjectMeta{
					Name: testNamespace,
					Labels: map[string]string{
						labelKey: labelValue,
					},
				},
			}
			Expect(k8sClient.Create(ctx, ns)).To(Succeed())

			// Create CRQ with CPU limit
			crq := &quotav1alpha1.ClusterResourceQuota{
				ObjectMeta: metav1.ObjectMeta{
					Name: testCRQName,
				},
				Spec: quotav1alpha1.ClusterResourceQuotaSpec{
					NamespaceSelector: &metav1.LabelSelector{
						MatchLabels: map[string]string{
							labelKey: labelValue,
						},
					},
					Hard: quotav1alpha1.ResourceList{
						corev1.ResourceRequestsCPU: resource.MustParse("100m"),
					},
				},
			}
			Expect(k8sClient.Create(ctx, crq)).To(Succeed())

			// Wait for the CRQ to be processed
			Eventually(func() bool {
				err := k8sClient.Get(ctx, client.ObjectKey{Name: testCRQName}, crq)
				return err == nil && crq.Status.Total.Hard != nil
			}, "10s", "250ms").Should(BeTrue())

			// Create a pod with minimal resources
			pod := &corev1.Pod{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "test-pod-" + testSuffix,
					Namespace: testNamespace,
				},
				Spec: corev1.PodSpec{
					Containers: []corev1.Container{
						{
							Name:  "test-container",
							Image: "nginx:latest",
							Resources: corev1.ResourceRequirements{
								Requests: corev1.ResourceList{
									corev1.ResourceCPU: resource.MustParse("10m"),
								},
							},
						},
					},
				},
			}

			// Create the pod
			Expect(k8sClient.Create(ctx, pod)).To(Succeed())

			// Wait for pod to be created and get the latest version
			Eventually(func() bool {
				err := k8sClient.Get(ctx, client.ObjectKey{Name: pod.Name, Namespace: pod.Namespace}, pod)
				return err == nil
			}, "10s", "250ms").Should(BeTrue())

			// Get the latest version of the pod before updating
			Expect(k8sClient.Get(ctx, client.ObjectKey{Name: pod.Name, Namespace: pod.Namespace}, pod)).To(Succeed())

			// Try to update the pod to exceed limits
			pod.Spec.Containers[0].Resources.Requests[corev1.ResourceCPU] = resource.MustParse("200m") // Exceeds limit

			// The webhook should deny this update
			Expect(k8sClient.Update(ctx, pod)).ToNot(Succeed())

			// Clean up the pod
			Expect(k8sClient.Delete(ctx, pod)).To(Succeed())
		})
	})

	Context("Resource configuration validation", func() {
		var (
			ns  *corev1.Namespace
			crq *quotav1alpha1.ClusterResourceQuota
		)

		BeforeEach(func() {
			// Create namespace with specific label
			labelKey := "resource-config-test-" + testSuffix
			labelValue := "config-test-" + testSuffix

			ns = &corev1.Namespace{
				ObjectMeta: metav1.ObjectMeta{
					Name: testNamespace,
					Labels: map[string]string{
						labelKey: labelValue,
					},
				},
			}
			Expect(k8sClient.Create(ctx, ns)).To(Succeed())

			// Create CRQ with both CPU and memory limits
			crq = &quotav1alpha1.ClusterResourceQuota{
				ObjectMeta: metav1.ObjectMeta{
					Name: testCRQName,
				},
				Spec: quotav1alpha1.ClusterResourceQuotaSpec{
					NamespaceSelector: &metav1.LabelSelector{
						MatchLabels: map[string]string{
							labelKey: labelValue,
						},
					},
					Hard: quotav1alpha1.ResourceList{
						corev1.ResourceRequestsCPU:    resource.MustParse("500m"),
						corev1.ResourceRequestsMemory: resource.MustParse("1Gi"),
						corev1.ResourceLimitsCPU:      resource.MustParse("1000m"),
						corev1.ResourceLimitsMemory:   resource.MustParse("2Gi"),
					},
				},
			}
			Expect(k8sClient.Create(ctx, crq)).To(Succeed())

			// Wait for the CRQ to be processed
			Eventually(func() bool {
				err := k8sClient.Get(ctx, client.ObjectKey{Name: testCRQName}, crq)
				return err == nil && crq.Status.Total.Hard != nil
			}, "10s", "250ms").Should(BeTrue())
		})

		It("should allow pods with limits but no requests", func() {
			pod := &corev1.Pod{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "limits-only-pod-" + testSuffix,
					Namespace: testNamespace,
				},
				Spec: corev1.PodSpec{
					Containers: []corev1.Container{
						{
							Name:  "test-container",
							Image: "nginx:latest",
							Resources: corev1.ResourceRequirements{
								// Only limits, no requests
								Limits: corev1.ResourceList{
									corev1.ResourceCPU:    resource.MustParse("200m"),
									corev1.ResourceMemory: resource.MustParse("512Mi"),
								},
							},
						},
					},
				},
			}

			// Should be allowed since it doesn't count against requests quota
			Expect(k8sClient.Create(ctx, pod)).To(Succeed())
			Expect(k8sClient.Delete(ctx, pod)).To(Succeed())
		})

		It("should deny pods with limits-only that exceed limits quota", func() {
			pod := &corev1.Pod{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "limits-exceed-pod-" + testSuffix,
					Namespace: testNamespace,
				},
				Spec: corev1.PodSpec{
					Containers: []corev1.Container{
						{
							Name:  "test-container",
							Image: "nginx:latest",
							Resources: corev1.ResourceRequirements{
								// Only limits that exceed quota
								Limits: corev1.ResourceList{
									corev1.ResourceCPU:    resource.MustParse("1500m"), // Exceeds 1000m limit
									corev1.ResourceMemory: resource.MustParse("3Gi"),   // Exceeds 2Gi limit
								},
							},
						},
					},
				},
			}

			// Should be denied due to limits quota violation
			Expect(k8sClient.Create(ctx, pod)).ToNot(Succeed())
		})

		It("should allow pods with requests but no limits", func() {
			pod := &corev1.Pod{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "requests-only-pod-" + testSuffix,
					Namespace: testNamespace,
				},
				Spec: corev1.PodSpec{
					Containers: []corev1.Container{
						{
							Name:  "test-container",
							Image: "nginx:latest",
							Resources: corev1.ResourceRequirements{
								// Only requests, no limits
								Requests: corev1.ResourceList{
									corev1.ResourceCPU:    resource.MustParse("100m"),
									corev1.ResourceMemory: resource.MustParse("256Mi"),
								},
							},
						},
					},
				},
			}

			// Should be allowed since it doesn't count against limits quota
			Expect(k8sClient.Create(ctx, pod)).To(Succeed())
			Expect(k8sClient.Delete(ctx, pod)).To(Succeed())
		})

		It("should allow pods with mixed resource configurations", func() {
			pod := &corev1.Pod{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "mixed-config-pod-" + testSuffix,
					Namespace: testNamespace,
				},
				Spec: corev1.PodSpec{
					Containers: []corev1.Container{
						{
							Name:  "test-container",
							Image: "nginx:latest",
							Resources: corev1.ResourceRequirements{
								Requests: corev1.ResourceList{
									corev1.ResourceCPU: resource.MustParse("100m"),
									// Memory request not specified
								},
								Limits: corev1.ResourceList{
									corev1.ResourceMemory: resource.MustParse("512Mi"),
									// CPU limit not specified
								},
							},
						},
					},
				},
			}

			// Should be allowed since each resource type is within its respective quota
			Expect(k8sClient.Create(ctx, pod)).To(Succeed())
			Expect(k8sClient.Delete(ctx, pod)).To(Succeed())
		})

		It("should allow pods with no tracked resources", func() {
			pod := &corev1.Pod{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "no-resources-pod-" + testSuffix,
					Namespace: testNamespace,
				},
				Spec: corev1.PodSpec{
					Containers: []corev1.Container{
						{
							Name:  "test-container",
							Image: "nginx:latest",
							// No resource specifications
						},
					},
				},
			}

			// Should be allowed since it doesn't consume any tracked resources
			Expect(k8sClient.Create(ctx, pod)).To(Succeed())
			Expect(k8sClient.Delete(ctx, pod)).To(Succeed())
		})

		It("should handle pods with multiple containers correctly", func() {
			pod := &corev1.Pod{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "multi-container-pod-" + testSuffix,
					Namespace: testNamespace,
				},
				Spec: corev1.PodSpec{
					Containers: []corev1.Container{
						{
							Name:  "main-container",
							Image: "nginx:latest",
							Resources: corev1.ResourceRequirements{
								Requests: corev1.ResourceList{
									corev1.ResourceCPU:    resource.MustParse("100m"),
									corev1.ResourceMemory: resource.MustParse("256Mi"),
								},
							},
						},
						{
							Name:  "sidecar-container",
							Image: "alpine:latest",
							Resources: corev1.ResourceRequirements{
								Requests: corev1.ResourceList{
									corev1.ResourceCPU:    resource.MustParse("50m"),
									corev1.ResourceMemory: resource.MustParse("128Mi"),
								},
							},
						},
					},
				},
			}

			// Should be allowed as total resources (150m CPU, 384Mi memory) are within limits
			Expect(k8sClient.Create(ctx, pod)).To(Succeed())
			Expect(k8sClient.Delete(ctx, pod)).To(Succeed())
		})

		It("should handle pods with init containers correctly", func() {
			pod := &corev1.Pod{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "init-container-pod-" + testSuffix,
					Namespace: testNamespace,
				},
				Spec: corev1.PodSpec{
					InitContainers: []corev1.Container{
						{
							Name:  "init-container",
							Image: "busybox:latest",
							Resources: corev1.ResourceRequirements{
								Requests: corev1.ResourceList{
									corev1.ResourceCPU:    resource.MustParse("50m"),
									corev1.ResourceMemory: resource.MustParse("128Mi"),
								},
							},
						},
					},
					Containers: []corev1.Container{
						{
							Name:  "main-container",
							Image: "nginx:latest",
							Resources: corev1.ResourceRequirements{
								Requests: corev1.ResourceList{
									corev1.ResourceCPU:    resource.MustParse("100m"),
									corev1.ResourceMemory: resource.MustParse("256Mi"),
								},
							},
						},
					},
				},
			}

			// Should be allowed as total resources include both init and regular containers
			Expect(k8sClient.Create(ctx, pod)).To(Succeed())
			Expect(k8sClient.Delete(ctx, pod)).To(Succeed())
		})
	})

	Context("Resource update scenarios", func() {
		var (
			ns  *corev1.Namespace
			crq *quotav1alpha1.ClusterResourceQuota
		)

		BeforeEach(func() {
			// Create namespace with specific label
			labelKey := "resource-update-test-" + testSuffix
			labelValue := "update-test-" + testSuffix

			ns = &corev1.Namespace{
				ObjectMeta: metav1.ObjectMeta{
					Name: testNamespace,
					Labels: map[string]string{
						labelKey: labelValue,
					},
				},
			}
			Expect(k8sClient.Create(ctx, ns)).To(Succeed())

			// Create CRQ with limits for both requests and limits
			crq = &quotav1alpha1.ClusterResourceQuota{
				ObjectMeta: metav1.ObjectMeta{
					Name: testCRQName,
				},
				Spec: quotav1alpha1.ClusterResourceQuotaSpec{
					NamespaceSelector: &metav1.LabelSelector{
						MatchLabels: map[string]string{
							labelKey: labelValue,
						},
					},
					Hard: quotav1alpha1.ResourceList{
						corev1.ResourceRequestsCPU:    resource.MustParse("500m"),
						corev1.ResourceLimitsCPU:      resource.MustParse("1000m"),
						corev1.ResourceRequestsMemory: resource.MustParse("1Gi"),
						corev1.ResourceLimitsMemory:   resource.MustParse("2Gi"),
					},
				},
			}
			Expect(k8sClient.Create(ctx, crq)).To(Succeed())

			// Wait for the CRQ to be processed
			Eventually(func() bool {
				err := k8sClient.Get(ctx, client.ObjectKey{Name: testCRQName}, crq)
				return err == nil && crq.Status.Total.Hard != nil
			}, "10s", "250ms").Should(BeTrue())
		})

		It("should deny updates that add limits exceeding quota", func() {
			// Create pod with requests only
			pod := &corev1.Pod{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "exceed-limits-pod-" + testSuffix,
					Namespace: testNamespace,
				},
				Spec: corev1.PodSpec{
					Containers: []corev1.Container{
						{
							Name:  "test-container",
							Image: "nginx:latest",
							Resources: corev1.ResourceRequirements{
								Requests: corev1.ResourceList{
									corev1.ResourceCPU:    resource.MustParse("100m"),
									corev1.ResourceMemory: resource.MustParse("256Mi"),
								},
							},
						},
					},
				},
			}

			Expect(k8sClient.Create(ctx, pod)).To(Succeed())

			// Wait for pod to be created
			Eventually(func() bool {
				err := k8sClient.Get(ctx, client.ObjectKey{Name: pod.Name, Namespace: pod.Namespace}, pod)
				return err == nil
			}, "10s", "250ms").Should(BeTrue())

			// Try to add limits that exceed quota
			pod.Spec.Containers[0].Resources.Limits = corev1.ResourceList{
				corev1.ResourceCPU:    resource.MustParse("1500m"), // Exceeds 1000m limit
				corev1.ResourceMemory: resource.MustParse("3Gi"),   // Exceeds 2Gi limit
			}

			// Should be denied
			Expect(k8sClient.Update(ctx, pod)).ToNot(Succeed())

			// Clean up
			Expect(k8sClient.Delete(ctx, pod)).To(Succeed())
		})
	})

	Context("Job-based pod lifecycle", func() {
		var (
			ns  *corev1.Namespace
			crq *quotav1alpha1.ClusterResourceQuota
		)

		BeforeEach(func() {
			// Create namespace with specific label
			labelKey := "job-lifecycle-test-" + testSuffix
			labelValue := "lifecycle-test-" + testSuffix

			ns = &corev1.Namespace{
				ObjectMeta: metav1.ObjectMeta{
					Name: testNamespace,
					Labels: map[string]string{
						labelKey: labelValue,
					},
				},
			}
			Expect(k8sClient.Create(ctx, ns)).To(Succeed())

			// Create CRQ with restrictive limits
			crq = &quotav1alpha1.ClusterResourceQuota{
				ObjectMeta: metav1.ObjectMeta{
					Name: testCRQName,
				},
				Spec: quotav1alpha1.ClusterResourceQuotaSpec{
					NamespaceSelector: &metav1.LabelSelector{
						MatchLabels: map[string]string{
							labelKey: labelValue,
						},
					},
					Hard: quotav1alpha1.ResourceList{
						corev1.ResourceRequestsCPU:    resource.MustParse("200m"),
						corev1.ResourceRequestsMemory: resource.MustParse("512Mi"),
					},
				},
			}
			Expect(k8sClient.Create(ctx, crq)).To(Succeed())

			// Wait for the CRQ to be processed
			Eventually(func() bool {
				err := k8sClient.Get(ctx, client.ObjectKey{Name: testCRQName}, crq)
				return err == nil && crq.Status.Total.Hard != nil
			}, "30s", "1s").Should(BeTrue())
		})

		It("should allow pod creation after job succeeds", func() {
			// Create a job that uses most of the quota
			job := &batchv1.Job{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "success-job-" + testSuffix,
					Namespace: testNamespace,
				},
				Spec: batchv1.JobSpec{
					Template: corev1.PodTemplateSpec{
						Spec: corev1.PodSpec{
							Containers: []corev1.Container{
								{
									Name:  "test-container",
									Image: "busybox",
									Command: []string{
										"sh", "-c", "sleep 5 && exit 0",
									},
									Resources: corev1.ResourceRequirements{
										Requests: corev1.ResourceList{
											corev1.ResourceCPU:    resource.MustParse("150m"),
											corev1.ResourceMemory: resource.MustParse("400Mi"),
										},
									},
								},
							},
							RestartPolicy: corev1.RestartPolicyNever,
						},
					},
				},
			}

			Expect(k8sClient.Create(ctx, job)).To(Succeed())

			// Wait for the job to complete
			Eventually(func() bool {
				err := k8sClient.Get(ctx, client.ObjectKey{Name: job.Name, Namespace: job.Namespace}, job)
				if err != nil {
					return false
				}
				return job.Status.Succeeded > 0
			}, "30s", "1s").Should(BeTrue(), "Job should complete successfully")

			// Create a pod that uses the full quota
			pod := &corev1.Pod{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "full-quota-pod-" + testSuffix,
					Namespace: testNamespace,
				},
				Spec: corev1.PodSpec{
					Containers: []corev1.Container{
						{
							Name:  "test-container",
							Image: "nginx:latest",
							Resources: corev1.ResourceRequirements{
								Requests: corev1.ResourceList{
									corev1.ResourceCPU:    resource.MustParse("200m"),
									corev1.ResourceMemory: resource.MustParse("512Mi"),
								},
							},
						},
					},
				},
			}

			// The pod should be allowed since the job is completed
			Expect(k8sClient.Create(ctx, pod)).To(Succeed())
			Expect(k8sClient.Delete(ctx, pod)).To(Succeed())
		})

		It("should allow pod creation after job fails", func() {
			// Create a job that uses most of the quota but fails
			job := &batchv1.Job{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "failed-job-" + testSuffix,
					Namespace: testNamespace,
				},
				Spec: batchv1.JobSpec{
					Template: corev1.PodTemplateSpec{
						Spec: corev1.PodSpec{
							Containers: []corev1.Container{
								{
									Name:  "test-container",
									Image: "busybox",
									Command: []string{
										"sh", "-c", "sleep 1 && exit 1",
									},
									Resources: corev1.ResourceRequirements{
										Requests: corev1.ResourceList{
											corev1.ResourceCPU:    resource.MustParse("150m"),
											corev1.ResourceMemory: resource.MustParse("400Mi"),
										},
									},
								},
							},
							RestartPolicy: corev1.RestartPolicyNever,
						},
					},
				},
			}

			Expect(k8sClient.Create(ctx, job)).To(Succeed())

			// Wait for the job to fail
			Eventually(func() bool {
				err := k8sClient.Get(ctx, client.ObjectKey{Name: job.Name, Namespace: job.Namespace}, job)
				if err != nil {
					return false
				}
				return job.Status.Failed > 0
			}, "30s", "1s").Should(BeTrue(), "Job should fail")

			// Create a pod that uses the full quota
			pod := &corev1.Pod{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "full-quota-pod-" + testSuffix,
					Namespace: testNamespace,
				},
				Spec: corev1.PodSpec{
					Containers: []corev1.Container{
						{
							Name:  "test-container",
							Image: "nginx:latest",
							Resources: corev1.ResourceRequirements{
								Requests: corev1.ResourceList{
									corev1.ResourceCPU:    resource.MustParse("200m"),
									corev1.ResourceMemory: resource.MustParse("512Mi"),
								},
							},
						},
					},
				},
			}

			// The pod should be allowed since the job is completed
			Expect(k8sClient.Create(ctx, pod)).To(Succeed())
			Expect(k8sClient.Delete(ctx, pod)).To(Succeed())
		})
	})
})
