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

package v1alpha1

import (
	"context"
	"errors"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	quotav1alpha1 "github.com/powerhome/pac-quota-controller/api/v1alpha1"
	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/resource"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/client/fake"
)

// mockCRQClient implements quota.CRQClientInterface for testing
type mockCRQClient struct {
	crqs     map[string]*quotav1alpha1.ClusterResourceQuota
	selector func(*corev1.Namespace) (*quotav1alpha1.ClusterResourceQuota, error)
}

func newMockCRQClient() *mockCRQClient {
	return &mockCRQClient{
		crqs: make(map[string]*quotav1alpha1.ClusterResourceQuota),
	}
}

func (m *mockCRQClient) ListAllCRQs(ctx context.Context) ([]quotav1alpha1.ClusterResourceQuota, error) {
	result := make([]quotav1alpha1.ClusterResourceQuota, 0, len(m.crqs))
	for _, crq := range m.crqs {
		result = append(result, *crq)
	}
	return result, nil
}

func (m *mockCRQClient) GetCRQByNamespace(ctx context.Context, ns *corev1.Namespace) (*quotav1alpha1.ClusterResourceQuota, error) {
	if m.selector != nil {
		return m.selector(ns)
	}
	return nil, nil
}

func (m *mockCRQClient) NamespaceMatchesCRQ(ns *corev1.Namespace, crq *quotav1alpha1.ClusterResourceQuota) (bool, error) {
	return false, nil
}

func (m *mockCRQClient) GetNamespacesFromStatus(crq *quotav1alpha1.ClusterResourceQuota) []string {
	return []string{}
}

func (m *mockCRQClient) setCRQ(name string, crq *quotav1alpha1.ClusterResourceQuota) {
	m.crqs[name] = crq
}

func (m *mockCRQClient) setSelector(fn func(*corev1.Namespace) (*quotav1alpha1.ClusterResourceQuota, error)) {
	m.selector = fn
}

