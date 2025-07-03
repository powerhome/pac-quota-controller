/*
Copyright 2025.

Licensed under the Apache License, Version 2.0 (the "License");
you may not use this file except in compliance with the License.
You may obtain a copy of the License at

    http://www.apache.org/licenses/LICENSE-2.0

Unless required by applicable law or agreed to in writing, software
distributed under the License is distributed on an "AS IS" BASIS,
WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
See the License for the specific language governing permissions and
limitations under the License.
*/

package e2e

import (
	"context"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"

	quotav1alpha1 "github.com/powerhome/pac-quota-controller/api/v1alpha1"
	corev1 "k8s.io/api/core/v1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/api/resource"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"sigs.k8s.io/controller-runtime/pkg/client"
)

var _ = Describe("ClusterResourceQuota Compute Resources E2E", func() {
	var (
		ctx            context.Context
		namespace1     *corev1.Namespace
		namespace2     *corev1.Namespace
		crq            *quotav1alpha1.ClusterResourceQuota
		testNamePrefix string
	)

	BeforeEach(func() {
		ctx = context.Background()
		testNamePrefix = generateTestName("compute-resources")

		// Create test namespaces with unique labels for each test
		namespace1 = &corev1.Namespace{
			ObjectMeta: metav1.ObjectMeta{
				Name: testNamePrefix + "-ns1",
				Labels: map[string]string{
					"test-run": testNamePrefix,
				},
			},
		}
		namespace2 = &corev1.Namespace{
			ObjectMeta: metav1.ObjectMeta{
				Name: testNamePrefix + "-ns2",
				Labels: map[string]string{
					"test-run": testNamePrefix,
				},
			},
		}

		Expect(k8sClient.Create(ctx, namespace1)).To(Succeed())
		Expect(k8sClient.Create(ctx, namespace2)).To(Succeed())

		// Create ClusterResourceQuota
		crq = &quotav1alpha1.ClusterResourceQuota{
			ObjectMeta: metav1.ObjectMeta{
				Name: testNamePrefix + "-crq",
			},
			Spec: quotav1alpha1.ClusterResourceQuotaSpec{
				NamespaceSelector: &metav1.LabelSelector{
					MatchLabels: map[string]string{
						"test-run": testNamePrefix,
					},
				},
				Hard: quotav1alpha1.ResourceList{
					corev1.ResourceRequestsCPU:    resource.MustParse("1000m"),
					corev1.ResourceRequestsMemory: resource.MustParse("2Gi"),
					corev1.ResourceLimitsCPU:      resource.MustParse("2000m"),
					corev1.ResourceLimitsMemory:   resource.MustParse("4Gi"),
					"nvidia.com/gpu":              resource.MustParse("10"),
					"example.com/foo":             resource.MustParse("20"),
				},
			},
		}
		Expect(k8sClient.Create(ctx, crq)).To(Succeed())

		// Wait for initial reconciliation to complete with empty state
		Eventually(func() bool {
			err := k8sClient.Get(ctx, client.ObjectKeyFromObject(crq), crq)
			if err != nil {
				return false
			}
			// Ensure that the namespaces are tracked but no resources are used yet
			cpuUsed, cpuExists := crq.Status.Total.Used[corev1.ResourceRequestsCPU]
			memUsed, memExists := crq.Status.Total.Used[corev1.ResourceRequestsMemory]

			return len(crq.Status.Namespaces) >= 2 &&
				(!cpuExists || cpuUsed.IsZero()) &&
				(!memExists || memUsed.IsZero())
		}, "10s", "250ms").Should(BeTrue())
	})

	AfterEach(func() {
		// Clean up pods first to avoid resource leakage
		if namespace1 != nil {
			podList := &corev1.PodList{}
			if err := k8sClient.List(ctx, podList, client.InNamespace(namespace1.Name)); err == nil {
				for i := range podList.Items {
					pod := &podList.Items[i]
					GinkgoWriter.Printf("Deleting pod %s/%s\n", pod.Namespace, pod.Name)
					_ = k8sClient.Delete(ctx, pod)
				}
			}
		}
		if namespace2 != nil {
			podList := &corev1.PodList{}
			if err := k8sClient.List(ctx, podList, client.InNamespace(namespace2.Name)); err == nil {
				for i := range podList.Items {
					pod := &podList.Items[i]
					GinkgoWriter.Printf("Deleting pod %s/%s\n", pod.Namespace, pod.Name)
					_ = k8sClient.Delete(ctx, pod)
				}
			}
		}

		// Clean up CRQ first
		if crq != nil {
			_ = k8sClient.Delete(ctx, crq)
			// Wait for CRQ deletion to complete
			Eventually(func() bool {
				err := k8sClient.Get(ctx, client.ObjectKeyFromObject(crq), &quotav1alpha1.ClusterResourceQuota{})
				return apierrors.IsNotFound(err)
			}, "10s", "250ms").Should(BeTrue())
		}

		// Clean up namespaces
		if namespace1 != nil {
			_ = k8sClient.Delete(ctx, namespace1)
			// Wait for namespace deletion to complete with longer timeout
			Eventually(func() bool {
				err := k8sClient.Get(ctx, client.ObjectKeyFromObject(namespace1), &corev1.Namespace{})
				return apierrors.IsNotFound(err)
			}, "30s", "1s").Should(BeTrue())
		}
		if namespace2 != nil {
			_ = k8sClient.Delete(ctx, namespace2)
			// Wait for namespace deletion to complete with longer timeout
			Eventually(func() bool {
				err := k8sClient.Get(ctx, client.ObjectKeyFromObject(namespace2), &corev1.Namespace{})
				return apierrors.IsNotFound(err)
			}, "30s", "1s").Should(BeTrue())
		}
	})

	Context("CPU and Memory Requests", func() {
		It("should calculate CPU and memory requests across namespaces", func() {
			// Create pods with CPU and memory requests
			pod1 := &corev1.Pod{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "pod1",
					Namespace: namespace1.Name,
				},
				Spec: corev1.PodSpec{
					Containers: []corev1.Container{
						{
							Name:  "container1",
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

			pod2 := &corev1.Pod{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "pod2",
					Namespace: namespace2.Name,
				},
				Spec: corev1.PodSpec{
					Containers: []corev1.Container{
						{
							Name:  "container1",
							Image: "nginx:latest",
							Resources: corev1.ResourceRequirements{
								Requests: corev1.ResourceList{
									corev1.ResourceCPU:    resource.MustParse("300m"),
									corev1.ResourceMemory: resource.MustParse("1Gi"),
								},
							},
						},
					},
				},
			}

			Expect(k8sClient.Create(ctx, pod1)).To(Succeed())
			Expect(k8sClient.Create(ctx, pod2)).To(Succeed())

			// Wait for reconciliation and check status
			Eventually(func() bool {
				err := k8sClient.Get(ctx, client.ObjectKeyFromObject(crq), crq)
				if err != nil {
					return false
				}

				// Check if CPU requests are calculated correctly (200m + 300m = 500m)
				cpuUsed, exists := crq.Status.Total.Used[corev1.ResourceRequestsCPU]
				if !exists {
					return false
				}

				// Check if Memory requests are calculated correctly (512Mi + 1Gi = 1536Mi)
				memUsed, exists := crq.Status.Total.Used[corev1.ResourceRequestsMemory]
				if !exists {
					return false
				}

				expectedCPU := resource.MustParse("500m")
				expectedMem := resource.MustParse("1536Mi")

				return cpuUsed.Equal(expectedCPU) && memUsed.Equal(expectedMem)
			}, "10s", "250ms").Should(BeTrue())

			// Verify namespace-specific usage
			Eventually(func() bool {
				err := k8sClient.Get(ctx, client.ObjectKeyFromObject(crq), crq)
				if err != nil {
					return false
				}

				if len(crq.Status.Namespaces) != 2 {
					return false
				}

				// Check individual namespace usage
				for _, nsStatus := range crq.Status.Namespaces {
					switch nsStatus.Namespace {
					case namespace1.Name:
						cpuUsed := nsStatus.Status.Used[corev1.ResourceRequestsCPU]
						memUsed := nsStatus.Status.Used[corev1.ResourceRequestsMemory]
						expectedCPU := resource.MustParse("200m")
						expectedMem := resource.MustParse("512Mi")
						if !cpuUsed.Equal(expectedCPU) || !memUsed.Equal(expectedMem) {
							return false
						}
					case namespace2.Name:
						cpuUsed := nsStatus.Status.Used[corev1.ResourceRequestsCPU]
						memUsed := nsStatus.Status.Used[corev1.ResourceRequestsMemory]
						expectedCPU := resource.MustParse("300m")
						expectedMem := resource.MustParse("1Gi")
						if !cpuUsed.Equal(expectedCPU) || !memUsed.Equal(expectedMem) {
							return false
						}
					}
				}
				return true
			}, "10s", "250ms").Should(BeTrue())
		})
	})

	Context("CPU and Memory Limits", func() {
		It("should calculate CPU and memory limits across namespaces", func() {
			// Create pods with CPU and memory limits
			pod1 := &corev1.Pod{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "pod-limits-1",
					Namespace: namespace1.Name,
				},
				Spec: corev1.PodSpec{
					Containers: []corev1.Container{
						{
							Name:  "container1",
							Image: "nginx:latest",
							Resources: corev1.ResourceRequirements{
								Requests: corev1.ResourceList{
									corev1.ResourceCPU:    resource.MustParse("200m"),
									corev1.ResourceMemory: resource.MustParse("512Mi"),
								},
								Limits: corev1.ResourceList{
									corev1.ResourceCPU:    resource.MustParse("500m"),
									corev1.ResourceMemory: resource.MustParse("1Gi"),
								},
							},
						},
					},
				},
			}

			pod2 := &corev1.Pod{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "pod-limits-2",
					Namespace: namespace2.Name,
				},
				Spec: corev1.PodSpec{
					Containers: []corev1.Container{
						{
							Name:  "container1",
							Image: "nginx:latest",
							Resources: corev1.ResourceRequirements{
								Requests: corev1.ResourceList{
									corev1.ResourceCPU:    resource.MustParse("300m"),
									corev1.ResourceMemory: resource.MustParse("1Gi"),
								},
								Limits: corev1.ResourceList{
									corev1.ResourceCPU:    resource.MustParse("700m"),
									corev1.ResourceMemory: resource.MustParse("2Gi"),
								},
							},
						},
					},
				},
			}

			Expect(k8sClient.Create(ctx, pod1)).To(Succeed())
			Expect(k8sClient.Create(ctx, pod2)).To(Succeed())

			// Wait for reconciliation and check limits usage
			Eventually(func() bool {
				err := k8sClient.Get(ctx, client.ObjectKeyFromObject(crq), crq)
				if err != nil {
					return false
				}

				// Check if CPU limits are calculated correctly (500m + 700m = 1200m)
				cpuUsed, exists := crq.Status.Total.Used[corev1.ResourceLimitsCPU]
				if !exists {
					return false
				}

				// Check if Memory limits are calculated correctly (1Gi + 2Gi = 3Gi)
				memUsed, exists := crq.Status.Total.Used[corev1.ResourceLimitsMemory]
				if !exists {
					return false
				}

				expectedCPU := resource.MustParse("1200m")
				expectedMem := resource.MustParse("3Gi")

				return cpuUsed.Equal(expectedCPU) && memUsed.Equal(expectedMem)
			}, "10s", "250ms").Should(BeTrue())
		})
	})

	Context("Init Containers", func() {
		It("should include init container resources in calculations", func() {
			// Create pod with init containers
			pod := &corev1.Pod{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "pod-with-init",
					Namespace: namespace1.Name,
				},
				Spec: corev1.PodSpec{
					InitContainers: []corev1.Container{
						{
							Name:  "init-container",
							Image: "busybox:latest",
							Resources: corev1.ResourceRequirements{
								Requests: corev1.ResourceList{
									corev1.ResourceCPU:    resource.MustParse("100m"),
									corev1.ResourceMemory: resource.MustParse("256Mi"),
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
									corev1.ResourceCPU:    resource.MustParse("200m"),
									corev1.ResourceMemory: resource.MustParse("512Mi"),
								},
							},
						},
					},
				},
			}

			Expect(k8sClient.Create(ctx, pod)).To(Succeed())

			// Wait for reconciliation and verify init containers are included
			Eventually(func() bool {
				err := k8sClient.Get(ctx, client.ObjectKeyFromObject(crq), crq)
				if err != nil {
					return false
				}

				// Check CPU usage: sum(init: 100m + regular: 200m) = 300m
				cpuUsed, exists := crq.Status.Total.Used[corev1.ResourceRequestsCPU]
				if !exists {
					return false
				}

				// Check memory usage: sum(init: 256Mi + regular: 512Mi) = 768Mi
				memUsed, exists := crq.Status.Total.Used[corev1.ResourceRequestsMemory]
				if !exists {
					return false
				}

				expectedCPU := resource.MustParse("300m")
				expectedMem := resource.MustParse("768Mi")
				GinkgoWriter.Printf("CPU Used: %s, Memory Used: %s\n", cpuUsed.String(), memUsed.String())
				GinkgoWriter.Printf("Expected CPU: %s, Expected Memory: %s\n", expectedCPU.String(), expectedMem.String())

				return cpuUsed.Equal(expectedCPU) && memUsed.Equal(expectedMem)
			}, "10s", "250ms").Should(BeTrue())
		})
	})

	Context("Hugepages Support", func() {
		It("should calculate hugepages usage correctly", func() {
			// Create pod with hugepages (must include CPU/memory requirements)
			pod := &corev1.Pod{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "hugepages-pod",
					Namespace: namespace1.Name,
				},
				Spec: corev1.PodSpec{
					Containers: []corev1.Container{
						{
							Name:  "container1",
							Image: "nginx:latest",
							Resources: corev1.ResourceRequirements{
								Requests: corev1.ResourceList{
									corev1.ResourceCPU:    resource.MustParse("100m"),
									corev1.ResourceMemory: resource.MustParse("128Mi"),
									"hugepages-2Mi":       resource.MustParse("1Gi"),
								},
								Limits: corev1.ResourceList{
									corev1.ResourceCPU:    resource.MustParse("200m"),
									corev1.ResourceMemory: resource.MustParse("256Mi"),
									"hugepages-2Mi":       resource.MustParse("1Gi"),
								},
							},
						},
					},
				},
			}

			// Update CRQ to include hugepages quota (retry on conflicts)
			Eventually(func() error {
				// Get the latest version of the CRQ
				if err := k8sClient.Get(ctx, client.ObjectKeyFromObject(crq), crq); err != nil {
					return err
				}
				crq.Spec.Hard["hugepages-2Mi"] = resource.MustParse("2Gi")
				return k8sClient.Update(ctx, crq)
			}, "5s", "100ms").Should(Succeed())

			Expect(k8sClient.Create(ctx, pod)).To(Succeed())

			// Wait for reconciliation and check hugepages usage
			Eventually(func() bool {
				err := k8sClient.Get(ctx, client.ObjectKeyFromObject(crq), crq)
				if err != nil {
					return false
				}

				hugepagesUsed, exists := crq.Status.Total.Used["hugepages-2Mi"]
				if !exists {
					return false
				}

				expectedHugepages := resource.MustParse("1Gi")
				return hugepagesUsed.Equal(expectedHugepages)
			}, "10s", "250ms").Should(BeTrue())
		})
	})

	Context("Extended Resources", func() {
		It("should calculate GPU usage correctly", func() {
			// Create pod with GPU requests
			pod := &corev1.Pod{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "gpu-pod",
					Namespace: namespace1.Name,
				},
				Spec: corev1.PodSpec{
					Containers: []corev1.Container{
						{
							Name:  "gpu-container",
							Image: "nvidia/cuda:latest",
							Resources: corev1.ResourceRequirements{
								Limits: corev1.ResourceList{
									"nvidia.com/gpu": resource.MustParse("2"),
								},
							},
						},
					},
				},
			}

			Expect(k8sClient.Create(ctx, pod)).To(Succeed())

			// Wait for reconciliation and check GPU usage
			Eventually(func() bool {
				err := k8sClient.Get(ctx, client.ObjectKeyFromObject(crq), crq)
				if err != nil {
					return false
				}

				gpuUsed, exists := crq.Status.Total.Used["nvidia.com/gpu"]
				if !exists {
					return false
				}

				expectedGPU := resource.MustParse("2")
				return gpuUsed.Equal(expectedGPU)
			}, "10s", "250ms").Should(BeTrue())
		})

		It("should calculate custom resource usage correctly", func() {
			// Create pod with custom resource requests
			pod := &corev1.Pod{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "custom-resource-pod",
					Namespace: namespace1.Name,
				},
				Spec: corev1.PodSpec{
					Containers: []corev1.Container{
						{
							Name:  "custom-container",
							Image: "busybox:latest",
							Resources: corev1.ResourceRequirements{
								Limits: corev1.ResourceList{
									"example.com/foo": resource.MustParse("5"),
								},
							},
						},
					},
				},
			}

			Expect(k8sClient.Create(ctx, pod)).To(Succeed())

			// Wait for reconciliation and check custom resource usage
			Eventually(func() bool {
				err := k8sClient.Get(ctx, client.ObjectKeyFromObject(crq), crq)
				if err != nil {
					return false
				}

				customUsed, exists := crq.Status.Total.Used["example.com/foo"]
				if !exists {
					return false
				}

				expectedCustom := resource.MustParse("5")
				return customUsed.Equal(expectedCustom)
			}, "10s", "250ms").Should(BeTrue())
		})
	})

	Context("Resource Validation", func() {
		It("should validate resource requests, limits, and extended resources", func() {
			By("creating pods with CPU and memory requests")
			pod1 := &corev1.Pod{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "pod1",
					Namespace: namespace1.Name,
				},
				Spec: corev1.PodSpec{
					Containers: []corev1.Container{
						{
							Name:  "container1",
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

			pod2 := &corev1.Pod{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "pod2",
					Namespace: namespace2.Name,
				},
				Spec: corev1.PodSpec{
					Containers: []corev1.Container{
						{
							Name:  "container1",
							Image: "nginx:latest",
							Resources: corev1.ResourceRequirements{
								Requests: corev1.ResourceList{
									corev1.ResourceCPU:    resource.MustParse("300m"),
									corev1.ResourceMemory: resource.MustParse("1Gi"),
								},
							},
						},
					},
				},
			}

			Expect(k8sClient.Create(ctx, pod1)).To(Succeed())
			Expect(k8sClient.Create(ctx, pod2)).To(Succeed())

			By("validating CPU and memory requests across namespaces")
			Eventually(func() bool {
				err := k8sClient.Get(ctx, client.ObjectKeyFromObject(crq), crq)
				if err != nil {
					return false
				}

				cpuUsed, exists := crq.Status.Total.Used[corev1.ResourceRequestsCPU]
				if !exists {
					return false
				}

				memUsed, exists := crq.Status.Total.Used[corev1.ResourceRequestsMemory]
				if !exists {
					return false
				}

				expectedCPU := resource.MustParse("500m")
				expectedMem := resource.MustParse("1536Mi")

				return cpuUsed.Equal(expectedCPU) && memUsed.Equal(expectedMem)
			}, "10s", "250ms").Should(BeTrue())

			By("creating pods with extended resources")
			pod3 := &corev1.Pod{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "gpu-pod",
					Namespace: namespace1.Name,
				},
				Spec: corev1.PodSpec{
					Containers: []corev1.Container{
						{
							Name:  "gpu-container",
							Image: "nvidia/cuda:latest",
							Resources: corev1.ResourceRequirements{
								Limits: corev1.ResourceList{
									"nvidia.com/gpu": resource.MustParse("2"),
								},
							},
						},
					},
				},
			}

			pod4 := &corev1.Pod{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "custom-resource-pod",
					Namespace: namespace1.Name,
				},
				Spec: corev1.PodSpec{
					Containers: []corev1.Container{
						{
							Name:  "custom-container",
							Image: "busybox:latest",
							Resources: corev1.ResourceRequirements{
								Limits: corev1.ResourceList{
									"example.com/foo": resource.MustParse("5"),
								},
							},
						},
					},
				},
			}

			Expect(k8sClient.Create(ctx, pod3)).To(Succeed())
			Expect(k8sClient.Create(ctx, pod4)).To(Succeed())

			By("validating extended resource usage")
			Eventually(func() bool {
				err := k8sClient.Get(ctx, client.ObjectKeyFromObject(crq), crq)
				if err != nil {
					return false
				}

				gpuUsed, exists := crq.Status.Total.Used["nvidia.com/gpu"]
				if !exists {
					return false
				}

				customUsed, exists := crq.Status.Total.Used["example.com/foo"]
				if !exists {
					return false
				}

				expectedGPU := resource.MustParse("2")
				expectedCustom := resource.MustParse("5")

				return gpuUsed.Equal(expectedGPU) && customUsed.Equal(expectedCustom)
			}, "10s", "250ms").Should(BeTrue())

			By("cleaning up resources")
			Expect(k8sClient.Delete(ctx, pod1)).To(Succeed())
			Expect(k8sClient.Delete(ctx, pod2)).To(Succeed())
			Expect(k8sClient.Delete(ctx, pod3)).To(Succeed())
			Expect(k8sClient.Delete(ctx, pod4)).To(Succeed())
		})
	})
})
