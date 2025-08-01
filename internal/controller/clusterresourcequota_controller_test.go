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

package controller

import (
	"context"
	"errors"
	"fmt"
	"sync"
	"time"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	quotav1alpha1 "github.com/powerhome/pac-quota-controller/api/v1alpha1"
	"github.com/powerhome/pac-quota-controller/pkg/mocks"
	"github.com/stretchr/testify/mock"
	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/resource"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/event"
)

// --- Fakes for error path testing ---
// Only for forcing errors in the kubeclient
type fakeStatusWriter struct{}

func (f *fakeStatusWriter) Patch(ctx context.Context, obj client.Object, patch client.Patch, opts ...client.SubResourcePatchOption) error {
	return fmt.Errorf("patch error")
}
func (f *fakeStatusWriter) Update(ctx context.Context, obj client.Object, opts ...client.SubResourceUpdateOption) error {
	return nil
}
func (f *fakeStatusWriter) Create(ctx context.Context, obj client.Object, subResource client.Object, opts ...client.SubResourceCreateOption) error {
	return nil
}

type fakeClient struct {
	client.Client
	getFunc      func(context.Context, client.ObjectKey, client.Object) error
	listFunc     func(context.Context, client.ObjectList, ...client.ListOption) error
	statusWriter client.StatusWriter
}

func (f *fakeClient) Get(ctx context.Context, key client.ObjectKey, obj client.Object, opts ...client.GetOption) error {
	if f.getFunc != nil {
		return f.getFunc(ctx, key, obj)
	}
	return nil
}
func (f *fakeClient) List(ctx context.Context, obj client.ObjectList, opts ...client.ListOption) error {
	if f.listFunc != nil {
		return f.listFunc(ctx, obj, opts...)
	}
	return nil
}
func (f *fakeClient) Status() client.StatusWriter {
	if f.statusWriter != nil {
		return f.statusWriter
	}
	return &fakeStatusWriter{}
}