var _ = Describe("Pod Webhook", func() {
	var (
		ctx        context.Context
		validator  *PodCustomValidator
		fakeClient client.Client
		mockCRQ    *mockCRQClient
		testNS     *corev1.Namespace
		testCRQ    *quotav1alpha1.ClusterResourceQuota
	)

	BeforeEach(func() {
		ctx = context.Background()

		// Set up fake client with test scheme
		scheme := runtime.NewScheme()
		Expect(corev1.AddToScheme(scheme)).To(Succeed())
		Expect(quotav1alpha1.AddToScheme(scheme)).To(Succeed())

		fakeClient = fake.NewClientBuilder().WithScheme(scheme).Build()
		mockCRQ = newMockCRQClient()

		validator = &PodCustomValidator{
			Client:    fakeClient,
			crqClient: mockCRQ,
		}

		// Create test namespace
		testNS = &corev1.Namespace{
			ObjectMeta: metav1.ObjectMeta{
				Name: "test-namespace",
				Labels: map[string]string{
					"app": "test",
				},
			},
		}
		Expect(fakeClient.Create(ctx, testNS)).To(Succeed())

		// Create test ClusterResourceQuota
		testCRQ = &quotav1alpha1.ClusterResourceQuota{
			ObjectMeta: metav1.ObjectMeta{
				Name: "test-crq",
			},
			Spec: quotav1alpha1.ClusterResourceQuotaSpec{
				NamespaceSelector: &metav1.LabelSelector{
					MatchLabels: map[string]string{
						"app": "test",
					},
				},
				Hard: quotav1alpha1.ResourceList{
					corev1.ResourceRequestsCPU:    resource.MustParse("2"),
					corev1.ResourceRequestsMemory: resource.MustParse("4Gi"),
					corev1.ResourceLimitsCPU:      resource.MustParse("4"),
					corev1.ResourceLimitsMemory:   resource.MustParse("8Gi"),
				},
			},
			Status: quotav1alpha1.ClusterResourceQuotaStatus{
				Total: quotav1alpha1.ResourceQuotaStatus{
					Hard: quotav1alpha1.ResourceList{
						corev1.ResourceRequestsCPU:    resource.MustParse("2"),
						corev1.ResourceRequestsMemory: resource.MustParse("4Gi"),
						corev1.ResourceLimitsCPU:      resource.MustParse("4"),
						corev1.ResourceLimitsMemory:   resource.MustParse("8Gi"),
					},
					Used: quotav1alpha1.ResourceList{
						corev1.ResourceRequestsCPU:    resource.MustParse("1"),
						corev1.ResourceRequestsMemory: resource.MustParse("2Gi"),
						corev1.ResourceLimitsCPU:      resource.MustParse("2"),
						corev1.ResourceLimitsMemory:   resource.MustParse("4Gi"),
					},
				},
			},
		}
		Expect(fakeClient.Create(ctx, testCRQ)).To(Succeed())
		mockCRQ.setCRQ("test-crq", testCRQ)
	})

	Describe("ValidateCreate", func() {
		var testPod *corev1.Pod

		BeforeEach(func() {
			testPod = &corev1.Pod{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "test-pod",
					Namespace: "test-namespace",
				},
				Spec: corev1.PodSpec{
					Containers: []corev1.Container{
						{
							Name:  "test-container",
							Image: "nginx:latest",
							Resources: corev1.ResourceRequirements{
								Requests: corev1.ResourceList{
									corev1.ResourceCPU:    resource.MustParse("500m"),
									corev1.ResourceMemory: resource.MustParse("1Gi"),
								},
								Limits: corev1.ResourceList{
									corev1.ResourceCPU:    resource.MustParse("1"),
									corev1.ResourceMemory: resource.MustParse("2Gi"),
								},
							},
						},
					},
				},
			}
		})

		Context("when pod object is invalid", func() {
			It("should return error for non-Pod object", func() {
				invalidObj := &corev1.ConfigMap{}
				_, err := validator.ValidateCreate(ctx, invalidObj)
				Expect(err).To(HaveOccurred())
				Expect(err.Error()).To(ContainSubstring("expected a Pod object but got"))
			})
		})

		Context("when namespace doesn't exist", func() {
			It("should return error", func() {
				testPod.Namespace = "non-existent-namespace"
				_, err := validator.ValidateCreate(ctx, testPod)
				Expect(err).To(HaveOccurred())
				Expect(err.Error()).To(ContainSubstring("failed to get namespace"))
			})
		})

		Context("when CRQ client fails", func() {
			It("should return error when GetCRQByNamespace fails", func() {
				mockCRQ.setSelector(func(*corev1.Namespace) (*quotav1alpha1.ClusterResourceQuota, error) {
					return nil, errors.New("CRQ client error")
				})
				_, err := validator.ValidateCreate(ctx, testPod)
				Expect(err).To(HaveOccurred())
				Expect(err.Error()).To(ContainSubstring("failed to get CRQ for namespace"))
			})
		})

		Context("when no CRQ selects the namespace", func() {
			It("should allow pod creation", func() {
				mockCRQ.setSelector(func(*corev1.Namespace) (*quotav1alpha1.ClusterResourceQuota, error) {
					return nil, nil
				})
				_, err := validator.ValidateCreate(ctx, testPod)
				Expect(err).ToNot(HaveOccurred())
			})
		})

		Context("when CRQ selects the namespace", func() {
			BeforeEach(func() {
				mockCRQ.setSelector(func(*corev1.Namespace) (*quotav1alpha1.ClusterResourceQuota, error) {
					return testCRQ, nil
				})
			})

			It("should allow pod creation within limits", func() {
				_, err := validator.ValidateCreate(ctx, testPod)
				Expect(err).ToNot(HaveOccurred())
			})

			It("should deny pod creation when CPU requests exceed limit", func() {
				testPod.Spec.Containers[0].Resources.Requests[corev1.ResourceCPU] = resource.MustParse("2")
				_, err := validator.ValidateCreate(ctx, testPod)
				Expect(err).To(HaveOccurred())
				Expect(err.Error()).To(ContainSubstring("would exceed ClusterResourceQuota"))
				Expect(err.Error()).To(ContainSubstring("requests.cpu"))
			})

			It("should deny pod creation when memory requests exceed limit", func() {
				testPod.Spec.Containers[0].Resources.Requests[corev1.ResourceMemory] = resource.MustParse("3Gi")
				_, err := validator.ValidateCreate(ctx, testPod)
				Expect(err).To(HaveOccurred())
				Expect(err.Error()).To(ContainSubstring("would exceed ClusterResourceQuota"))
				Expect(err.Error()).To(ContainSubstring("requests.memory"))
			})

			It("should deny pod creation when CPU limits exceed limit", func() {
				testPod.Spec.Containers[0].Resources.Limits[corev1.ResourceCPU] = resource.MustParse("3")
				_, err := validator.ValidateCreate(ctx, testPod)
				Expect(err).To(HaveOccurred())
				Expect(err.Error()).To(ContainSubstring("would exceed ClusterResourceQuota"))
				Expect(err.Error()).To(ContainSubstring("limits.cpu"))
			})

			It("should deny pod creation when memory limits exceed limit", func() {
				testPod.Spec.Containers[0].Resources.Limits[corev1.ResourceMemory] = resource.MustParse("5Gi")
				_, err := validator.ValidateCreate(ctx, testPod)
				Expect(err).To(HaveOccurred())
				Expect(err.Error()).To(ContainSubstring("would exceed ClusterResourceQuota"))
				Expect(err.Error()).To(ContainSubstring("limits.memory"))
			})

			It("should handle pods with multiple containers", func() {
				testPod.Spec.Containers = append(testPod.Spec.Containers, corev1.Container{
					Name:  "second-container",
					Image: "alpine:latest",
					Resources: corev1.ResourceRequirements{
						Requests: corev1.ResourceList{
							corev1.ResourceCPU:    resource.MustParse("400m"),
							corev1.ResourceMemory: resource.MustParse("1Gi"),
						},
					},
				})
				_, err := validator.ValidateCreate(ctx, testPod)
				Expect(err).ToNot(HaveOccurred())
			})

			It("should handle pods with init containers", func() {
				testPod.Spec.InitContainers = []corev1.Container{
					{
						Name:  "init-container",
						Image: "busybox:latest",
						Resources: corev1.ResourceRequirements{
							Requests: corev1.ResourceList{
								corev1.ResourceCPU:    resource.MustParse("100m"),
								corev1.ResourceMemory: resource.MustParse("500Mi"),
							},
						},
					},
				}
				_, err := validator.ValidateCreate(ctx, testPod)
				Expect(err).ToNot(HaveOccurred())
			})

			It("should allow pod with no tracked resources", func() {
				testPod.Spec.Containers[0].Resources = corev1.ResourceRequirements{}
				_, err := validator.ValidateCreate(ctx, testPod)
				Expect(err).ToNot(HaveOccurred())
			})

			It("should return error when CRQ status cannot be retrieved", func() {
				// Remove the CRQ from the fake client to simulate not found
				Expect(fakeClient.Delete(ctx, testCRQ)).To(Succeed())
				_, err := validator.ValidateCreate(ctx, testPod)
				Expect(err).To(HaveOccurred())
				Expect(err.Error()).To(ContainSubstring("failed to get current CRQ status"))
			})

			It("should handle pods with limits but no requests", func() {
				testPod.Spec.Containers[0].Resources = corev1.ResourceRequirements{
					Limits: corev1.ResourceList{
						corev1.ResourceCPU:    resource.MustParse("1"),
						corev1.ResourceMemory: resource.MustParse("2Gi"),
					},
					// No requests specified - should not count towards requests.cpu/requests.memory
				}
				_, err := validator.ValidateCreate(ctx, testPod)
				Expect(err).ToNot(HaveOccurred())
			})

			It("should handle pods with requests but no limits", func() {
				testPod.Spec.Containers[0].Resources = corev1.ResourceRequirements{
					Requests: corev1.ResourceList{
						corev1.ResourceCPU:    resource.MustParse("500m"),
						corev1.ResourceMemory: resource.MustParse("1Gi"),
					},
					// No limits specified - should not count towards limits.cpu/limits.memory
				}
				_, err := validator.ValidateCreate(ctx, testPod)
				Expect(err).ToNot(HaveOccurred())
			})

			It("should deny pod creation when limits-only pod exceeds limits quota", func() {
				testPod.Spec.Containers[0].Resources = corev1.ResourceRequirements{
					Limits: corev1.ResourceList{
						corev1.ResourceCPU: resource.MustParse("3"), // Would exceed the 4 CPU limit (2 already used)
					},
				}
				_, err := validator.ValidateCreate(ctx, testPod)
				Expect(err).To(HaveOccurred())
				Expect(err.Error()).To(ContainSubstring("would exceed ClusterResourceQuota"))
				// Will fail on CPU first since it's checked first
				Expect(err.Error()).To(ContainSubstring("limits.cpu"))
			})

			It("should handle mixed resource configurations", func() {
				testPod.Spec.Containers[0].Resources = corev1.ResourceRequirements{
					Requests: corev1.ResourceList{
						corev1.ResourceCPU: resource.MustParse("200m"),
						// Memory request not specified
					},
					Limits: corev1.ResourceList{
						corev1.ResourceMemory: resource.MustParse("1Gi"),
						// CPU limit not specified
					},
				}
				_, err := validator.ValidateCreate(ctx, testPod)
				Expect(err).ToNot(HaveOccurred())
			})

			It("should handle pods with only non-tracked resources", func() {
				testPod.Spec.Containers[0].Resources = corev1.ResourceRequirements{
					Requests: corev1.ResourceList{
						"example.com/custom-resource": resource.MustParse("5"),
					},
					Limits: corev1.ResourceList{
						"example.com/another-resource": resource.MustParse("10"),
					},
				}
				_, err := validator.ValidateCreate(ctx, testPod)
				Expect(err).ToNot(HaveOccurred())
			})

			It("should allow creation of pods that start in terminal state", func() {
				// Some edge case scenarios where a pod might be created in terminal state
				testPod.Status.Phase = corev1.PodSucceeded
				testPod.Spec.Containers[0].Resources.Requests[corev1.ResourceCPU] = resource.MustParse("5") // Would normally exceed quota
				_, err := validator.ValidateCreate(ctx, testPod)
				Expect(err).ToNot(HaveOccurred())
			})
		})
	})

	Describe("ValidateUpdate", func() {
		const testNginxImage = "nginx:1.20"
		var oldPod, newPod *corev1.Pod

		BeforeEach(func() {
			oldPod = &corev1.Pod{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "test-pod",
					Namespace: "test-namespace",
				},
				Spec: corev1.PodSpec{
					Containers: []corev1.Container{
						{
							Name:  "test-container",
							Image: "nginx:latest",
							Resources: corev1.ResourceRequirements{
								Requests: corev1.ResourceList{
									corev1.ResourceCPU:    resource.MustParse("500m"),
									corev1.ResourceMemory: resource.MustParse("1Gi"),
								},
								Limits: corev1.ResourceList{
									corev1.ResourceCPU:    resource.MustParse("1"),
									corev1.ResourceMemory: resource.MustParse("2Gi"),
								},
							},
						},
					},
				},
			}

			newPod = oldPod.DeepCopy()
		})

		Context("when pod objects are invalid", func() {
			It("should return error for invalid newObj", func() {
				invalidObj := &corev1.ConfigMap{}
				_, err := validator.ValidateUpdate(ctx, oldPod, invalidObj)
				Expect(err).To(HaveOccurred())
				Expect(err.Error()).To(ContainSubstring("expected a Pod object for the newObj"))
			})

			It("should return error for invalid oldObj", func() {
				invalidObj := &corev1.ConfigMap{}
				_, err := validator.ValidateUpdate(ctx, invalidObj, newPod)
				Expect(err).To(HaveOccurred())
				Expect(err.Error()).To(ContainSubstring("expected a Pod object for the oldObj"))
			})
		})

		Context("when pod spec hasn't changed", func() {
			It("should allow update for status-only changes", func() {
				// Only status changes, spec remains the same
				newPod.Status.Phase = corev1.PodRunning
				_, err := validator.ValidateUpdate(ctx, oldPod, newPod)
				Expect(err).ToNot(HaveOccurred())
			})
		})

		Context("when namespace doesn't exist", func() {
			It("should return error", func() {
				newPod.Namespace = "non-existent-namespace"
				newPod.Spec.Containers[0].Image = testNginxImage
				_, err := validator.ValidateUpdate(ctx, oldPod, newPod)
				Expect(err).To(HaveOccurred())
				Expect(err.Error()).To(ContainSubstring("failed to get namespace"))
			})
		})

		Context("when no CRQ selects the namespace", func() {
			It("should allow pod update", func() {
				mockCRQ.setSelector(func(*corev1.Namespace) (*quotav1alpha1.ClusterResourceQuota, error) {
					return nil, nil
				})
				newPod.Spec.Containers[0].Image = testNginxImage
				_, err := validator.ValidateUpdate(ctx, oldPod, newPod)
				Expect(err).ToNot(HaveOccurred())
			})
		})

		Context("when CRQ selects the namespace", func() {
			BeforeEach(func() {
				mockCRQ.setSelector(func(*corev1.Namespace) (*quotav1alpha1.ClusterResourceQuota, error) {
					return testCRQ, nil
				})
			})

			It("should allow pod update within limits", func() {
				newPod.Spec.Containers[0].Resources.Requests[corev1.ResourceCPU] = resource.MustParse("600m")
				_, err := validator.ValidateUpdate(ctx, oldPod, newPod)
				Expect(err).ToNot(HaveOccurred())
			})

			It("should deny pod update when CPU increase exceeds limit", func() {
				newPod.Spec.Containers[0].Resources.Requests[corev1.ResourceCPU] = resource.MustParse("2")
				_, err := validator.ValidateUpdate(ctx, oldPod, newPod)
				Expect(err).To(HaveOccurred())
				Expect(err.Error()).To(ContainSubstring("would exceed ClusterResourceQuota"))
				Expect(err.Error()).To(ContainSubstring("requests.cpu"))
			})

			It("should allow pod update when resources decrease", func() {
				newPod.Spec.Containers[0].Resources.Requests[corev1.ResourceCPU] = resource.MustParse("300m")
				_, err := validator.ValidateUpdate(ctx, oldPod, newPod)
				Expect(err).ToNot(HaveOccurred())
			})

			It("should allow pod update when no resource delta", func() {
				newPod.Spec.Containers[0].Image = testNginxImage // Non-resource change
				_, err := validator.ValidateUpdate(ctx, oldPod, newPod)
				Expect(err).ToNot(HaveOccurred())
			})

			It("should handle mixed resource changes (some increase, some decrease)", func() {
				// Increase CPU, decrease memory
				newPod.Spec.Containers[0].Resources.Requests[corev1.ResourceCPU] = resource.MustParse("600m")
				newPod.Spec.Containers[0].Resources.Requests[corev1.ResourceMemory] = resource.MustParse("512Mi")
				_, err := validator.ValidateUpdate(ctx, oldPod, newPod)
				Expect(err).ToNot(HaveOccurred())
			})
		})

		Context("when dealing with terminal pods", func() {
			BeforeEach(func() {
				mockCRQ.setSelector(func(*corev1.Namespace) (*quotav1alpha1.ClusterResourceQuota, error) {
					return testCRQ, nil
				})
			})

			It("should allow updates to terminal pods (Succeeded)", func() {
				// Create an old pod that's succeeded
				oldPod.Status.Phase = corev1.PodSucceeded
				newPod.Status.Phase = corev1.PodSucceeded
				// Change some resource (this shouldn't matter for terminal pods)
				newPod.Spec.Containers[0].Resources.Requests[corev1.ResourceCPU] = resource.MustParse("2")

				_, err := validator.ValidateUpdate(ctx, oldPod, newPod)
				Expect(err).ToNot(HaveOccurred())
			})

			It("should allow updates to terminal pods (Failed)", func() {
				// Create an old pod that's failed
				oldPod.Status.Phase = corev1.PodFailed
				newPod.Status.Phase = corev1.PodFailed
				// Change some resource (this shouldn't matter for terminal pods)
				newPod.Spec.Containers[0].Resources.Requests[corev1.ResourceCPU] = resource.MustParse("2")

				_, err := validator.ValidateUpdate(ctx, oldPod, newPod)
				Expect(err).ToNot(HaveOccurred())
			})

			It("should validate transition from Running to Terminal state", func() {
				// Pod is running and being updated to succeeded - this is allowed
				oldPod.Status.Phase = corev1.PodRunning
				newPod.Status.Phase = corev1.PodSucceeded
				// Spec stays the same, only status changes

				_, err := validator.ValidateUpdate(ctx, oldPod, newPod)
				Expect(err).ToNot(HaveOccurred())
			})
		})

		Context("when updating resource configurations", func() {
			BeforeEach(func() {
				mockCRQ.setSelector(func(*corev1.Namespace) (*quotav1alpha1.ClusterResourceQuota, error) {
					return testCRQ, nil
				})
			})

			It("should handle limits-only to requests-only changes", func() {
				// Old pod has only limits
				oldPod.Spec.Containers[0].Resources = corev1.ResourceRequirements{
					Limits: corev1.ResourceList{
						corev1.ResourceCPU:    resource.MustParse("1"),
						corev1.ResourceMemory: resource.MustParse("2Gi"),
					},
				}
				// New pod has only requests
				newPod.Spec.Containers[0].Resources = corev1.ResourceRequirements{
					Requests: corev1.ResourceList{
						corev1.ResourceCPU:    resource.MustParse("500m"),
						corev1.ResourceMemory: resource.MustParse("1Gi"),
					},
				}

				_, err := validator.ValidateUpdate(ctx, oldPod, newPod)
				Expect(err).ToNot(HaveOccurred())
			})

			It("should handle requests-only to limits-only changes", func() {
				// Old pod has only requests
				oldPod.Spec.Containers[0].Resources = corev1.ResourceRequirements{
					Requests: corev1.ResourceList{
						corev1.ResourceCPU:    resource.MustParse("500m"),
						corev1.ResourceMemory: resource.MustParse("1Gi"),
					},
				}
				// New pod has only limits
				newPod.Spec.Containers[0].Resources = corev1.ResourceRequirements{
					Limits: corev1.ResourceList{
						corev1.ResourceCPU:    resource.MustParse("1"),
						corev1.ResourceMemory: resource.MustParse("2Gi"),
					},
				}

				_, err := validator.ValidateUpdate(ctx, oldPod, newPod)
				Expect(err).ToNot(HaveOccurred())
			})

			It("should deny update when adding limits exceeds quota", func() {
				// Old pod has no limits
				oldPod.Spec.Containers[0].Resources = corev1.ResourceRequirements{
					Requests: corev1.ResourceList{
						corev1.ResourceCPU:    resource.MustParse("500m"),
						corev1.ResourceMemory: resource.MustParse("1Gi"),
					},
				}
				// New pod adds limits that exceed quota
				newPod.Spec.Containers[0].Resources = corev1.ResourceRequirements{
					Requests: corev1.ResourceList{
						corev1.ResourceCPU:    resource.MustParse("500m"),
						corev1.ResourceMemory: resource.MustParse("1Gi"),
					},
					Limits: corev1.ResourceList{
						corev1.ResourceCPU:    resource.MustParse("3"),   // Would exceed limits quota
						corev1.ResourceMemory: resource.MustParse("5Gi"), // Would exceed limits quota
					},
				}

				_, err := validator.ValidateUpdate(ctx, oldPod, newPod)
				Expect(err).To(HaveOccurred())
				Expect(err.Error()).To(ContainSubstring("would exceed ClusterResourceQuota"))
			})
		})
	})
})
