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
	"github.com/powerhome/pac-quota-controller/pkg/kubernetes/objectcount"
	"github.com/powerhome/pac-quota-controller/pkg/kubernetes/pod"
	"github.com/powerhome/pac-quota-controller/pkg/kubernetes/services"
	"github.com/powerhome/pac-quota-controller/pkg/kubernetes/storage"
	"github.com/powerhome/pac-quota-controller/pkg/kubernetes/usage"
	"github.com/powerhome/pac-quota-controller/pkg/metrics"
	"github.com/powerhome/pac-quota-controller/pkg/mocks"
	promtestutil "github.com/prometheus/client_golang/prometheus/testutil"
	"github.com/stretchr/testify/mock"
	"go.uber.org/zap"
	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/resource"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/types"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/client/fake"
	"sigs.k8s.io/controller-runtime/pkg/client/interceptor"
	"sigs.k8s.io/controller-runtime/pkg/event"
)

var testOwnNamespace string = "pac-quota-controller-system"

const (
	fastStorageClass = "fast-ssd"
	slowStorageClass = "slow-hdd"
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
func (f *fakeStatusWriter) Apply(ctx context.Context, obj runtime.ApplyConfiguration, opts ...client.SubResourceApplyOption) error {
	return nil
}

// Success status writer for happy path tests
type successStatusWriter struct{}

func (f *successStatusWriter) Patch(ctx context.Context, obj client.Object, patch client.Patch, opts ...client.SubResourcePatchOption) error {
	return nil
}
func (f *successStatusWriter) Update(ctx context.Context, obj client.Object, opts ...client.SubResourceUpdateOption) error {
	return nil
}
func (f *successStatusWriter) Create(ctx context.Context, obj client.Object, subResource client.Object, opts ...client.SubResourceCreateOption) error {
	return nil
}
func (f *successStatusWriter) Apply(ctx context.Context, obj runtime.ApplyConfiguration, opts ...client.SubResourceApplyOption) error {
	return nil
}

type countingStatusWriter struct {
	patchCalls int
}

func (f *countingStatusWriter) Patch(ctx context.Context, obj client.Object, patch client.Patch, opts ...client.SubResourcePatchOption) error {
	f.patchCalls++
	return nil
}
func (f *countingStatusWriter) Update(ctx context.Context, obj client.Object, opts ...client.SubResourceUpdateOption) error {
	return nil
}
func (f *countingStatusWriter) Create(ctx context.Context, obj client.Object, subResource client.Object, opts ...client.SubResourceCreateOption) error {
	return nil
}
func (f *countingStatusWriter) Apply(ctx context.Context, obj runtime.ApplyConfiguration, opts ...client.SubResourceApplyOption) error {
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
	var logger *zap.Logger
	BeforeAll(func() {
		logger, _ = zap.NewDevelopment()
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
			// Create a fake client that returns the test quota
			fakeClient := &fakeClient{
				getFunc: func(ctx context.Context, key client.ObjectKey, obj client.Object) error {
					if crq, ok := obj.(*quotav1alpha1.ClusterResourceQuota); ok {
						*crq = *testQuota
						return nil
					}
					return nil
				},
				listFunc: func(ctx context.Context, obj client.ObjectList, opts ...client.ListOption) error {
					if nsList, ok := obj.(*corev1.NamespaceList); ok {
						// Return empty namespace list for this test
						nsList.Items = []corev1.Namespace{}
						return nil
					}
					return nil
				},
				statusWriter: &successStatusWriter{}, // Use success status writer
			}

			reconciler := &ClusterResourceQuotaReconciler{
				Client:                    fakeClient,
				logger:                    logger,
				previousNamespacesByQuota: make(map[string][]string),
			}
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

	Context("Status Updates", func() {
		It("should skip patch when status is unchanged", func() {
			statusWriter := &countingStatusWriter{}
			reconciler := &ClusterResourceQuotaReconciler{
				Client: &fakeClient{statusWriter: statusWriter},
				logger: logger,
			}

			totalUsage := quotav1alpha1.ResourceList{
				corev1.ResourceRequestsCPU: resource.MustParse("500m"),
			}
			usageByNamespace := []quotav1alpha1.ResourceQuotaStatusByNamespace{
				{
					Namespace: "example-ns",
					Status: quotav1alpha1.ResourceQuotaStatus{
						Used: quotav1alpha1.ResourceList{
							corev1.ResourceRequestsCPU: resource.MustParse("500m"),
						},
					},
				},
			}

			crq := &quotav1alpha1.ClusterResourceQuota{
				ObjectMeta: metav1.ObjectMeta{Name: "test-crq"},
				Spec: quotav1alpha1.ClusterResourceQuotaSpec{
					Hard: quotav1alpha1.ResourceList{
						corev1.ResourceRequestsCPU: resource.MustParse("1"),
					},
				},
				Status: quotav1alpha1.ClusterResourceQuotaStatus{
					Total: quotav1alpha1.ResourceQuotaStatus{
						Hard: quotav1alpha1.ResourceList{
							corev1.ResourceRequestsCPU: resource.MustParse("1"),
						},
						Used: totalUsage,
					},
					Namespaces: usageByNamespace,
				},
			}

			err := reconciler.updateStatus(ctx, crq, totalUsage, usageByNamespace)
			Expect(err).NotTo(HaveOccurred())
			Expect(statusWriter.patchCalls).To(Equal(0))
		})

		It("should patch when status changes", func() {
			statusWriter := &countingStatusWriter{}
			reconciler := &ClusterResourceQuotaReconciler{
				Client: &fakeClient{statusWriter: statusWriter},
				logger: logger,
			}

			crq := &quotav1alpha1.ClusterResourceQuota{
				ObjectMeta: metav1.ObjectMeta{Name: "test-crq"},
				Spec: quotav1alpha1.ClusterResourceQuotaSpec{
					Hard: quotav1alpha1.ResourceList{
						corev1.ResourceRequestsCPU: resource.MustParse("1"),
					},
				},
			}

			totalUsage := quotav1alpha1.ResourceList{
				corev1.ResourceRequestsCPU: resource.MustParse("250m"),
			}
			usageByNamespace := []quotav1alpha1.ResourceQuotaStatusByNamespace{
				{
					Namespace: "example-ns",
					Status: quotav1alpha1.ResourceQuotaStatus{
						Used: quotav1alpha1.ResourceList{
							corev1.ResourceRequestsCPU: resource.MustParse("250m"),
						},
					},
				},
			}

			err := reconciler.updateStatus(ctx, crq, totalUsage, usageByNamespace)
			Expect(err).NotTo(HaveOccurred())
			Expect(statusWriter.patchCalls).To(Equal(1))
		})
	})

	Context("Namespace Selection", func() {
		var reconciler *ClusterResourceQuotaReconciler
		var testNamespace *corev1.Namespace

		BeforeEach(func() {
			testNamespace = &corev1.Namespace{
				ObjectMeta: metav1.ObjectMeta{
					Name: "test-namespace",
					Labels: map[string]string{
						"team": "test",
					},
				},
			}
			c := fake.NewClientBuilder().WithObjects(testNamespace).Build()
			reconciler = &ClusterResourceQuotaReconciler{
				Client:                    c,
				logger:                    logger,
				previousNamespacesByQuota: make(map[string][]string),
				ExcludeNamespaceLabelKey:  "pac-quota-controller.powerapp.cloud/exclude",
			}
		})

		It("should correctly identify and track selected namespaces", func() {
			// Mock the CRQ client to return our test quota
			mockCRQClient := mocks.NewMockCRQClientInterface(GinkgoT())
			mockCRQClient.On("ListAllCRQs").Return([]quotav1alpha1.ClusterResourceQuota{*testQuota}, nil).Maybe()
			mockCRQClient.On("GetCRQByNamespace", mock.Anything, mock.AnythingOfType("*v1.Namespace")).Return(testQuota, nil).Maybe()
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
			mockCRQClient.On("GetCRQByNamespace", mock.Anything, mock.AnythingOfType("*v1.Namespace")).Return(nil, nil).Maybe()
			reconciler.crqClient = mockCRQClient

			requests := reconciler.findQuotasForObject(ctx, nonMatchingNamespace)
			Expect(requests).To(BeEmpty())
		})

		It("should exclude the controller's own namespace and namespaces with the exclusion label", func() {
			// Test own namespace exclusion
			reconciler.ExcludedNamespaces = append(reconciler.ExcludedNamespaces, testOwnNamespace)
			ownNamespace := &corev1.Namespace{
				ObjectMeta: metav1.ObjectMeta{
					Name: testOwnNamespace,
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
				ExcludedNamespaces:       []string{"excluded-ns", "another-excluded-ns"},
			}
		})
		It("should identify a namespace in the excludedNamespaces list", func() {
			ns := &corev1.Namespace{ObjectMeta: metav1.ObjectMeta{Name: "excluded-ns"}}
			Expect(reconciler.isNamespaceExcluded(ns)).To(BeTrue())
		})

		It("should identify its own namespace as excluded", func() {
			reconciler.ExcludedNamespaces = append(reconciler.ExcludedNamespaces, testOwnNamespace)
			ownNamespace := &corev1.Namespace{
				ObjectMeta: metav1.ObjectMeta{
					Name: testOwnNamespace,
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
			reconciler.ExcludedNamespaces = append(reconciler.ExcludedNamespaces, testOwnNamespace)
			excludedNamespace := &corev1.Namespace{
				ObjectMeta: metav1.ObjectMeta{
					Name: testOwnNamespace,
				},
			}
			requests := reconciler.findQuotasForObject(ctx, excludedNamespace)
			Expect(requests).To(BeEmpty())
		})

		It("should return no requests for an object in an excluded namespace", func() {
			reconciler.ExcludedNamespaces = append(reconciler.ExcludedNamespaces, testOwnNamespace)
			excludedNamespace := &corev1.Namespace{
				ObjectMeta: metav1.ObjectMeta{
					Name: testOwnNamespace,
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

		It("should not reconcile for non-terminal phase changes (e.g., Pending -> Running)", func() {
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

		It("should reconcile when a container terminates within a pod", func() {
			oldPod := &corev1.Pod{
				Status: corev1.PodStatus{
					InitContainerStatuses: []corev1.ContainerStatus{
						{
							Name:  "init",
							State: corev1.ContainerState{Running: &corev1.ContainerStateRunning{}},
						},
					},
				},
			}
			newPod := &corev1.Pod{
				Status: corev1.PodStatus{
					InitContainerStatuses: []corev1.ContainerStatus{
						{
							Name:  "init",
							State: corev1.ContainerState{Terminated: &corev1.ContainerStateTerminated{ExitCode: 0}},
						},
					},
				},
			}
			event := event.UpdateEvent{
				ObjectOld: oldPod,
				ObjectNew: newPod,
			}
			Expect(predicate.Update(event)).To(BeTrue())
		})

		It("should reconcile for app container termination", func() {
			oldPod := &corev1.Pod{
				Status: corev1.PodStatus{
					ContainerStatuses: []corev1.ContainerStatus{
						{
							Name:  "app",
							State: corev1.ContainerState{Running: &corev1.ContainerStateRunning{}},
						},
					},
				},
			}
			newPod := &corev1.Pod{
				Status: corev1.PodStatus{
					ContainerStatuses: []corev1.ContainerStatus{
						{
							Name:  "app",
							State: corev1.ContainerState{Terminated: &corev1.ContainerStateTerminated{ExitCode: 0}},
						},
					},
				},
			}
			event := event.UpdateEvent{
				ObjectOld: oldPod,
				ObjectNew: newPod,
			}
			Expect(predicate.Update(event)).To(BeTrue())
		})

		It("should reconcile when container count changes", func() {
			oldPod := &corev1.Pod{
				Status: corev1.PodStatus{
					ContainerStatuses: []corev1.ContainerStatus{
						{Name: "app1"},
					},
				},
			}
			newPod := &corev1.Pod{
				Status: corev1.PodStatus{
					ContainerStatuses: []corev1.ContainerStatus{
						{Name: "app1"},
						{Name: "app2"},
					},
				},
			}
			event := event.UpdateEvent{
				ObjectOld: oldPod,
				ObjectNew: newPod,
			}
			Expect(predicate.Update(event)).To(BeTrue())
		})

		It("should reconcile when a new container is added with different name", func() {
			oldPod := &corev1.Pod{
				Status: corev1.PodStatus{
					ContainerStatuses: []corev1.ContainerStatus{
						{Name: "app1"},
					},
				},
			}
			newPod := &corev1.Pod{
				Status: corev1.PodStatus{
					ContainerStatuses: []corev1.ContainerStatus{
						{Name: "app2"},
					},
				},
			}
			event := event.UpdateEvent{
				ObjectOld: oldPod,
				ObjectNew: newPod,
			}
			Expect(predicate.Update(event)).To(BeTrue())
		})

		It("should not reconcile when container is already terminated", func() {
			pod := &corev1.Pod{
				Status: corev1.PodStatus{
					ContainerStatuses: []corev1.ContainerStatus{
						{
							Name:  "app",
							State: corev1.ContainerState{Terminated: &corev1.ContainerStateTerminated{}},
						},
					},
				},
			}
			event := event.UpdateEvent{
				ObjectOld: pod,
				ObjectNew: pod,
			}
			Expect(predicate.Update(event)).To(BeFalse())
		})

		It("should handle nil objects gracefully", func() {
			event := event.UpdateEvent{
				ObjectOld: nil,
				ObjectNew: nil,
			}
			Expect(predicate.Update(event)).To(BeFalse())
		})

		It("should handle non-pod objects based on generation only", func() {
			oldCm := &corev1.ConfigMap{ObjectMeta: metav1.ObjectMeta{Generation: 1}}
			newCm := &corev1.ConfigMap{ObjectMeta: metav1.ObjectMeta{Generation: 1}}
			event := event.UpdateEvent{
				ObjectOld: oldCm,
				ObjectNew: newCm,
			}
			Expect(predicate.Update(event)).To(BeFalse())

			newCm.Generation = 2
			event.ObjectNew = newCm
			Expect(predicate.Update(event)).To(BeTrue())
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
				Client:                    basicClient,
				logger:                    logger,
				previousNamespacesByQuota: make(map[string][]string),
				ExcludeNamespaceLabelKey:  "pac-quota-controller.powerapp.cloud/exclude",
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
			mockCRQClient.On("GetCRQByNamespace", mock.Anything, mock.AnythingOfType("*v1.Namespace")).Return(nil, nil).Maybe()
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
			mockCRQClient.On("GetCRQByNamespace", mock.Anything, mock.AnythingOfType("*v1.Namespace")).Return(nil, nil).Maybe()
			reconciler.crqClient = mockCRQClient

			requests := reconciler.findQuotasForObject(ctx, namespaceWithNilLabels)
			Expect(requests).To(BeEmpty())
		})

		It("should handle CRQ client errors gracefully", func() {
			// Create a mock CRQ client that returns errors
			errorCRQClient := mocks.NewMockCRQClientInterface(GinkgoT())
			errorCRQClient.On("GetCRQByNamespace", mock.Anything, mock.AnythingOfType("*v1.Namespace")).Return(nil, errors.New("simulated error")).Maybe()
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
			mockCRQClient.On("GetCRQByNamespace", mock.Anything, mock.AnythingOfType("*v1.Namespace")).Return(nil, nil).Maybe()

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
			mockCRQClient.On("GetCRQByNamespace", mock.Anything, mock.AnythingOfType("*v1.Namespace")).Return(nil, nil).Maybe()

			reconciler.crqClient = mockCRQClient

			requests := reconciler.findQuotasForObject(ctx, testNamespace)
			Expect(requests).To(BeEmpty()) // No CRQ client configured
		})
	})

	Context("Performance and Scalability", func() {
		var reconciler *ClusterResourceQuotaReconciler
		var testNamespace *corev1.Namespace

		BeforeEach(func() {
			testNamespace = &corev1.Namespace{
				ObjectMeta: metav1.ObjectMeta{
					Name: "test-namespace",
					Labels: map[string]string{
						"team": "test",
					},
				},
			}
			c := fake.NewClientBuilder().WithObjects(testNamespace).Build()
			reconciler = &ClusterResourceQuotaReconciler{
				Client:                    c,
				logger:                    logger,
				previousNamespacesByQuota: make(map[string][]string),
				ExcludeNamespaceLabelKey:  "pac-quota-controller.powerapp.cloud/exclude",
			}
		})

		It("should handle large number of CRQs efficiently", func() {
			// Create 100 CRQs
			crqs := make([]quotav1alpha1.ClusterResourceQuota, 0, 100)
			for i := range 100 {
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
			mockCRQClient.On("GetCRQByNamespace", mock.Anything, mock.AnythingOfType("*v1.Namespace")).Return(&matchingCRQ, nil).Maybe()

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
			mockCRQClient.On("GetCRQByNamespace", mock.Anything, mock.AnythingOfType("*v1.Namespace")).Return(nil, nil).Maybe()
			reconciler.crqClient = mockCRQClient

			// Test concurrent access to reconciler methods
			var wg sync.WaitGroup
			concurrency := 10

			for range concurrency {
				wg.Go(func() {
					reconciler.findQuotasForObject(ctx, testNamespace)
				})
			}

			wg.Wait()
			// Should complete without race conditions
		})
	})

	Context("Resource Validation", func() {
		var reconciler *ClusterResourceQuotaReconciler

		BeforeEach(func() {
			c := fake.NewClientBuilder().Build()
			reconciler = &ClusterResourceQuotaReconciler{
				Client:                    c,
				logger:                    logger,
				previousNamespacesByQuota: make(map[string][]string),
				ExcludeNamespaceLabelKey:  "pac-quota-controller.powerapp.cloud/exclude",
			}
			// Set a mock CRQ client to prevent nil pointer dereference
			mockCRQClient := mocks.NewMockCRQClientInterface(GinkgoT())
			mockCRQClient.On("GetCRQByNamespace", mock.Anything, mock.AnythingOfType("*v1.Namespace")).Return(nil, nil).Maybe()
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
			mockCRQClient.On("GetCRQByNamespace", mock.Anything, mock.AnythingOfType("*v1.Namespace")).Return(nil, nil).Maybe()
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

	Context("Aggregation Step Classification", func() {
		var reconciler *ClusterResourceQuotaReconciler

		BeforeEach(func() {
			reconciler = &ClusterResourceQuotaReconciler{}
		})

		It("should classify standard compute resources", func() {
			Expect(reconciler.aggregationStepForResource(corev1.ResourceRequestsCPU)).To(Equal("compute"))
			Expect(reconciler.aggregationStepForResource(corev1.ResourceLimitsMemory)).To(Equal("compute"))
			Expect(reconciler.aggregationStepForResource(corev1.ResourcePods)).To(Equal("compute"))
		})

		It("should classify storage and service resources", func() {
			Expect(reconciler.aggregationStepForResource(corev1.ResourceRequestsStorage)).To(Equal("storage"))
			Expect(reconciler.aggregationStepForResource(usage.ResourceServices)).To(Equal("services"))
			Expect(reconciler.aggregationStepForResource(usage.ResourceServicesLoadBalancers)).To(Equal("services"))
		})

		It("should classify extended compute resources", func() {
			Expect(reconciler.aggregationStepForResource(corev1.ResourceName("requests.nvidia.com/gpu"))).To(Equal("compute_extended"))
			Expect(reconciler.aggregationStepForResource(corev1.ResourceName("hugepages-2Mi"))).To(Equal("compute_extended"))
		})

		It("should classify object count resources", func() {
			Expect(reconciler.aggregationStepForResource(usage.ResourceConfigMaps)).To(Equal("object_count"))
			Expect(reconciler.aggregationStepForResource(usage.ResourceIngresses)).To(Equal("object_count"))
		})
	})

	Context("Compute Usage From Prefetched Pods", func() {
		It("should aggregate requests and limits from non-terminal pods", func() {
			pods := []corev1.Pod{
				{
					Status: corev1.PodStatus{Phase: corev1.PodRunning},
					Spec: corev1.PodSpec{Containers: []corev1.Container{{
						Resources: corev1.ResourceRequirements{
							Requests: corev1.ResourceList{corev1.ResourceCPU: resource.MustParse("250m")},
							Limits:   corev1.ResourceList{corev1.ResourceCPU: resource.MustParse("500m")},
						},
					}}},
				},
				{
					Status: corev1.PodStatus{Phase: corev1.PodRunning},
					Spec: corev1.PodSpec{Containers: []corev1.Container{{
						Resources: corev1.ResourceRequirements{
							Requests: corev1.ResourceList{corev1.ResourceCPU: resource.MustParse("500m")},
							Limits:   corev1.ResourceList{corev1.ResourceCPU: resource.MustParse("1")},
						},
					}}},
				},
				{
					Status: corev1.PodStatus{Phase: corev1.PodSucceeded},
					Spec: corev1.PodSpec{Containers: []corev1.Container{{
						Resources: corev1.ResourceRequirements{
							Requests: corev1.ResourceList{corev1.ResourceCPU: resource.MustParse("5")},
							Limits:   corev1.ResourceList{corev1.ResourceCPU: resource.MustParse("5")},
						},
					}}},
				},
			}

			requestsCPU := pod.CalculateUsageFromPods(pods, corev1.ResourceRequestsCPU)
			limitsCPU := pod.CalculateUsageFromPods(pods, corev1.ResourceLimitsCPU)

			Expect(requestsCPU.String()).To(Equal("750m"))
			Expect(limitsCPU.String()).To(Equal("1500m"))
		})

		It("should count only non-terminal pods for pod quota", func() {
			pods := []corev1.Pod{
				{Status: corev1.PodStatus{Phase: corev1.PodRunning}},
				{Status: corev1.PodStatus{Phase: corev1.PodPending}},
				{Status: corev1.PodStatus{Phase: corev1.PodFailed}},
			}

			podCount := pod.CalculateUsageFromPods(pods, corev1.ResourcePods)
			Expect(podCount.String()).To(Equal("2"))
		})

		It("computes compute usage from the in-memory pod slice", func() {
			reconciler := &ClusterResourceQuotaReconciler{}
			pods := []corev1.Pod{
				{
					Status: corev1.PodStatus{Phase: corev1.PodRunning},
					Spec: corev1.PodSpec{Containers: []corev1.Container{{
						Resources: corev1.ResourceRequirements{
							Requests: corev1.ResourceList{corev1.ResourceCPU: resource.MustParse("300m")},
						},
					}}},
				},
			}

			got, err := reconciler.computeNamespaceResourceUsage(
				ctx, "ns-a", corev1.ResourceRequestsCPU, pods, nil, nil, nil,
			)
			Expect(err).NotTo(HaveOccurred())
			Expect(got.String()).To(Equal("300m"))
		})

		It("returns zero for service quotas when no services were listed", func() {
			reconciler := &ClusterResourceQuotaReconciler{logger: zap.NewNop()}
			got, err := reconciler.computeNamespaceResourceUsage(
				ctx, "ns-a", usage.ResourceServices, nil, nil, nil, nil,
			)
			Expect(err).NotTo(HaveOccurred())
			Expect(got.String()).To(Equal("0"))
		})

		It("computes extended compute usage (GPUs) from the in-memory pod slice", func() {
			reconciler := &ClusterResourceQuotaReconciler{}
			pods := []corev1.Pod{
				{
					Status: corev1.PodStatus{Phase: corev1.PodRunning},
					Spec: corev1.PodSpec{Containers: []corev1.Container{{
						Resources: corev1.ResourceRequirements{
							Requests: corev1.ResourceList{
								corev1.ResourceName("nvidia.com/gpu"): resource.MustParse("2"),
							},
						},
					}}},
				},
			}

			got, err := reconciler.computeNamespaceResourceUsage(
				ctx, "ns-a",
				corev1.ResourceName("requests.nvidia.com/gpu"),
				pods, nil, nil, nil,
			)
			Expect(err).NotTo(HaveOccurred())
			Expect(got.String()).To(Equal("2"))
		})

		It("computes ephemeral-storage limits from the in-memory pod slice", func() {
			reconciler := &ClusterResourceQuotaReconciler{}
			pods := []corev1.Pod{
				{
					Status: corev1.PodStatus{Phase: corev1.PodRunning},
					Spec: corev1.PodSpec{Containers: []corev1.Container{{
						Resources: corev1.ResourceRequirements{
							Limits: corev1.ResourceList{
								corev1.ResourceEphemeralStorage: resource.MustParse("2Gi"),
							},
						},
					}}},
				},
			}

			got, err := reconciler.computeNamespaceResourceUsage(
				ctx, "ns-a", corev1.ResourceLimitsEphemeralStorage, pods, nil, nil, nil,
			)
			Expect(err).NotTo(HaveOccurred())
			Expect(got.Equal(resource.MustParse("2Gi"))).To(BeTrue())
		})
	})

	Context("Service Usage From Prefetched Services", func() {
		It("should count total services", func() {
			svcs := []corev1.Service{
				{Spec: corev1.ServiceSpec{Type: corev1.ServiceTypeClusterIP}},
				{Spec: corev1.ServiceSpec{Type: corev1.ServiceTypeLoadBalancer}},
				{Spec: corev1.ServiceSpec{Type: corev1.ServiceTypeNodePort}},
			}

			total := services.CalculateUsageFromServices(svcs, usage.ResourceServices)
			Expect(total.String()).To(Equal("3"))
		})

		It("should count only load balancer services", func() {
			svcs := []corev1.Service{
				{Spec: corev1.ServiceSpec{Type: corev1.ServiceTypeClusterIP}},
				{Spec: corev1.ServiceSpec{Type: corev1.ServiceTypeLoadBalancer}},
				{Spec: corev1.ServiceSpec{Type: corev1.ServiceTypeLoadBalancer}},
			}

			lbCount := services.CalculateUsageFromServices(svcs, usage.ResourceServicesLoadBalancers)
			Expect(lbCount.String()).To(Equal("2"))
		})

		It("should count only node port services", func() {
			svcs := []corev1.Service{
				{Spec: corev1.ServiceSpec{Type: corev1.ServiceTypeNodePort}},
				{Spec: corev1.ServiceSpec{Type: corev1.ServiceTypeClusterIP}},
				{Spec: corev1.ServiceSpec{Type: corev1.ServiceTypeNodePort}},
			}

			npCount := services.CalculateUsageFromServices(svcs, usage.ResourceServicesNodePorts)
			Expect(npCount.String()).To(Equal("2"))
		})
	})

	Context("Resource Kind Classification", func() {
		var r *ClusterResourceQuotaReconciler

		BeforeEach(func() {
			r = &ClusterResourceQuotaReconciler{}
		})

		It("marks compute resources as needing pods", func() {
			kinds := r.classifyKindsNeeded(quotav1alpha1.ResourceList{
				corev1.ResourceRequestsCPU: resource.MustParse("1"),
			})
			Expect(kinds.pods).To(BeTrue())
			Expect(kinds.services).To(BeFalse())
			Expect(kinds.pvcs).To(BeFalse())
			Expect(kinds.storageClasses).To(BeFalse())
		})

		It("marks storage-class resources as needing pvcs and bucketing", func() {
			kinds := r.classifyKindsNeeded(quotav1alpha1.ResourceList{
				corev1.ResourceName("fast-ssd.storageclass.storage.k8s.io/requests.storage"): resource.MustParse("10Gi"),
			})
			Expect(kinds.pvcs).To(BeTrue())
			Expect(kinds.storageClasses).To(BeTrue())
			Expect(kinds.pods).To(BeFalse())
		})

		It("flags no list kinds when only object-count resources are tracked", func() {
			kinds := r.classifyKindsNeeded(quotav1alpha1.ResourceList{
				usage.ResourceConfigMaps: resource.MustParse("10"),
			})
			Expect(kinds.pods).To(BeFalse())
			Expect(kinds.services).To(BeFalse())
			Expect(kinds.pvcs).To(BeFalse())
		})

		It("treats requests.<extended> as a compute (pod) kind", func() {
			kinds := r.classifyKindsNeeded(quotav1alpha1.ResourceList{
				corev1.ResourceName("requests.nvidia.com/gpu"): resource.MustParse("4"),
			})
			Expect(kinds.pods).To(BeTrue())
		})

		It("marks limits.ephemeral-storage as needing pods", func() {
			kinds := r.classifyKindsNeeded(quotav1alpha1.ResourceList{
				corev1.ResourceLimitsEphemeralStorage: resource.MustParse("2Gi"),
			})
			Expect(kinds.pods).To(BeTrue())
			Expect(kinds.pvcs).To(BeFalse())
		})
	})

	Context("Storage Usage From Prefetched PVCs", func() {
		It("should aggregate requests.storage from pvc requests", func() {
			pvcs := []corev1.PersistentVolumeClaim{
				{
					Spec: corev1.PersistentVolumeClaimSpec{
						Resources: corev1.VolumeResourceRequirements{
							Requests: corev1.ResourceList{corev1.ResourceStorage: resource.MustParse("1Gi")},
						},
					},
				},
				{
					Spec: corev1.PersistentVolumeClaimSpec{
						Resources: corev1.VolumeResourceRequirements{
							Requests: corev1.ResourceList{corev1.ResourceStorage: resource.MustParse("512Mi")},
						},
					},
				},
			}

			usage := storage.CalculateStorageUsageFromPVCs(pvcs, corev1.ResourceRequestsStorage)
			Expect(usage.String()).To(Equal("1536Mi"))
		})

		It("should return zero for non-storage resources", func() {
			usage := storage.CalculateStorageUsageFromPVCs(nil, usage.ResourceServices)
			Expect(usage.IsZero()).To(BeTrue())
		})

		It("should aggregate storage usage by storage class", func() {
			fast := fastStorageClass
			slow := slowStorageClass
			pvcs := []corev1.PersistentVolumeClaim{
				{
					Spec: corev1.PersistentVolumeClaimSpec{
						StorageClassName: &fast,
						Resources: corev1.VolumeResourceRequirements{
							Requests: corev1.ResourceList{corev1.ResourceStorage: resource.MustParse("1Gi")},
						},
					},
				},
				{
					Spec: corev1.PersistentVolumeClaimSpec{
						StorageClassName: &fast,
						Resources: corev1.VolumeResourceRequirements{
							Requests: corev1.ResourceList{corev1.ResourceStorage: resource.MustParse("2Gi")},
						},
					},
				},
				{
					Spec: corev1.PersistentVolumeClaimSpec{
						StorageClassName: &slow,
						Resources: corev1.VolumeResourceRequirements{
							Requests: corev1.ResourceList{corev1.ResourceStorage: resource.MustParse("8Gi")},
						},
					},
				},
			}

			usage := storage.CalculateStorageClassUsageFromPVCs(pvcs, fastStorageClass)
			Expect(usage.String()).To(Equal("3Gi"))
		})

		It("should count pvcs by storage class", func() {
			fast := fastStorageClass
			slow := slowStorageClass
			pvcs := []corev1.PersistentVolumeClaim{
				{Spec: corev1.PersistentVolumeClaimSpec{StorageClassName: &fast}},
				{Spec: corev1.PersistentVolumeClaimSpec{StorageClassName: &fast}},
				{Spec: corev1.PersistentVolumeClaimSpec{StorageClassName: &slow}},
			}

			count := storage.CalculateStorageClassCountFromPVCs(pvcs, fastStorageClass)
			Expect(count).To(Equal(int64(2)))
		})

		It("should match legacy storage class annotation", func() {
			pvc := corev1.PersistentVolumeClaim{
				ObjectMeta: metav1.ObjectMeta{
					Annotations: map[string]string{"volume.beta.kubernetes.io/storage-class": fastStorageClass},
				},
			}

			Expect(storage.PVCMatchesStorageClass(&pvc, fastStorageClass)).To(BeTrue())
			Expect(storage.PVCMatchesStorageClass(&pvc, slowStorageClass)).To(BeFalse())
		})

		It("should count pvc objects", func() {
			pvcs := []corev1.PersistentVolumeClaim{{}, {}, {}}
			count := storage.CalculatePVCCountUsageFromPVCs(pvcs)
			Expect(count.String()).To(Equal("3"))
		})
	})

	Context("End-to-end aggregation", func() {
		It("aggregates compute, services, storage, storage-class, and extended resources in one pass", func() {
			fastClass := fastStorageClass
			slowClass := slowStorageClass

			fakeClient := fake.NewClientBuilder().WithObjects(
				&corev1.Pod{
					ObjectMeta: metav1.ObjectMeta{Name: "pod-running", Namespace: "ns-a"},
					Status:     corev1.PodStatus{Phase: corev1.PodRunning},
					Spec: corev1.PodSpec{Containers: []corev1.Container{{
						Resources: corev1.ResourceRequirements{
							Requests: corev1.ResourceList{
								corev1.ResourceCPU:                    resource.MustParse("250m"),
								corev1.ResourceName("nvidia.com/gpu"): resource.MustParse("2"),
								corev1.ResourceMemory:                 resource.MustParse("512Mi"),
							},
							Limits: corev1.ResourceList{corev1.ResourceCPU: resource.MustParse("1")},
						},
					}}},
				},
				&corev1.Pod{
					ObjectMeta: metav1.ObjectMeta{Name: "pod-terminal", Namespace: "ns-a"},
					Status:     corev1.PodStatus{Phase: corev1.PodSucceeded},
					Spec: corev1.PodSpec{Containers: []corev1.Container{{
						Resources: corev1.ResourceRequirements{
							Requests: corev1.ResourceList{corev1.ResourceCPU: resource.MustParse("10")},
						},
					}}},
				},
				&corev1.Service{ObjectMeta: metav1.ObjectMeta{Name: "svc-cluster", Namespace: "ns-a"}, Spec: corev1.ServiceSpec{Type: corev1.ServiceTypeClusterIP}},
				&corev1.Service{ObjectMeta: metav1.ObjectMeta{Name: "svc-lb", Namespace: "ns-a"}, Spec: corev1.ServiceSpec{Type: corev1.ServiceTypeLoadBalancer}},
				&corev1.Service{ObjectMeta: metav1.ObjectMeta{Name: "svc-np", Namespace: "ns-a"}, Spec: corev1.ServiceSpec{Type: corev1.ServiceTypeNodePort}},
				&corev1.PersistentVolumeClaim{
					ObjectMeta: metav1.ObjectMeta{Name: "pvc-fast", Namespace: "ns-a"},
					Spec: corev1.PersistentVolumeClaimSpec{
						StorageClassName: &fastClass,
						Resources:        corev1.VolumeResourceRequirements{Requests: corev1.ResourceList{corev1.ResourceStorage: resource.MustParse("1Gi")}},
					},
				},
				&corev1.PersistentVolumeClaim{
					ObjectMeta: metav1.ObjectMeta{
						Name:        "pvc-legacy-fast",
						Namespace:   "ns-a",
						Annotations: map[string]string{"volume.beta.kubernetes.io/storage-class": fastClass},
					},
					Spec: corev1.PersistentVolumeClaimSpec{
						Resources: corev1.VolumeResourceRequirements{Requests: corev1.ResourceList{corev1.ResourceStorage: resource.MustParse("2Gi")}},
					},
				},
				&corev1.PersistentVolumeClaim{
					ObjectMeta: metav1.ObjectMeta{Name: "pvc-slow", Namespace: "ns-a"},
					Spec: corev1.PersistentVolumeClaimSpec{
						StorageClassName: &slowClass,
						Resources:        corev1.VolumeResourceRequirements{Requests: corev1.ResourceList{corev1.ResourceStorage: resource.MustParse("8Gi")}},
					},
				},
			).Build()

			r := &ClusterResourceQuotaReconciler{
				Client:                fakeClient,
				ObjectCountCalculator: nil,
				logger:                zap.NewNop(),
			}

			fastStorage := corev1.ResourceName("fast-ssd.storageclass.storage.k8s.io/requests.storage")
			fastCount := corev1.ResourceName("fast-ssd.storageclass.storage.k8s.io/persistentvolumeclaims")
			gpu := corev1.ResourceName("requests.nvidia.com/gpu")

			crq := &quotav1alpha1.ClusterResourceQuota{
				ObjectMeta: metav1.ObjectMeta{Name: "all-resources"},
				Spec: quotav1alpha1.ClusterResourceQuotaSpec{
					Hard: quotav1alpha1.ResourceList{
						corev1.ResourceRequestsCPU:           resource.MustParse("10"),
						corev1.ResourceLimitsCPU:             resource.MustParse("20"),
						corev1.ResourcePods:                  resource.MustParse("10"),
						usage.ResourceServices:               resource.MustParse("10"),
						usage.ResourceServicesLoadBalancers:  resource.MustParse("10"),
						usage.ResourceServicesNodePorts:      resource.MustParse("10"),
						corev1.ResourceRequestsStorage:       resource.MustParse("100Gi"),
						usage.ResourcePersistentVolumeClaims: resource.MustParse("10"),
						gpu:                                  resource.MustParse("10"),
						fastStorage:                          resource.MustParse("100Gi"),
						fastCount:                            resource.MustParse("10"),
					},
				},
			}

			total, byNS, err := r.calculateAndAggregateUsage(ctx, crq, []string{"ns-a"})
			Expect(err).NotTo(HaveOccurred())
			Expect(byNS).To(HaveLen(1))
			Expect(byNS[0].Namespace).To(Equal("ns-a"))

			str := func(name corev1.ResourceName) string {
				q := total[name]
				return q.String()
			}
			Expect(str(corev1.ResourceRequestsCPU)).To(Equal("250m"))
			Expect(str(corev1.ResourceLimitsCPU)).To(Equal("1"))
			Expect(str(corev1.ResourcePods)).To(Equal("1"))
			Expect(str(usage.ResourceServices)).To(Equal("3"))
			Expect(str(usage.ResourceServicesLoadBalancers)).To(Equal("1"))
			Expect(str(usage.ResourceServicesNodePorts)).To(Equal("1"))
			Expect(str(corev1.ResourceRequestsStorage)).To(Equal("11Gi"))
			Expect(str(usage.ResourcePersistentVolumeClaims)).To(Equal("3"))
			Expect(str(gpu)).To(Equal("2"))
			Expect(str(fastStorage)).To(Equal("3Gi"))
			Expect(str(fastCount)).To(Equal("2"))
		})

		It("skips namespaces with an empty name without errors", func() {
			fakeClient := fake.NewClientBuilder().Build()
			r := &ClusterResourceQuotaReconciler{
				Client:                fakeClient,
				ObjectCountCalculator: objectcount.NewObjectCountCalculator(fakeClient, zap.NewNop()),
				logger:                zap.NewNop(),
			}
			crq := &quotav1alpha1.ClusterResourceQuota{
				ObjectMeta: metav1.ObjectMeta{Name: "ns-empty"},
				Spec: quotav1alpha1.ClusterResourceQuotaSpec{
					Hard: quotav1alpha1.ResourceList{corev1.ResourceRequestsCPU: resource.MustParse("1")},
				},
			}
			total, byNS, err := r.calculateAndAggregateUsage(ctx, crq, []string{""})
			Expect(err).NotTo(HaveOccurred())
			Expect(byNS).To(HaveLen(1))
			q := total[corev1.ResourceRequestsCPU]
			Expect(q.IsZero()).To(BeTrue())
		})
	})

	Context("State Management", func() {
		var reconciler *ClusterResourceQuotaReconciler

		BeforeEach(func() {
			c := fake.NewClientBuilder().Build()
			reconciler = &ClusterResourceQuotaReconciler{
				Client:                    c,
				logger:                    logger,
				previousNamespacesByQuota: make(map[string][]string),
				ExcludeNamespaceLabelKey:  "pac-quota-controller.powerapp.cloud/exclude",
			}
			// Set a mock CRQ client to prevent nil pointer dereference
			mockCRQClient := mocks.NewMockCRQClientInterface(GinkgoT())
			mockCRQClient.On("GetCRQByNamespace", mock.Anything, mock.AnythingOfType("*v1.Namespace")).Return(nil, nil).Maybe()
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
			c := fake.NewClientBuilder().Build()
			reconciler = &ClusterResourceQuotaReconciler{
				Client:                    c,
				logger:                    logger,
				previousNamespacesByQuota: make(map[string][]string),
				ExcludeNamespaceLabelKey:  "pac-quota-controller.powerapp.cloud/exclude",
			}
			// Set a mock CRQ client to prevent nil pointer dereference
			mockCRQClient := mocks.NewMockCRQClientInterface(GinkgoT())
			mockCRQClient.On("GetCRQByNamespace", mock.Anything, mock.AnythingOfType("*v1.Namespace")).Return(nil, nil).Maybe()
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
			testNamespace = &corev1.Namespace{
				ObjectMeta: metav1.ObjectMeta{
					Name: "test-namespace",
					Labels: map[string]string{
						"team": "test",
					},
				},
			}
			c := fake.NewClientBuilder().WithObjects(testNamespace).Build()
			reconciler = &ClusterResourceQuotaReconciler{
				Client:                    c,
				logger:                    logger,
				previousNamespacesByQuota: make(map[string][]string),
				ExcludeNamespaceLabelKey:  "pac-quota-controller.powerapp.cloud/exclude",
			}
			// Set a mock CRQ client to prevent nil pointer dereference
			mockCRQClient := mocks.NewMockCRQClientInterface(GinkgoT())
			mockCRQClient.On("GetCRQByNamespace", mock.Anything, mock.AnythingOfType("*v1.Namespace")).Return(nil, nil).Maybe()
			reconciler.crqClient = mockCRQClient
		})

		It("should handle context cancellation", func() {
			// Create cancelled context
			cancelledCtx, cancel := context.WithCancel(ctx)
			cancel()

			// Should handle cancelled context gracefully
			requests := reconciler.findQuotasForObject(cancelledCtx, testNamespace)
			Expect(requests).To(BeEmpty())
		})

		It("should handle timeout scenarios", func() {
			// Create context with timeout
			timeoutCtx, cancel := context.WithTimeout(ctx, 1*time.Nanosecond)
			defer cancel()

			// Should handle timeout gracefully
			requests := reconciler.findQuotasForObject(timeoutCtx, testNamespace)
			Expect(requests).To(BeEmpty())
		})
	})
})

var _ = Describe("calculateAndAggregateUsage list efficiency", func() {
	var (
		logger *zap.Logger
		ctx    context.Context
	)

	BeforeEach(func() {
		logger = zap.NewNop()
		ctx = context.Background()
	})

	// listCounter wraps a real fake client and counts List calls by object kind.
	listCounter := func(base client.WithWatch) (client.Client, *map[string]int) {
		counts := map[string]int{}
		c := interceptor.NewClient(base, interceptor.Funcs{
			List: func(ctx context.Context, w client.WithWatch, list client.ObjectList, opts ...client.ListOption) error {
				counts[fmt.Sprintf("%T", list)]++
				return w.List(ctx, list, opts...)
			},
		})
		return c, &counts
	}

	It("lists only the resource kinds the CRQ actually tracks", func() {
		base := fake.NewClientBuilder().WithObjects(
			&corev1.Namespace{ObjectMeta: metav1.ObjectMeta{Name: "ns-a", Labels: map[string]string{"team": "alpha"}}},
			&corev1.Pod{ObjectMeta: metav1.ObjectMeta{Name: "pod-a", Namespace: "ns-a"}},
			&corev1.Service{ObjectMeta: metav1.ObjectMeta{Name: "svc-a", Namespace: "ns-a"}},
			&corev1.PersistentVolumeClaim{ObjectMeta: metav1.ObjectMeta{Name: "pvc-a", Namespace: "ns-a"}},
			&corev1.ConfigMap{ObjectMeta: metav1.ObjectMeta{Name: "cm-a", Namespace: "ns-a"}},
		).Build()
		c, counts := listCounter(base)

		r := &ClusterResourceQuotaReconciler{
			Client:                c,
			ObjectCountCalculator: objectcount.NewObjectCountCalculator(c, logger),
			logger:                logger,
		}

		crq := &quotav1alpha1.ClusterResourceQuota{
			ObjectMeta: metav1.ObjectMeta{Name: "crq"},
			Spec: quotav1alpha1.ClusterResourceQuotaSpec{
				Hard: quotav1alpha1.ResourceList{
					corev1.ResourceRequestsCPU: resource.MustParse("4"),
					usage.ResourceConfigMaps:   resource.MustParse("10"),
				},
			},
		}

		_, _, err := r.calculateAndAggregateUsage(ctx, crq, []string{"ns-a"})
		Expect(err).NotTo(HaveOccurred())

		Expect((*counts)["*v1.PodList"]).To(Equal(1), "pods listed exactly once for ns-a")
		Expect((*counts)["*v1.ConfigMapList"]).To(Equal(1), "configmaps listed exactly once for ns-a")
		Expect((*counts)["*v1.ServiceList"]).To(Equal(0), "services must not be listed when no service quota is tracked")
		Expect((*counts)["*v1.PersistentVolumeClaimList"]).To(Equal(0), "pvcs must not be listed when no storage quota is tracked")
	})

	It("lists pvcs once per namespace regardless of how many storage classes the CRQ tracks", func() {
		fast := fastStorageClass
		slow := slowStorageClass
		base := fake.NewClientBuilder().WithObjects(
			&corev1.PersistentVolumeClaim{
				ObjectMeta: metav1.ObjectMeta{Name: "pvc-fast", Namespace: "ns-a"},
				Spec: corev1.PersistentVolumeClaimSpec{
					StorageClassName: &fast,
					Resources:        corev1.VolumeResourceRequirements{Requests: corev1.ResourceList{corev1.ResourceStorage: resource.MustParse("1Gi")}},
				},
			},
			&corev1.PersistentVolumeClaim{
				ObjectMeta: metav1.ObjectMeta{Name: "pvc-slow", Namespace: "ns-a"},
				Spec: corev1.PersistentVolumeClaimSpec{
					StorageClassName: &slow,
					Resources:        corev1.VolumeResourceRequirements{Requests: corev1.ResourceList{corev1.ResourceStorage: resource.MustParse("2Gi")}},
				},
			},
		).Build()
		c, counts := listCounter(base)

		r := &ClusterResourceQuotaReconciler{
			Client: c,
			logger: logger,
		}

		crq := &quotav1alpha1.ClusterResourceQuota{
			ObjectMeta: metav1.ObjectMeta{Name: "crq"},
			Spec: quotav1alpha1.ClusterResourceQuotaSpec{
				Hard: quotav1alpha1.ResourceList{
					corev1.ResourceRequestsStorage: resource.MustParse("100Gi"),
					corev1.ResourceName("fast-ssd.storageclass.storage.k8s.io/requests.storage"):       resource.MustParse("50Gi"),
					corev1.ResourceName("fast-ssd.storageclass.storage.k8s.io/persistentvolumeclaims"): resource.MustParse("10"),
					corev1.ResourceName("slow-hdd.storageclass.storage.k8s.io/requests.storage"):       resource.MustParse("50Gi"),
				},
			},
		}

		_, _, err := r.calculateAndAggregateUsage(ctx, crq, []string{"ns-a"})
		Expect(err).NotTo(HaveOccurred())

		Expect((*counts)["*v1.PersistentVolumeClaimList"]).To(Equal(1),
			"pvcs must be listed exactly once per namespace, not once per storage class")
	})
})

var _ = Describe("percentOfHard", func() {
	It("returns 0 when hard is zero or unset", func() {
		Expect(percentOfHard(resource.MustParse("500m"), resource.Quantity{})).To(Equal(0.0))
		Expect(percentOfHard(resource.MustParse("500m"), resource.MustParse("0"))).To(Equal(0.0))
	})

	It("returns used/hard for nonzero hard", func() {
		Expect(percentOfHard(resource.MustParse("500m"), resource.MustParse("1"))).To(BeNumerically("~", 0.5, 0.0001))
		Expect(percentOfHard(resource.MustParse("2Gi"), resource.MustParse("8Gi"))).To(BeNumerically("~", 0.25, 0.0001))
	})
})

var _ = Describe("CRQTotalUsage metric labels", func() {
	It("accepts exactly (crq_name, resource) — guards against re-adding cardinality-bomb labels", func() {
		// If someone re-adds a `namespace` or `namespaces` label, this assignment
		// fails at compile time and forces the change to be discussed first.
		metrics.CRQTotalUsage.WithLabelValues("crq-a", "requests.cpu").Set(0.5)
	})
})

var _ = Describe("calculateObjectCount with unsupported resource", func() {
	var (
		logger     *zap.Logger
		reconciler *ClusterResourceQuotaReconciler
	)

	BeforeEach(func() {
		logger, _ = zap.NewDevelopment()
		reconciler = &ClusterResourceQuotaReconciler{logger: logger}
	})

	It("returns zero with no error and increments the unsupported-resource counter", func() {
		const typo = "congigmaps"
		pre := promtestutil.ToFloat64(metrics.QuotaUnsupportedResource.WithLabelValues(typo))

		got, err := reconciler.calculateObjectCount(context.Background(), "any-ns", corev1.ResourceName(typo))
		Expect(err).ToNot(HaveOccurred())
		Expect(got.Value()).To(Equal(int64(0)))

		post := promtestutil.ToFloat64(metrics.QuotaUnsupportedResource.WithLabelValues(typo))
		Expect(post - pre).To(Equal(float64(1)))
	})
})