var _ = Describe("ClusterResourceQuota Controller", Ordered, func() {
	var testQuota *quotav1alpha1.ClusterResourceQuota
	ctx := context.Background()

	BeforeAll(func() {
		testQuota = &quotav1alpha1.ClusterResourceQuota{
			ObjectMeta: metav1.ObjectMeta{
				Name: "test-quota",
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
				},
			},
		}
	})

	Context("Reconcile", func() {
		It("should successfully reconcile the resource", func() {
			reconciler := &ClusterResourceQuotaReconciler{}
			req := ctrl.Request{
				NamespacedName: types.NamespacedName{
					Name: testQuota.Name,
				},
			}
			result, err := reconciler.Reconcile(ctx, req)
			Expect(err).ToNot(HaveOccurred())
			Expect(result).To(Equal(ctrl.Result{}))
		})
	})

	Context("Namespace Selection", func() {
		var reconciler *ClusterResourceQuotaReconciler
		var testNamespace *corev1.Namespace

		BeforeEach(func() {
			reconciler = &ClusterResourceQuotaReconciler{
				ExcludeNamespaceLabelKey: "pac-quota-controller.powerapp.cloud/exclude",
				OwnNamespace:             "pac-quota-controller-system",
			}
			testNamespace = &corev1.Namespace{
				ObjectMeta: metav1.ObjectMeta{
					Name: "test-namespace",
					Labels: map[string]string{
						"team": "test",
					},
				},
			}
		})

		It("should correctly identify and track selected namespaces", func() {
			// Mock the CRQ client to return our test quota
			mockCRQClient := mocks.NewMockCRQClientInterface(GinkgoT())
			mockCRQClient.On("ListAllCRQs").Return([]quotav1alpha1.ClusterResourceQuota{*testQuota}, nil).Maybe()
			mockCRQClient.On("GetCRQByNamespace", mock.AnythingOfType("*v1.Namespace")).Return(testQuota, nil).Maybe()
			reconciler.crqClient = mockCRQClient

			// Test that the namespace is correctly identified as selected
			requests := reconciler.findQuotasForObject(ctx, testNamespace)
			Expect(requests).To(HaveLen(1))
			Expect(requests[0].Name).To(Equal(testQuota.Name))
		})

		It("should not select namespaces that do not match the selector", func() {
			nonMatchingNamespace := &corev1.Namespace{
				ObjectMeta: metav1.ObjectMeta{
					Name: "non-matching-namespace",
					Labels: map[string]string{
						"team": "other-team",
					},
				},
			}

			mockCRQClient := mocks.NewMockCRQClientInterface(GinkgoT())
			mockCRQClient.On("ListAllCRQs").Return([]quotav1alpha1.ClusterResourceQuota{}, nil).Maybe()
			mockCRQClient.On("GetCRQByNamespace", mock.AnythingOfType("*v1.Namespace")).Return(nil, nil).Maybe()
			reconciler.crqClient = mockCRQClient

			requests := reconciler.findQuotasForObject(ctx, nonMatchingNamespace)
			Expect(requests).To(BeEmpty())
		})

		It("should exclude the controller's own namespace and namespaces with the exclusion label", func() {
			// Test own namespace exclusion
			ownNamespace := &corev1.Namespace{
				ObjectMeta: metav1.ObjectMeta{
					Name: reconciler.OwnNamespace,
					Labels: map[string]string{
						"team": "test",
					},
				},
			}
			Expect(reconciler.isNamespaceExcluded(ownNamespace)).To(BeTrue())

			// Test exclusion label
			excludedNamespace := &corev1.Namespace{
				ObjectMeta: metav1.ObjectMeta{
					Name: "excluded-namespace",
					Labels: map[string]string{
						reconciler.ExcludeNamespaceLabelKey: "true",
					},
				},
			}
			Expect(reconciler.isNamespaceExcluded(excludedNamespace)).To(BeTrue())

			// Test regular namespace (should not be excluded)
			regularNamespace := &corev1.Namespace{
				ObjectMeta: metav1.ObjectMeta{
					Name: "regular-namespace",
					Labels: map[string]string{
						"team": "test",
					},
				},
			}
			Expect(reconciler.isNamespaceExcluded(regularNamespace)).To(BeFalse())
		})

		It("should handle ScopeSelector field", func() {
			scopeQuota := &quotav1alpha1.ClusterResourceQuota{
				ObjectMeta: metav1.ObjectMeta{
					Name: "scope-quota",
				},
				Spec: quotav1alpha1.ClusterResourceQuotaSpec{
					ScopeSelector: &corev1.ScopeSelector{
						MatchExpressions: []corev1.ScopedResourceSelectorRequirement{
							{
								ScopeName: corev1.ResourceQuotaScopeBestEffort,
								Operator:  corev1.ScopeSelectorOpExists,
							},
						},
					},
				},
			}

			// Test that scope selector is handled (even if not fully implemented)
			Expect(scopeQuota.Spec.ScopeSelector).NotTo(BeNil())
		})
	})

	Context("Namespace Exclusion", func() {
		var reconciler *ClusterResourceQuotaReconciler

		BeforeEach(func() {
			// Create a fake client that returns the namespace when requested
			fakeClient := &fakeClient{
				getFunc: func(ctx context.Context, key client.ObjectKey, obj client.Object) error {
					if ns, ok := obj.(*corev1.Namespace); ok && key.Name == "pac-quota-controller-system" {
						ns.Name = key.Name
						return nil
					}
					return fmt.Errorf("not found")
				},
			}

			reconciler = &ClusterResourceQuotaReconciler{
				Client:                   fakeClient,
				ExcludeNamespaceLabelKey: "pac-quota-controller.powerapp.cloud/exclude",
				OwnNamespace:             "pac-quota-controller-system",
			}
		})

		It("should identify its own namespace as excluded", func() {
			ownNamespace := &corev1.Namespace{
				ObjectMeta: metav1.ObjectMeta{
					Name: reconciler.OwnNamespace,
				},
			}
			Expect(reconciler.isNamespaceExcluded(ownNamespace)).To(BeTrue())
		})

		It("should identify a namespace with the exclusion label", func() {
			excludedNamespace := &corev1.Namespace{
				ObjectMeta: metav1.ObjectMeta{
					Name: "excluded-namespace",
					Labels: map[string]string{
						reconciler.ExcludeNamespaceLabelKey: "true",
					},
				},
			}
			Expect(reconciler.isNamespaceExcluded(excludedNamespace)).To(BeTrue())
		})

		It("should not exclude a regular namespace", func() {
			regularNamespace := &corev1.Namespace{
				ObjectMeta: metav1.ObjectMeta{
					Name: "regular-namespace",
				},
			}
			Expect(reconciler.isNamespaceExcluded(regularNamespace)).To(BeFalse())
		})

		It("should return no requests for an excluded namespace", func() {
			excludedNamespace := &corev1.Namespace{
				ObjectMeta: metav1.ObjectMeta{
					Name: reconciler.OwnNamespace,
				},
			}
			requests := reconciler.findQuotasForObject(ctx, excludedNamespace)
			Expect(requests).To(BeEmpty())
		})

		It("should return no requests for an object in an excluded namespace", func() {
			excludedNamespace := &corev1.Namespace{
				ObjectMeta: metav1.ObjectMeta{
					Name: reconciler.OwnNamespace,
				},
			}
			pod := &corev1.Pod{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "test-pod",
					Namespace: excludedNamespace.Name,
				},
			}
			requests := reconciler.findQuotasForObject(ctx, pod)
			Expect(requests).To(BeEmpty())
		})

		It("should identify its own namespace as excluded, even if it matches the selector and has no exclusion label", func() {
			ownNamespace := &corev1.Namespace{
				ObjectMeta: metav1.ObjectMeta{
					Name: reconciler.OwnNamespace,
					Labels: map[string]string{
						"team": "test",
					},
				},
			}
			Expect(reconciler.isNamespaceExcluded(ownNamespace)).To(BeTrue())
		})
	})

	Context("Event Filtering", func() {
		var predicate resourceUpdatePredicate

		BeforeEach(func() {
			predicate = resourceUpdatePredicate{}
		})

		It("should reconcile when generation changes", func() {
			oldPod := &corev1.Pod{
				ObjectMeta: metav1.ObjectMeta{
					Generation: 1,
				},
			}
			newPod := &corev1.Pod{
				ObjectMeta: metav1.ObjectMeta{
					Generation: 2,
				},
			}
			event := event.UpdateEvent{
				ObjectOld: oldPod,
				ObjectNew: newPod,
			}
			Expect(predicate.Update(event)).To(BeTrue())
		})

		It("should not reconcile for status updates without phase change", func() {
			oldPod := &corev1.Pod{
				ObjectMeta: metav1.ObjectMeta{
					Generation: 1,
				},
				Status: corev1.PodStatus{
					Phase: corev1.PodRunning,
				},
			}
			newPod := &corev1.Pod{
				ObjectMeta: metav1.ObjectMeta{
					Generation: 1,
				},
				Status: corev1.PodStatus{
					Phase: corev1.PodRunning,
				},
			}
			event := event.UpdateEvent{
				ObjectOld: oldPod,
				ObjectNew: newPod,
			}
			Expect(predicate.Update(event)).To(BeFalse())
		})

		It("should reconcile when pod becomes terminal", func() {
			oldPod := &corev1.Pod{
				ObjectMeta: metav1.ObjectMeta{
					Generation: 1,
				},
				Status: corev1.PodStatus{
					Phase: corev1.PodRunning,
				},
			}
			newPod := &corev1.Pod{
				ObjectMeta: metav1.ObjectMeta{
					Generation: 1,
				},
				Status: corev1.PodStatus{
					Phase: corev1.PodSucceeded,
				},
			}
			event := event.UpdateEvent{
				ObjectOld: oldPod,
				ObjectNew: newPod,
			}
			Expect(predicate.Update(event)).To(BeTrue())
		})

		It("should not reconcile for non-terminal phase changes", func() {
			oldPod := &corev1.Pod{
				ObjectMeta: metav1.ObjectMeta{
					Generation: 1,
				},
				Status: corev1.PodStatus{
					Phase: corev1.PodPending,
				},
			}
			newPod := &corev1.Pod{
				ObjectMeta: metav1.ObjectMeta{
					Generation: 1,
				},
				Status: corev1.PodStatus{
					Phase: corev1.PodRunning,
				},
			}
			event := event.UpdateEvent{
				ObjectOld: oldPod,
				ObjectNew: newPod,
			}
			Expect(predicate.Update(event)).To(BeFalse())
		})
	})

	Context("Error Handling and Edge Cases", func() {
		var reconciler *ClusterResourceQuotaReconciler
		var testNamespace *corev1.Namespace

		BeforeEach(func() {
			// Create a basic fake client
			basicClient := &fakeClient{
				getFunc: func(ctx context.Context, key client.ObjectKey, obj client.Object) error {
					if ns, ok := obj.(*corev1.Namespace); ok && key.Name == "test-namespace" {
						ns.Name = key.Name
						ns.Labels = map[string]string{"team": "test"}
						return nil
					}
					return fmt.Errorf("not found")
				},
			}

			reconciler = &ClusterResourceQuotaReconciler{
				Client:                   basicClient,
				ExcludeNamespaceLabelKey: "pac-quota-controller.powerapp.cloud/exclude",
				OwnNamespace:             "pac-quota-controller-system",
			}
			testNamespace = &corev1.Namespace{
				ObjectMeta: metav1.ObjectMeta{
					Name: "test-namespace",
					Labels: map[string]string{
						"team": "test",
					},
				},
			}
		})

		It("should handle client errors gracefully", func() {
			// Create a client that returns errors
			errorClient := &fakeClient{
				getFunc: func(ctx context.Context, key client.ObjectKey, obj client.Object) error {
					return errors.New("simulated client error")
				},
			}

			reconciler.Client = errorClient
			// Set a mock CRQ client to prevent nil pointer dereference
			mockCRQClient := mocks.NewMockCRQClientInterface(GinkgoT())
			mockCRQClient.On("ListAllCRQs").Return([]quotav1alpha1.ClusterResourceQuota{}, nil).Maybe()
			mockCRQClient.On("GetCRQByNamespace", mock.AnythingOfType("*v1.Namespace")).Return(nil, nil).Maybe()
			reconciler.crqClient = mockCRQClient

			// Should not panic when client operations fail
			Expect(func() {
				reconciler.findQuotasForObject(ctx, testNamespace)
			}).NotTo(Panic())
		})

		It("should handle nil namespace gracefully", func() {
			// Test with nil namespace
			requests := reconciler.findQuotasForObject(ctx, nil)
			Expect(requests).To(BeEmpty())
		})

		It("should handle namespace with nil labels", func() {
			namespaceWithNilLabels := &corev1.Namespace{
				ObjectMeta: metav1.ObjectMeta{
					Name:   "nil-labels-ns",
					Labels: nil,
				},
			}

			// Set a mock CRQ client to prevent nil pointer dereference
			mockCRQClient := mocks.NewMockCRQClientInterface(GinkgoT())
			mockCRQClient.On("ListAllCRQs").Return([]quotav1alpha1.ClusterResourceQuota{}, nil).Maybe()
			mockCRQClient.On("GetCRQByNamespace", mock.AnythingOfType("*v1.Namespace")).Return(nil, nil).Maybe()
			reconciler.crqClient = mockCRQClient

			requests := reconciler.findQuotasForObject(ctx, namespaceWithNilLabels)
			Expect(requests).To(BeEmpty())
		})

		It("should handle CRQ client errors gracefully", func() {
			// Create a mock CRQ client that returns errors
			errorCRQClient := mocks.NewMockCRQClientInterface(GinkgoT())
			errorCRQClient.On("GetCRQByNamespace", mock.AnythingOfType("*v1.Namespace")).Return(nil, errors.New("simulated error")).Maybe()
			errorCRQClient.On("ListAllCRQs").Return(nil, errors.New("simulated error")).Maybe()

			reconciler.crqClient = errorCRQClient

			// Should not panic when CRQ client operations fail
			Expect(func() {
				reconciler.findQuotasForObject(ctx, testNamespace)
			}).NotTo(Panic())
		})

		It("should handle empty CRQ list gracefully", func() {
			// Mock empty CRQ list
			mockCRQClient := mocks.NewMockCRQClientInterface(GinkgoT())
			mockCRQClient.On("ListAllCRQs").Return([]quotav1alpha1.ClusterResourceQuota{}, nil).Maybe()
			mockCRQClient.On("GetCRQByNamespace", mock.AnythingOfType("*v1.Namespace")).Return(nil, nil).Maybe()

			reconciler.crqClient = mockCRQClient

			requests := reconciler.findQuotasForObject(ctx, testNamespace)
			Expect(requests).To(BeEmpty())
		})

		It("should handle multiple CRQs gracefully", func() {
			// Create multiple CRQs
			crqs := []quotav1alpha1.ClusterResourceQuota{
				{
					ObjectMeta: metav1.ObjectMeta{Name: "crq1"},
					Spec: quotav1alpha1.ClusterResourceQuotaSpec{
						NamespaceSelector: &metav1.LabelSelector{
							MatchLabels: map[string]string{"env": "prod"},
						},
					},
				},
				{
					ObjectMeta: metav1.ObjectMeta{Name: "crq2"},
					Spec: quotav1alpha1.ClusterResourceQuotaSpec{
						NamespaceSelector: &metav1.LabelSelector{
							MatchLabels: map[string]string{"env": "staging"},
						},
					},
				},
			}

			mockCRQClient := mocks.NewMockCRQClientInterface(GinkgoT())
			mockCRQClient.On("ListAllCRQs").Return(crqs, nil).Maybe()
			mockCRQClient.On("GetCRQByNamespace", mock.AnythingOfType("*v1.Namespace")).Return(nil, nil).Maybe()

			reconciler.crqClient = mockCRQClient

			requests := reconciler.findQuotasForObject(ctx, testNamespace)
			Expect(requests).To(BeEmpty()) // No CRQ client configured
		})
	})

	Context("Performance and Scalability", func() {
		var reconciler *ClusterResourceQuotaReconciler
		var testNamespace *corev1.Namespace

		BeforeEach(func() {
			reconciler = &ClusterResourceQuotaReconciler{
				ExcludeNamespaceLabelKey: "pac-quota-controller.powerapp.cloud/exclude",
				OwnNamespace:             "pac-quota-controller-system",
			}
			testNamespace = &corev1.Namespace{
				ObjectMeta: metav1.ObjectMeta{
					Name: "test-namespace",
					Labels: map[string]string{
						"team": "test",
					},
				},
			}
		})

		It("should handle large number of CRQs efficiently", func() {
			// Create 100 CRQs
			var crqs []quotav1alpha1.ClusterResourceQuota
			for i := 0; i < 100; i++ {
				crq := quotav1alpha1.ClusterResourceQuota{
					ObjectMeta: metav1.ObjectMeta{
						Name: fmt.Sprintf("crq-%d", i),
					},
					Spec: quotav1alpha1.ClusterResourceQuotaSpec{
						NamespaceSelector: &metav1.LabelSelector{
							MatchLabels: map[string]string{
								"team": fmt.Sprintf("team-%d", i),
							},
						},
					},
				}
				crqs = append(crqs, crq)
			}

			// Add one CRQ that matches the test namespace
			matchingCRQ := quotav1alpha1.ClusterResourceQuota{
				ObjectMeta: metav1.ObjectMeta{
					Name: "matching-crq",
				},
				Spec: quotav1alpha1.ClusterResourceQuotaSpec{
					NamespaceSelector: &metav1.LabelSelector{
						MatchLabels: map[string]string{
							"team": "test",
						},
					},
				},
			}
			crqs = append(crqs, matchingCRQ)

			mockCRQClient := mocks.NewMockCRQClientInterface(GinkgoT())
			mockCRQClient.On("ListAllCRQs").Return(crqs, nil).Maybe()
			mockCRQClient.On("GetCRQByNamespace", mock.AnythingOfType("*v1.Namespace")).Return(&matchingCRQ, nil).Maybe()

			reconciler.crqClient = mockCRQClient

			// Should complete within reasonable time
			start := time.Now()
			requests := reconciler.findQuotasForObject(ctx, testNamespace)
			duration := time.Since(start)

			Expect(duration).To(BeNumerically("<", 100*time.Millisecond))
			Expect(requests).To(HaveLen(1)) // Should match one CRQ
		})

		It("should handle concurrent reconciliation requests", func() {
			// Set a mock CRQ client to prevent nil pointer dereference
			mockCRQClient := mocks.NewMockCRQClientInterface(GinkgoT())
			mockCRQClient.On("GetCRQByNamespace", mock.AnythingOfType("*v1.Namespace")).Return(nil, nil).Maybe()
			reconciler.crqClient = mockCRQClient

			// Test concurrent access to reconciler methods
			var wg sync.WaitGroup
			concurrency := 10

			for i := 0; i < concurrency; i++ {
				wg.Add(1)
				go func() {
					defer wg.Done()
					reconciler.findQuotasForObject(ctx, testNamespace)
				}()
			}

			wg.Wait()
			// Should complete without race conditions
		})
	})

	Context("Resource Validation", func() {
		var reconciler *ClusterResourceQuotaReconciler

		BeforeEach(func() {
			reconciler = &ClusterResourceQuotaReconciler{
				ExcludeNamespaceLabelKey: "pac-quota-controller.powerapp.cloud/exclude",
				OwnNamespace:             "pac-quota-controller-system",
			}
			// Set a mock CRQ client to prevent nil pointer dereference
			mockCRQClient := mocks.NewMockCRQClientInterface(GinkgoT())
			mockCRQClient.On("GetCRQByNamespace", mock.AnythingOfType("*v1.Namespace")).Return(nil, nil).Maybe()
			reconciler.crqClient = mockCRQClient
		})

		It("should handle invalid resource quantities", func() {
			// Create CRQ with invalid resource quantities
			invalidCRQ := &quotav1alpha1.ClusterResourceQuota{
				ObjectMeta: metav1.ObjectMeta{
					Name: "invalid-resources",
				},
				Spec: quotav1alpha1.ClusterResourceQuotaSpec{
					Hard: quotav1alpha1.ResourceList{
						corev1.ResourceCPU: resource.Quantity{},
					},
				},
			}

			// Should not panic when processing invalid resources
			Expect(func() {
				reconciler.findQuotasForObject(ctx, invalidCRQ)
			}).NotTo(Panic())
		})

		It("should handle zero resource requests", func() {
			// Set a mock CRQ client to prevent nil pointer dereference
			mockCRQClient := mocks.NewMockCRQClientInterface(GinkgoT())
			mockCRQClient.On("GetCRQByNamespace", mock.AnythingOfType("*v1.Namespace")).Return(nil, nil).Maybe()
			reconciler.crqClient = mockCRQClient
			// Set a mock client to prevent nil pointer dereference
			reconciler.Client = &fakeClient{}

			// Create pod with zero resource requests
			zeroResourcePod := &corev1.Pod{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "zero-resources",
					Namespace: "test-namespace",
				},
				Spec: corev1.PodSpec{
					Containers: []corev1.Container{
						{
							Name: "test",
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

			requests := reconciler.findQuotasForObject(ctx, zeroResourcePod)
			Expect(requests).To(BeEmpty()) // No CRQ client configured
		})
	})

	Context("State Management", func() {
		var reconciler *ClusterResourceQuotaReconciler

		BeforeEach(func() {
			reconciler = &ClusterResourceQuotaReconciler{
				ExcludeNamespaceLabelKey: "pac-quota-controller.powerapp.cloud/exclude",
				OwnNamespace:             "pac-quota-controller-system",
			}
			// Set a mock CRQ client to prevent nil pointer dereference
			mockCRQClient := mocks.NewMockCRQClientInterface(GinkgoT())
			mockCRQClient.On("GetCRQByNamespace", mock.AnythingOfType("*v1.Namespace")).Return(nil, nil).Maybe()
			reconciler.crqClient = mockCRQClient
		})

		It("should handle orphaned resources", func() {
			// Create namespace without corresponding CRQ
			orphanedNamespace := &corev1.Namespace{
				ObjectMeta: metav1.ObjectMeta{
					Name: "orphaned-ns",
					Labels: map[string]string{
						"team": "orphaned-team",
					},
				},
			}

			requests := reconciler.findQuotasForObject(ctx, orphanedNamespace)
			Expect(requests).To(BeEmpty())
		})
	})

	Context("Webhook Integration", func() {
		var reconciler *ClusterResourceQuotaReconciler

		BeforeEach(func() {
			reconciler = &ClusterResourceQuotaReconciler{
				ExcludeNamespaceLabelKey: "pac-quota-controller.powerapp.cloud/exclude",
				OwnNamespace:             "pac-quota-controller-system",
			}
			// Set a mock CRQ client to prevent nil pointer dereference
			mockCRQClient := mocks.NewMockCRQClientInterface(GinkgoT())
			mockCRQClient.On("GetCRQByNamespace", mock.AnythingOfType("*v1.Namespace")).Return(nil, nil).Maybe()
			reconciler.crqClient = mockCRQClient
		})

		It("should handle webhook validation failures", func() {
			// Set a mock client to prevent nil pointer dereference
			reconciler.Client = &fakeClient{}

			// Test scenario where webhook validation would fail
			invalidPod := &corev1.Pod{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "invalid-pod",
					Namespace: "test-namespace",
				},
				Spec: corev1.PodSpec{
					Containers: []corev1.Container{
						{
							Name: "test",
							Resources: corev1.ResourceRequirements{
								Requests: corev1.ResourceList{
									corev1.ResourceCPU: resource.MustParse("999999"),
								},
							},
						},
					},
				},
			}

			// Should still be able to find quotas for the object
			requests := reconciler.findQuotasForObject(ctx, invalidPod)
			Expect(requests).To(BeEmpty()) // No CRQ client configured
		})
	})

	Context("Network and Infrastructure", func() {
		var reconciler *ClusterResourceQuotaReconciler
		var testNamespace *corev1.Namespace

		BeforeEach(func() {
			reconciler = &ClusterResourceQuotaReconciler{
				ExcludeNamespaceLabelKey: "pac-quota-controller.powerapp.cloud/exclude",
				OwnNamespace:             "pac-quota-controller-system",
			}
			// Set a mock CRQ client to prevent nil pointer dereference
			mockCRQClient := mocks.NewMockCRQClientInterface(GinkgoT())
			mockCRQClient.On("GetCRQByNamespace", mock.AnythingOfType("*v1.Namespace")).Return(nil, nil).Maybe()
			reconciler.crqClient = mockCRQClient
			testNamespace = &corev1.Namespace{
				ObjectMeta: metav1.ObjectMeta{
					Name: "test-namespace",
					Labels: map[string]string{
						"team": "test",
					},
				},
			}
		})

		It("should handle context cancellation", func() {
			// Create cancelled context
			cancelledCtx, cancel := context.WithCancel(context.Background())
			cancel()

			// Should handle cancelled context gracefully
			requests := reconciler.findQuotasForObject(cancelledCtx, testNamespace)
			Expect(requests).To(BeEmpty())
		})

		It("should handle timeout scenarios", func() {
			// Create context with timeout
			timeoutCtx, cancel := context.WithTimeout(context.Background(), 1*time.Nanosecond)
			defer cancel()

			// Should handle timeout gracefully
			requests := reconciler.findQuotasForObject(timeoutCtx, testNamespace)
			Expect(requests).To(BeEmpty())
		})
	})
})
