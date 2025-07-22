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
	"fmt"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	quotav1alpha1 "github.com/powerhome/pac-quota-controller/api/v1alpha1"
	corev1 "k8s.io/api/core/v1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/api/resource"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	schema "k8s.io/apimachinery/pkg/runtime/schema"
	"k8s.io/apimachinery/pkg/types"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/event"
	"sigs.k8s.io/controller-runtime/pkg/reconcile"

	"slices"
)

type mockCRQClient struct {
	GetCRQByNamespaceFunc func(ctx context.Context, ns *corev1.Namespace) (*quotav1alpha1.ClusterResourceQuota, error)
}

func (m *mockCRQClient) ListAllCRQs(ctx context.Context) ([]quotav1alpha1.ClusterResourceQuota, error) {
	panic("not implemented")
}
func (m *mockCRQClient) GetCRQByNamespace(ctx context.Context, ns *corev1.Namespace) (*quotav1alpha1.ClusterResourceQuota, error) {
	return m.GetCRQByNamespaceFunc(ctx, ns)
}
func (m *mockCRQClient) NamespaceMatchesCRQ(ns *corev1.Namespace, crq *quotav1alpha1.ClusterResourceQuota) (bool, error) {
	panic("not implemented")
}
func (m *mockCRQClient) GetNamespacesFromStatus(crq *quotav1alpha1.ClusterResourceQuota) []string {
	panic("not implemented")
}

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
		By("Creating a shared ClusterResourceQuota for all tests")
		testQuota = &quotav1alpha1.ClusterResourceQuota{
			ObjectMeta: metav1.ObjectMeta{
				Name: "test-namespace-selector",
			},
			Spec: quotav1alpha1.ClusterResourceQuotaSpec{
				Hard: quotav1alpha1.ResourceList{
					"pods": resource.MustParse("10"),
				},
				NamespaceSelector: &metav1.LabelSelector{
					MatchLabels: map[string]string{
						"quota": "limited",
					},
				},
			},
		}
		_ = k8sClient.Delete(ctx, testQuota) // ensure clean slate
		Expect(k8sClient.Create(ctx, testQuota)).To(Succeed())
	})

	AfterAll(func() {
		By("Cleaning up shared ClusterResourceQuota after all tests")
		_ = k8sClient.Delete(ctx, testQuota)
	})

	Context("When reconciling a resource", func() {
		const resourceName = "test-resource"

		typeNamespacedName := types.NamespacedName{
			Name: resourceName,
		}
		clusterresourcequota := &quotav1alpha1.ClusterResourceQuota{}

		BeforeEach(func() {
			By("creating the custom resource for the Kind ClusterResourceQuota")
			err := k8sClient.Get(ctx, typeNamespacedName, clusterresourcequota)
			if err != nil && apierrors.IsNotFound(err) {
				resource := &quotav1alpha1.ClusterResourceQuota{
					ObjectMeta: metav1.ObjectMeta{
						Name: resourceName,
					},
					Spec: quotav1alpha1.ClusterResourceQuotaSpec{
						Hard: quotav1alpha1.ResourceList{
							"pods":            resource.MustParse("10"),
							"requests.cpu":    resource.MustParse("1"),
							"requests.memory": resource.MustParse("1Gi"),
							"limits.cpu":      resource.MustParse("2"),
							"limits.memory":   resource.MustParse("2Gi"),
						},
						NamespaceSelector: &metav1.LabelSelector{
							MatchLabels: map[string]string{
								"quota": "limited",
							},
						},
						Scopes: []corev1.ResourceQuotaScope{
							corev1.ResourceQuotaScopeNotTerminating,
						},
					},
				}
				Expect(k8sClient.Create(ctx, resource)).To(Succeed())
			}
		})

		AfterEach(func() {
			// TODO(user): Cleanup logic after each test, like removing the resource instance.
			resource := &quotav1alpha1.ClusterResourceQuota{}
			err := k8sClient.Get(ctx, typeNamespacedName, resource)
			Expect(err).NotTo(HaveOccurred())

			By("Cleanup the specific resource instance ClusterResourceQuota")
			Expect(k8sClient.Delete(ctx, resource)).To(Succeed())
		})
		It("should successfully reconcile the resource", func() {
			By("Reconciling the created resource")
			controllerReconciler := &ClusterResourceQuotaReconciler{
				Client: k8sClient,
				Scheme: k8sClient.Scheme(),
			}

			_, err := controllerReconciler.Reconcile(ctx, reconcile.Request{
				NamespacedName: typeNamespacedName,
			})
			Expect(err).NotTo(HaveOccurred())

			// Verify the resource can be fetched
			fetchedResource := &quotav1alpha1.ClusterResourceQuota{}
			err = k8sClient.Get(ctx, typeNamespacedName, fetchedResource)
			Expect(err).NotTo(HaveOccurred())

			// Since our controller doesn't have any real logic yet,
			// we'll just verify the resource exists and its name matches what we expect
			Expect(fetchedResource.Name).To(Equal(resourceName))
			// Note: Other fields would be checked in a full implementation
		})

		It("should correctly identify and track selected namespaces", func() {
			By("Creating test namespaces with labels")

			// Create test namespaces with different labels
			testNamespace1 := &corev1.Namespace{
				ObjectMeta: metav1.ObjectMeta{
					Name: "test-ns-1",
					Labels: map[string]string{
						"quota": "limited",
						"team":  "frontend",
					},
				},
			}
			Expect(k8sClient.Create(ctx, testNamespace1)).To(Succeed())

			testNamespace2 := &corev1.Namespace{
				ObjectMeta: metav1.ObjectMeta{
					Name: "test-ns-2",
					Labels: map[string]string{
						"quota": "limited",
						"team":  "backend",
					},
				},
			}
			Expect(k8sClient.Create(ctx, testNamespace2)).To(Succeed())

			testNamespace3 := &corev1.Namespace{
				ObjectMeta: metav1.ObjectMeta{
					Name: "test-ns-3",
					Labels: map[string]string{
						"quota": "unlimited",
						"team":  "data",
					},
				},
			}
			Expect(k8sClient.Create(ctx, testNamespace3)).To(Succeed())

			defer func() {
				By("Cleaning up test namespaces")
				Expect(k8sClient.Delete(ctx, testNamespace1)).To(Succeed())
				Expect(k8sClient.Delete(ctx, testNamespace2)).To(Succeed())
				Expect(k8sClient.Delete(ctx, testNamespace3)).To(Succeed())
			}()

			By("Reconciling the ClusterResourceQuota")
			controllerReconciler := &ClusterResourceQuotaReconciler{
				Client: k8sClient,
				Scheme: k8sClient.Scheme(),
			}

			_, err := controllerReconciler.Reconcile(ctx, reconcile.Request{
				NamespacedName: types.NamespacedName{Name: "test-namespace-selector"},
			})
			Expect(err).NotTo(HaveOccurred())

			By("Checking if the namespaces are recorded in the status")
			updatedQuota := &quotav1alpha1.ClusterResourceQuota{}
			Eventually(func() bool {
				err := k8sClient.Get(ctx, types.NamespacedName{Name: "test-namespace-selector"}, updatedQuota)
				if err != nil {
					return false
				}

				namespaces := getNamespaceNamesFromStatus(updatedQuota.Status.Namespaces)

				// Should contain both test-ns-1 and test-ns-2, but not test-ns-3
				return len(namespaces) == 2 &&
					(slices.Contains(namespaces, "test-ns-1") && slices.Contains(namespaces, "test-ns-2"))
			}, "30s", "1s").Should(BeTrue())
		})
		It("should not select namespaces that do not match the selector", func() {
			By("Creating a namespace with non-matching labels")
			nonMatchingNS := &corev1.Namespace{
				ObjectMeta: metav1.ObjectMeta{
					Name: "test-ns-nomatch",
					Labels: map[string]string{
						"quota": "unlimited",
						"team":  "infra",
					},
				},
			}
			Expect(k8sClient.Create(ctx, nonMatchingNS)).To(Succeed())
			defer func() { _ = k8sClient.Delete(ctx, nonMatchingNS) }()

			By("Reconciling the ClusterResourceQuota")
			controllerReconciler := &ClusterResourceQuotaReconciler{
				Client: k8sClient,
				Scheme: k8sClient.Scheme(),
			}
			_, err := controllerReconciler.Reconcile(ctx, reconcile.Request{
				NamespacedName: types.NamespacedName{Name: "test-namespace-selector"},
			})
			Expect(err).NotTo(HaveOccurred())

			By("Checking that the non-matching namespace is not recorded in the status")
			updatedQuota := &quotav1alpha1.ClusterResourceQuota{}
			Consistently(func() bool {
				err := k8sClient.Get(ctx, types.NamespacedName{Name: "test-namespace-selector"}, updatedQuota)
				if err != nil {
					return false
				}
				namespaces := getNamespaceNamesFromStatus(updatedQuota.Status.Namespaces)
				// Debug: print the namespaces for troubleshooting
				GinkgoWriter.Printf("Current namespaces in status: %v\n", namespaces)
				return !slices.Contains(namespaces, "test-ns-nomatch")
			}, "5s", "1s").Should(BeTrue())
		})

		It("should exclude the controller's own namespace and namespaces with the exclusion label", func() {
			By("Creating namespaces for exclusion test")
			crqName := "test-exclusion-crq"
			exclusionLabel := "exclude-this-ns"
			controllerNsName := "controller-namespace"

			exclusionTestCRQ := &quotav1alpha1.ClusterResourceQuota{
				ObjectMeta: metav1.ObjectMeta{Name: crqName},
				Spec: quotav1alpha1.ClusterResourceQuotaSpec{
					Hard: quotav1alpha1.ResourceList{"pods": resource.MustParse("1")},
					NamespaceSelector: &metav1.LabelSelector{
						MatchLabels: map[string]string{"env": "test-exclusion"},
					},
				},
			}
			Expect(k8sClient.Create(ctx, exclusionTestCRQ)).To(Succeed())

			// This namespace should be selected
			selectedNs := &corev1.Namespace{
				ObjectMeta: metav1.ObjectMeta{
					Name:   "selected-ns-for-exclusion-test",
					Labels: map[string]string{"env": "test-exclusion"},
				},
			}
			Expect(k8sClient.Create(ctx, selectedNs)).To(Succeed())

			// This namespace should be excluded by label
			excludedLabelNs := &corev1.Namespace{
				ObjectMeta: metav1.ObjectMeta{
					Name:   "excluded-ns-by-label",
					Labels: map[string]string{"env": "test-exclusion", exclusionLabel: "true"},
				},
			}
			Expect(k8sClient.Create(ctx, excludedLabelNs)).To(Succeed())

			// This namespace should be excluded because it's the controller's own
			controllerNs := &corev1.Namespace{
				ObjectMeta: metav1.ObjectMeta{
					Name:   controllerNsName,
					Labels: map[string]string{"env": "test-exclusion"},
				},
			}
			Expect(k8sClient.Create(ctx, controllerNs)).To(Succeed())

			defer func() {
				By("Cleaning up exclusion test resources")
				_ = k8sClient.Delete(ctx, exclusionTestCRQ)
				_ = k8sClient.Delete(ctx, selectedNs)
				_ = k8sClient.Delete(ctx, excludedLabelNs)
				_ = k8sClient.Delete(ctx, controllerNs)
			}()

			By("Reconciling with exclusion settings")
			controllerReconciler := &ClusterResourceQuotaReconciler{
				Client:                   k8sClient,
				Scheme:                   k8sClient.Scheme(),
				OwnNamespace:             controllerNsName,
				ExcludeNamespaceLabelKey: exclusionLabel,
			}

			_, err := controllerReconciler.Reconcile(ctx, reconcile.Request{
				NamespacedName: types.NamespacedName{Name: crqName},
			})
			Expect(err).NotTo(HaveOccurred())

			By("Checking that only the correct namespace is in the status")
			updatedQuota := &quotav1alpha1.ClusterResourceQuota{}
			Eventually(func(g Gomega) {
				err := k8sClient.Get(ctx, types.NamespacedName{Name: crqName}, updatedQuota)
				g.Expect(err).NotTo(HaveOccurred())

				namespaces := getNamespaceNamesFromStatus(updatedQuota.Status.Namespaces)
				g.Expect(namespaces).To(HaveLen(1), "Should only contain one namespace")
				g.Expect(namespaces).To(ContainElement("selected-ns-for-exclusion-test"), "Should contain the selected namespace")
				g.Expect(namespaces).NotTo(ContainElement("excluded-ns-by-label"), "Should not contain the label-excluded namespace")
				g.Expect(namespaces).NotTo(ContainElement(controllerNsName), "Should not contain the controller's own namespace")
			}, "10s", "250ms").Should(Succeed())
		})

		It("should handle ScopeSelector field", func() {
			By("Creating a ClusterResourceQuota with ScopeSelector")
			resourceWithSelector := &quotav1alpha1.ClusterResourceQuota{
				ObjectMeta: metav1.ObjectMeta{
					Name: "test-scope-selector",
				},
				Spec: quotav1alpha1.ClusterResourceQuotaSpec{
					Hard: quotav1alpha1.ResourceList{
						"pods": resource.MustParse("5"),
					},
					NamespaceSelector: &metav1.LabelSelector{
						MatchLabels: map[string]string{
							"environment": "test",
						},
					},
					ScopeSelector: &corev1.ScopeSelector{
						MatchExpressions: []corev1.ScopedResourceSelectorRequirement{
							{
								ScopeName: corev1.ResourceQuotaScopePriorityClass,
								Operator:  corev1.ScopeSelectorOpIn,
								Values:    []string{"high"},
							},
						},
					},
				},
			}

			// Create the resource
			Expect(k8sClient.Create(ctx, resourceWithSelector)).To(Succeed())

			// Clean up after test
			defer func() {
				Expect(k8sClient.Delete(ctx, resourceWithSelector)).To(Succeed())
			}() // Verify it was created correctly
			fetchedResource := &quotav1alpha1.ClusterResourceQuota{}
			Eventually(func() error {
				return k8sClient.Get(ctx, types.NamespacedName{Name: "test-scope-selector"}, fetchedResource)
			}, "30s", "1s").Should(Succeed())

			// Since our controller doesn't modify the resource,
			// we'll just check that it exists and has the correct name
			Expect(fetchedResource.Name).To(Equal("test-scope-selector"))
		})
	})

	Context("Namespace Exclusion", func() {
		var reconciler *ClusterResourceQuotaReconciler

		BeforeEach(func() {
			reconciler = &ClusterResourceQuotaReconciler{
				Client:                   k8sClient,
				Scheme:                   k8sClient.Scheme(),
				OwnNamespace:             "controller-ns",
				ExcludeNamespaceLabelKey: "exclude-from-quota",
			}
		})

		It("should identify its own namespace as excluded", func() {
			ownNs := &corev1.Namespace{ObjectMeta: metav1.ObjectMeta{Name: "controller-ns"}}
			Expect(reconciler.isNamespaceExcluded(ownNs)).To(BeTrue())
		})

		It("should identify a namespace with the exclusion label", func() {
			labeledNs := &corev1.Namespace{
				ObjectMeta: metav1.ObjectMeta{
					Name:   "labeled-ns",
					Labels: map[string]string{"exclude-from-quota": "true"},
				},
			}
			Expect(reconciler.isNamespaceExcluded(labeledNs)).To(BeTrue())
		})

		It("should not exclude a regular namespace", func() {
			appNs := &corev1.Namespace{ObjectMeta: metav1.ObjectMeta{Name: "app-ns"}}
			Expect(reconciler.isNamespaceExcluded(appNs)).To(BeFalse())
		})

		It("should return no requests for an excluded namespace", func() {
			excludedNs := &corev1.Namespace{
				ObjectMeta: metav1.ObjectMeta{
					Name:   "another-excluded-ns",
					Labels: map[string]string{"exclude-from-quota": "true"},
				},
			}
			// We don't need to create this in the API server, just pass the object
			requests := reconciler.findQuotasForObject(ctx, excludedNs)
			Expect(requests).To(BeEmpty())
		})

		It("should return no requests for an object in an excluded namespace", func() {
			// The reconciler's isNamespaceExcluded checks the label on the namespace.
			// For this test to work, we need to simulate that the namespace exists and has the label.
			excludedNs := &corev1.Namespace{
				ObjectMeta: metav1.ObjectMeta{
					Name:   "special-ns",
					Labels: map[string]string{"exclude-from-quota": "true"},
				},
			}
			Expect(k8sClient.Create(ctx, excludedNs)).To(Succeed())
			defer func() {
				_ = k8sClient.Delete(ctx, excludedNs)
			}()

			podInExcludedNs := &corev1.Pod{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "test-pod",
					Namespace: "special-ns",
				},
			}
			requests := reconciler.findQuotasForObject(ctx, podInExcludedNs)
			Expect(requests).To(BeEmpty())
		})

		It("should identify its own namespace as excluded, even if it matches the selector and has no exclusion label", func() {
			ownNs := &corev1.Namespace{
				ObjectMeta: metav1.ObjectMeta{
					Name:   "controller-ns",
					Labels: map[string]string{"quota": "should-match"},
				},
			}
			// Exclusion should be true even if the label matches
			Expect(reconciler.isNamespaceExcluded(ownNs)).To(BeTrue())
		})
	})

	Context("Predicate Filtering", func() {
		var predicate resourceUpdatePredicate
		var oldPod, newPod *corev1.Pod

		BeforeEach(func() {
			predicate = resourceUpdatePredicate{}
			oldPod = &corev1.Pod{
				ObjectMeta: metav1.ObjectMeta{Generation: 1},
				Status:     corev1.PodStatus{Phase: corev1.PodRunning},
			}
			newPod = oldPod.DeepCopy()
		})

		It("should reconcile when generation changes", func() {
			newPod.Generation = 2
			event := event.UpdateEvent{ObjectOld: oldPod, ObjectNew: newPod}
			Expect(predicate.Update(event)).To(BeTrue())
		})

		It("should not reconcile for status updates without phase change", func() {
			newPod.Status.ContainerStatuses = []corev1.ContainerStatus{{Name: "test", Ready: true}}
			event := event.UpdateEvent{ObjectOld: oldPod, ObjectNew: newPod}
			Expect(predicate.Update(event)).To(BeFalse())
		})

		It("should reconcile when pod becomes terminal", func() {
			newPod.Status.Phase = corev1.PodSucceeded
			event := event.UpdateEvent{ObjectOld: oldPod, ObjectNew: newPod}
			Expect(predicate.Update(event)).To(BeTrue())
		})

		It("should not reconcile for non-terminal phase changes", func() {
			oldPod.Status.Phase = corev1.PodPending
			newPod.Status.Phase = corev1.PodRunning
			event := event.UpdateEvent{ObjectOld: oldPod, ObjectNew: newPod}
			Expect(predicate.Update(event)).To(BeFalse())
		})
	})

	Describe("findQuotasForObject (with Namespace objects)", func() {
		var (
			reconciler *ClusterResourceQuotaReconciler
			ns         *corev1.Namespace
		)

		BeforeEach(func() {
			reconciler = &ClusterResourceQuotaReconciler{
				OwnNamespace:             "controller-ns",
				ExcludeNamespaceLabelKey: "exclude-from-quota",
			}
		})

		It("returns empty if namespace is excluded (by name)", func() {
			ns = &corev1.Namespace{ObjectMeta: metav1.ObjectMeta{Name: "controller-ns"}}
			reconciler.crqClient = &mockCRQClient{
				GetCRQByNamespaceFunc: func(ctx context.Context, ns *corev1.Namespace) (*quotav1alpha1.ClusterResourceQuota, error) {
					return nil, nil
				},
			}
			result := reconciler.findQuotasForObject(context.Background(), ns)
			Expect(result).To(BeEmpty())
		})

		It("returns empty if namespace is excluded (by label)", func() {
			ns = &corev1.Namespace{ObjectMeta: metav1.ObjectMeta{Name: "foo", Labels: map[string]string{"exclude-from-quota": "true"}}}
			reconciler.crqClient = &mockCRQClient{
				GetCRQByNamespaceFunc: func(ctx context.Context, ns *corev1.Namespace) (*quotav1alpha1.ClusterResourceQuota, error) {
					return nil, nil
				},
			}
			result := reconciler.findQuotasForObject(context.Background(), ns)
			Expect(result).To(BeEmpty())
		})

		It("returns quotas that match the namespace selector", func() {
			ns = &corev1.Namespace{ObjectMeta: metav1.ObjectMeta{Name: "foo", Labels: map[string]string{"team": "dev"}}}
			crq := quotav1alpha1.ClusterResourceQuota{
				ObjectMeta: metav1.ObjectMeta{Name: "crq1"},
			}
			reconciler.crqClient = &mockCRQClient{
				GetCRQByNamespaceFunc: func(ctx context.Context, ns *corev1.Namespace) (*quotav1alpha1.ClusterResourceQuota, error) {
					return &crq, nil
				},
			}
			result := reconciler.findQuotasForObject(context.Background(), ns)
			Expect(result).To(HaveLen(1))
			Expect(result[0].Name).To(Equal("crq1"))
		})

		It("returns empty if no quotas match the namespace selector", func() {
			ns = &corev1.Namespace{ObjectMeta: metav1.ObjectMeta{Name: "foo", Labels: map[string]string{"team": "dev"}}}
			reconciler.crqClient = &mockCRQClient{
				GetCRQByNamespaceFunc: func(ctx context.Context, ns *corev1.Namespace) (*quotav1alpha1.ClusterResourceQuota, error) {
					return nil, nil
				},
			}
			result := reconciler.findQuotasForObject(context.Background(), ns)
			Expect(result).To(BeEmpty())
		})

		It("returns quotas with nil NamespaceSelector (selects all)", func() {
			ns = &corev1.Namespace{ObjectMeta: metav1.ObjectMeta{Name: "foo"}}
			crq := quotav1alpha1.ClusterResourceQuota{
				ObjectMeta: metav1.ObjectMeta{Name: "crq1"},
			}
			reconciler.crqClient = &mockCRQClient{
				GetCRQByNamespaceFunc: func(ctx context.Context, ns *corev1.Namespace) (*quotav1alpha1.ClusterResourceQuota, error) {
					return &crq, nil
				},
			}
			result := reconciler.findQuotasForObject(context.Background(), ns)
			Expect(result).To(HaveLen(1))
			Expect(result[0].Name).To(Equal("crq1"))
		})
	})

	Describe("findQuotasForObject", func() {
		var (
			reconciler *ClusterResourceQuotaReconciler
			pod        *corev1.Pod
		)

		BeforeEach(func() {
			reconciler = &ClusterResourceQuotaReconciler{
				OwnNamespace:             "controller-ns",
				ExcludeNamespaceLabelKey: "exclude-from-quota",
				Client:                   k8sClient, // Use the test env client for namespace lookup
				Scheme:                   k8sClient.Scheme(),
			}
		})

		It("returns empty if namespace is excluded", func() {
			// Create the excluded namespace in the test env
			ns := &corev1.Namespace{ObjectMeta: metav1.ObjectMeta{Name: "controller-ns"}}
			_ = k8sClient.Create(context.Background(), ns)
			defer func() { _ = k8sClient.Delete(context.Background(), ns) }()
			pod = &corev1.Pod{ObjectMeta: metav1.ObjectMeta{Name: "p1", Namespace: "controller-ns"}}
			reconciler.crqClient = &mockCRQClient{
				GetCRQByNamespaceFunc: func(ctx context.Context, ns *corev1.Namespace) (*quotav1alpha1.ClusterResourceQuota, error) {
					return nil, nil
				},
			}
			result := reconciler.findQuotasForObject(context.Background(), pod)
			Expect(result).To(BeEmpty())
		})

		It("returns quotas that match the object's namespace selector", func() {
			// Create the namespace in the test env
			ns := &corev1.Namespace{ObjectMeta: metav1.ObjectMeta{Name: "foo", Labels: map[string]string{"team": "dev"}}}
			_ = k8sClient.Create(context.Background(), ns)
			defer func() { _ = k8sClient.Delete(context.Background(), ns) }()
			pod = &corev1.Pod{ObjectMeta: metav1.ObjectMeta{Name: "p1", Namespace: "foo"}}
			crq := quotav1alpha1.ClusterResourceQuota{
				ObjectMeta: metav1.ObjectMeta{Name: "crq1"},
			}
			reconciler.crqClient = &mockCRQClient{
				GetCRQByNamespaceFunc: func(ctx context.Context, ns *corev1.Namespace) (*quotav1alpha1.ClusterResourceQuota, error) {
					return &crq, nil
				},
			}
			result := reconciler.findQuotasForObject(context.Background(), pod)
			Expect(result).To(HaveLen(1))
			Expect(result[0].Name).To(Equal("crq1"))
		})

		It("returns empty if namespace not found in cluster", func() {
			pod = &corev1.Pod{ObjectMeta: metav1.ObjectMeta{Name: "p1", Namespace: "bar"}}
			reconciler.crqClient = &mockCRQClient{
				GetCRQByNamespaceFunc: func(ctx context.Context, ns *corev1.Namespace) (*quotav1alpha1.ClusterResourceQuota, error) {
					return nil, nil
				},
			}
			result := reconciler.findQuotasForObject(context.Background(), pod)
			Expect(result).To(BeEmpty())
		})

		It("returns quotas with nil NamespaceSelector (selects all)", func() {
			// Create the namespace in the test env
			ns := &corev1.Namespace{ObjectMeta: metav1.ObjectMeta{Name: "foo"}}
			_ = k8sClient.Create(context.Background(), ns)
			defer func() { _ = k8sClient.Delete(context.Background(), ns) }()
			pod = &corev1.Pod{ObjectMeta: metav1.ObjectMeta{Name: "p1", Namespace: "foo"}}
			crq := quotav1alpha1.ClusterResourceQuota{
				ObjectMeta: metav1.ObjectMeta{Name: "crq1"},
			}
			reconciler.crqClient = &mockCRQClient{
				GetCRQByNamespaceFunc: func(ctx context.Context, ns *corev1.Namespace) (*quotav1alpha1.ClusterResourceQuota, error) {
					return &crq, nil
				},
			}
			result := reconciler.findQuotasForObject(context.Background(), pod)
			Expect(result).To(HaveLen(1))
			Expect(result[0].Name).To(Equal("crq1"))
		})
	})

	var _ = Describe("ClusterResourceQuotaReconciler.Reconcile error paths", func() {
		var (
			reconciler *ClusterResourceQuotaReconciler
			ctx        context.Context
		)

		BeforeEach(func() {
			ctx = context.Background()
		})

		It("returns nil if CRQ is not found (deleted)", func() {
			fakeClient := &fakeClient{
				getFunc: func(_ context.Context, key client.ObjectKey, obj client.Object) error {
					return apierrors.NewNotFound(schema.GroupResource{Group: "quota.powerapp.cloud", Resource: "clusterresourcequotas"}, key.Name)
				},
			}
			reconciler = &ClusterResourceQuotaReconciler{
				Client:    fakeClient,
				Scheme:    nil,
				crqClient: &mockCRQClient{},
			}
			result, err := reconciler.Reconcile(ctx, reconcile.Request{NamespacedName: types.NamespacedName{Name: "foo"}})
			Expect(err).ToNot(HaveOccurred())
			Expect(result).To(Equal(ctrl.Result{}))
		})

		It("returns error if Get returns error (not NotFound)", func() {
			fakeClient := &fakeClient{
				getFunc: func(_ context.Context, key client.ObjectKey, obj client.Object) error {
					return fmt.Errorf("some error")
				},
			}
			reconciler = &ClusterResourceQuotaReconciler{
				Client:    fakeClient,
				Scheme:    nil,
				crqClient: &mockCRQClient{},
			}
			_, err := reconciler.Reconcile(ctx, reconcile.Request{NamespacedName: types.NamespacedName{Name: "foo"}})
			Expect(err).To(MatchError("some error"))
		})

		It("returns error if label selector is invalid", func() {
			crq := &quotav1alpha1.ClusterResourceQuota{
				ObjectMeta: metav1.ObjectMeta{Name: "foo"},
				Spec: quotav1alpha1.ClusterResourceQuotaSpec{
					NamespaceSelector: &metav1.LabelSelector{MatchLabels: map[string]string{"bad[": "label"}},
				},
			}
			fakeClient := &fakeClient{
				getFunc: func(_ context.Context, key client.ObjectKey, obj client.Object) error {
					crq.DeepCopyInto(obj.(*quotav1alpha1.ClusterResourceQuota))
					return nil
				},
			}
			reconciler = &ClusterResourceQuotaReconciler{
				Client:    fakeClient,
				Scheme:    nil,
				crqClient: &mockCRQClient{},
			}
			_, err := reconciler.Reconcile(ctx, reconcile.Request{NamespacedName: types.NamespacedName{Name: "foo"}})
			Expect(err).To(HaveOccurred())
			Expect(err.Error()).To(ContainSubstring("failed to create selector"))
		})

		It("returns error if List namespaces fails", func() {
			crq := &quotav1alpha1.ClusterResourceQuota{
				ObjectMeta: metav1.ObjectMeta{Name: "foo"},
				Spec: quotav1alpha1.ClusterResourceQuotaSpec{
					NamespaceSelector: &metav1.LabelSelector{MatchLabels: map[string]string{"team": "dev"}},
				},
			}
			fakeClient := &fakeClient{
				getFunc: func(_ context.Context, key client.ObjectKey, obj client.Object) error {
					crq.DeepCopyInto(obj.(*quotav1alpha1.ClusterResourceQuota))
					return nil
				},
				listFunc: func(_ context.Context, obj client.ObjectList, opts ...client.ListOption) error {
					return fmt.Errorf("list error")
				},
			}
			reconciler = &ClusterResourceQuotaReconciler{
				Client:    fakeClient,
				Scheme:    nil,
				crqClient: &mockCRQClient{},
			}
			_, err := reconciler.Reconcile(ctx, reconcile.Request{NamespacedName: types.NamespacedName{Name: "foo"}})
			Expect(err).To(MatchError("list error"))
		})

		It("returns error if updateStatus fails", func() {
			crq := &quotav1alpha1.ClusterResourceQuota{
				ObjectMeta: metav1.ObjectMeta{Name: "foo"},
				Spec: quotav1alpha1.ClusterResourceQuotaSpec{
					NamespaceSelector: &metav1.LabelSelector{MatchLabels: map[string]string{"team": "dev"}},
					Hard:              quotav1alpha1.ResourceList{"pods": resource.MustParse("1")},
				},
			}
			nsList := &corev1.NamespaceList{Items: []corev1.Namespace{{ObjectMeta: metav1.ObjectMeta{Name: "ns1", Labels: map[string]string{"team": "dev"}}}}}
			fakeClient := &fakeClient{
				getFunc: func(_ context.Context, key client.ObjectKey, obj client.Object) error {
					crq.DeepCopyInto(obj.(*quotav1alpha1.ClusterResourceQuota))
					return nil
				},
				listFunc: func(_ context.Context, obj client.ObjectList, opts ...client.ListOption) error {
					nsList.DeepCopyInto(obj.(*corev1.NamespaceList))
					return nil
				},
				statusWriter: &fakeStatusWriter{},
			}
			reconciler = &ClusterResourceQuotaReconciler{
				Client:    fakeClient,
				Scheme:    nil,
				crqClient: &mockCRQClient{},
			}
			_, err := reconciler.Reconcile(ctx, reconcile.Request{NamespacedName: types.NamespacedName{Name: "foo"}})
			Expect(err).To(MatchError("patch error"))
		})
	})
})

// getNamespaceNamesFromStatus extracts namespace names from []ResourceQuotaStatusByNamespace
func getNamespaceNamesFromStatus(statuses []quotav1alpha1.ResourceQuotaStatusByNamespace) []string {
	names := make([]string, 0, len(statuses))
	for _, nsStatus := range statuses {
		names = append(names, nsStatus.Namespace)
	}
	return names
}
