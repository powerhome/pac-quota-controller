package controller

import (
	"context"
	"errors"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	"go.uber.org/zap"

	corev1 "k8s.io/api/core/v1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/api/resource"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/runtime/schema"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/client/fake"
	"sigs.k8s.io/controller-runtime/pkg/client/interceptor"
	"sigs.k8s.io/controller-runtime/pkg/event"

	quotav1alpha1 "github.com/powerhome/pac-quota-controller/api/v1alpha1"
	"github.com/powerhome/pac-quota-controller/pkg/events"
	"github.com/powerhome/pac-quota-controller/pkg/kubernetes/objectcount"
	"github.com/powerhome/pac-quota-controller/pkg/kubernetes/usage"
)

// fakeEventRecorder captures emitted events as "type/reason" strings.
type fakeEventRecorder struct {
	events []string
}

func (f *fakeEventRecorder) Eventf(
	regarding, related runtime.Object, eventtype, reason, action, note string, args ...any,
) {
	f.events = append(f.events, eventtype+"/"+reason)
}

// errStatusWriter fails Patch with a configurable error.
type errStatusWriter struct{ err error }

func (w *errStatusWriter) Patch(
	_ context.Context, _ client.Object, _ client.Patch, _ ...client.SubResourcePatchOption,
) error {
	return w.err
}
func (w *errStatusWriter) Update(_ context.Context, _ client.Object, _ ...client.SubResourceUpdateOption) error {
	return nil
}
func (w *errStatusWriter) Create(
	_ context.Context, _ client.Object, _ client.Object, _ ...client.SubResourceCreateOption,
) error {
	return nil
}
func (w *errStatusWriter) Apply(
	_ context.Context, _ runtime.ApplyConfiguration, _ ...client.SubResourceApplyOption,
) error {
	return nil
}

var _ = Describe("Reconciler error paths", func() {
	var (
		logger *zap.Logger
		rec    *fakeEventRecorder
		ctx    context.Context
	)

	BeforeEach(func() {
		logger = zap.NewNop()
		rec = &fakeEventRecorder{}
		ctx = context.Background()
	})

	newReconciler := func(c client.Client) *ClusterResourceQuotaReconciler {
		return &ClusterResourceQuotaReconciler{
			Client:                    c,
			logger:                    logger,
			EventRecorder:             events.NewEventRecorder(rec, "pac-quota-controller-system", logger),
			previousNamespacesByQuota: make(map[string][]string),
		}
	}

	// getCRQ returns a Get func that populates the requested CRQ object.
	getCRQ := func(crq *quotav1alpha1.ClusterResourceQuota) func(context.Context, client.ObjectKey, client.Object) error {
		return func(_ context.Context, _ client.ObjectKey, obj client.Object) error {
			if out, ok := obj.(*quotav1alpha1.ClusterResourceQuota); ok {
				*out = *crq
			}
			return nil
		}
	}

	req := ctrl.Request{NamespacedName: client.ObjectKey{Name: "test-quota"}}

	Describe("handleNamespaceChanges", func() {
		It("emits NamespaceAdded for new namespaces and tracks them sorted", func() {
			r := newReconciler(&fakeClient{})
			crq := &quotav1alpha1.ClusterResourceQuota{ObjectMeta: metav1.ObjectMeta{Name: "q"}}

			r.handleNamespaceChanges(crq, []string{"b", "a"})

			Expect(rec.events).To(ConsistOf("Normal/NamespaceAdded", "Normal/NamespaceAdded"))
			Expect(r.previousNamespacesByQuota["q"]).To(Equal([]string{"a", "b"}))
		})

		It("emits NamespaceAdded and NamespaceRemoved on a subsequent change", func() {
			r := newReconciler(&fakeClient{})
			crq := &quotav1alpha1.ClusterResourceQuota{ObjectMeta: metav1.ObjectMeta{Name: "q"}}

			r.handleNamespaceChanges(crq, []string{"a", "b"})
			rec.events = nil // reset, only inspect the second transition
			r.handleNamespaceChanges(crq, []string{"b", "c"})

			Expect(rec.events).To(ConsistOf("Normal/NamespaceAdded", "Normal/NamespaceRemoved"))
			Expect(r.previousNamespacesByQuota["q"]).To(Equal([]string{"b", "c"}))
		})
	})

	Describe("Reconcile", func() {
		It("emits InvalidSelector and returns an error for a malformed selector", func() {
			crq := &quotav1alpha1.ClusterResourceQuota{
				ObjectMeta: metav1.ObjectMeta{Name: "test-quota"},
				Spec: quotav1alpha1.ClusterResourceQuotaSpec{
					NamespaceSelector: &metav1.LabelSelector{
						MatchExpressions: []metav1.LabelSelectorRequirement{
							{Key: "team", Operator: "BadOperator"},
						},
					},
				},
			}
			r := newReconciler(&fakeClient{getFunc: getCRQ(crq)})

			_, err := r.Reconcile(ctx, req)
			Expect(err).To(HaveOccurred())
			Expect(rec.events).To(ContainElement("Warning/InvalidSelector"))
		})

		It("emits CalculationFailed and returns an error when listing namespaces fails", func() {
			crq := &quotav1alpha1.ClusterResourceQuota{
				ObjectMeta: metav1.ObjectMeta{Name: "test-quota"},
				Spec: quotav1alpha1.ClusterResourceQuotaSpec{
					NamespaceSelector: &metav1.LabelSelector{MatchLabels: map[string]string{"team": "a"}},
				},
			}
			r := newReconciler(&fakeClient{
				getFunc: getCRQ(crq),
				listFunc: func(_ context.Context, _ client.ObjectList, _ ...client.ListOption) error {
					return errors.New("list boom")
				},
			})

			_, err := r.Reconcile(ctx, req)
			Expect(err).To(HaveOccurred())
			Expect(rec.events).To(ContainElement("Warning/CalculationFailed"))
		})

		It("returns nil without error when the CRQ is not found", func() {
			r := newReconciler(&fakeClient{
				getFunc: func(_ context.Context, _ client.ObjectKey, _ client.Object) error {
					return apierrors.NewNotFound(schema.GroupResource{Resource: "clusterresourcequotas"}, "test-quota")
				},
			})

			result, err := r.Reconcile(ctx, req)
			Expect(err).NotTo(HaveOccurred())
			Expect(result).To(Equal(ctrl.Result{}))
		})

		It("returns an error when fetching the CRQ fails for a non-NotFound reason", func() {
			r := newReconciler(&fakeClient{
				getFunc: func(_ context.Context, _ client.ObjectKey, _ client.Object) error {
					return errors.New("get boom")
				},
			})

			_, err := r.Reconcile(ctx, req)
			Expect(err).To(HaveOccurred())
		})

		It("ignores a NotFound error from the status patch", func() {
			crq := &quotav1alpha1.ClusterResourceQuota{
				ObjectMeta: metav1.ObjectMeta{Name: "test-quota"},
				Spec: quotav1alpha1.ClusterResourceQuotaSpec{
					NamespaceSelector: &metav1.LabelSelector{MatchLabels: map[string]string{"team": "a"}},
					// Non-empty Hard makes the computed status differ from the empty
					// original, so updateStatus actually attempts the patch.
					Hard: quotav1alpha1.ResourceList{corev1.ResourceCPU: resource.MustParse("1")},
				},
			}
			r := newReconciler(&fakeClient{
				getFunc: getCRQ(crq),
				statusWriter: &errStatusWriter{
					err: apierrors.NewNotFound(schema.GroupResource{Resource: "clusterresourcequotas"}, "test-quota"),
				},
			})

			result, err := r.Reconcile(ctx, req)
			Expect(err).NotTo(HaveOccurred())
			Expect(result).To(Equal(ctrl.Result{}))
		})

		It("returns an error for a non-NotFound status patch failure", func() {
			crq := &quotav1alpha1.ClusterResourceQuota{
				ObjectMeta: metav1.ObjectMeta{Name: "test-quota"},
				Spec: quotav1alpha1.ClusterResourceQuotaSpec{
					NamespaceSelector: &metav1.LabelSelector{MatchLabels: map[string]string{"team": "a"}},
					Hard:              quotav1alpha1.ResourceList{corev1.ResourceCPU: resource.MustParse("1")},
				},
			}
			r := newReconciler(&fakeClient{
				getFunc:      getCRQ(crq),
				statusWriter: &errStatusWriter{err: errors.New("patch boom")},
			})

			_, err := r.Reconcile(ctx, req)
			Expect(err).To(HaveOccurred())
		})
	})

	Describe("checkQuotaThresholds", func() {
		It("emits QuotaExceeded when usage is over the hard limit", func() {
			r := newReconciler(&fakeClient{})
			crq := &quotav1alpha1.ClusterResourceQuota{
				ObjectMeta: metav1.ObjectMeta{Name: "q"},
				Spec: quotav1alpha1.ClusterResourceQuotaSpec{
					Hard: quotav1alpha1.ResourceList{corev1.ResourceCPU: resource.MustParse("1")},
				},
			}
			usage := quotav1alpha1.ResourceList{corev1.ResourceCPU: resource.MustParse("2")}

			r.checkQuotaThresholds(crq, usage)
			Expect(rec.events).To(ConsistOf("Warning/QuotaExceeded"))
		})

		It("does not emit when usage is within the limit", func() {
			r := newReconciler(&fakeClient{})
			crq := &quotav1alpha1.ClusterResourceQuota{
				ObjectMeta: metav1.ObjectMeta{Name: "q"},
				Spec: quotav1alpha1.ClusterResourceQuotaSpec{
					Hard: quotav1alpha1.ResourceList{corev1.ResourceCPU: resource.MustParse("2")},
				},
			}
			usage := quotav1alpha1.ResourceList{corev1.ResourceCPU: resource.MustParse("1")}

			r.checkQuotaThresholds(crq, usage)
			Expect(rec.events).To(BeEmpty())
		})
	})

	Describe("resourceUpdatePredicate.Delete", func() {
		pred := resourceUpdatePredicate{}

		It("triggers reconciliation on Pod deletion", func() {
			Expect(pred.Delete(event.DeleteEvent{Object: &corev1.Pod{}})).To(BeTrue())
		})

		It("ignores deletion of non-Pod resources", func() {
			Expect(pred.Delete(event.DeleteEvent{Object: &corev1.Service{}})).To(BeFalse())
		})

		It("ignores a nil object", func() {
			Expect(pred.Delete(event.DeleteEvent{Object: nil})).To(BeFalse())
		})
	})

	Describe("Reconcile happy path", func() {
		nsWithLabels := func(name string, lbls map[string]string) *corev1.Namespace {
			return &corev1.Namespace{ObjectMeta: metav1.ObjectMeta{Name: name, Labels: lbls}}
		}

		It("selects matching namespaces, skips excluded ones, and writes status", func() {
			crq := &quotav1alpha1.ClusterResourceQuota{
				ObjectMeta: metav1.ObjectMeta{Name: "test-quota"},
				Spec: quotav1alpha1.ClusterResourceQuotaSpec{
					NamespaceSelector: &metav1.LabelSelector{MatchLabels: map[string]string{"team": "a"}},
					Hard:              quotav1alpha1.ResourceList{corev1.ResourceRequestsCPU: resource.MustParse("10")},
				},
			}
			excludeKey := "pac-quota-controller.powerapp.cloud/exclude"
			c := fake.NewClientBuilder().
				WithObjects(
					crq,
					nsWithLabels("ns-a", map[string]string{"team": "a"}),
					nsWithLabels("ns-excluded", map[string]string{"team": "a", excludeKey: "true"}),
					nsWithLabels("ns-other", map[string]string{"team": "other"}),
				).
				WithStatusSubresource(&quotav1alpha1.ClusterResourceQuota{}).
				Build()
			r := newReconciler(c)
			r.ExcludeNamespaceLabelKey = excludeKey

			_, err := r.Reconcile(ctx, req)
			Expect(err).NotTo(HaveOccurred())

			updated := &quotav1alpha1.ClusterResourceQuota{}
			Expect(c.Get(ctx, req.NamespacedName, updated)).To(Succeed())
			Expect(updated.Status.Namespaces).To(HaveLen(1))
			Expect(updated.Status.Namespaces[0].Namespace).To(Equal("ns-a"))
			Expect(updated.Status.Total.Hard).To(HaveKey(corev1.ResourceRequestsCPU))
			Expect(rec.events).To(ContainElement("Normal/NamespaceAdded"))
		})
	})

	Describe("calculateAndAggregateUsage", func() {
		It("propagates errors from listing namespace resources", func() {
			crq := &quotav1alpha1.ClusterResourceQuota{
				ObjectMeta: metav1.ObjectMeta{Name: "q"},
				Spec: quotav1alpha1.ClusterResourceQuotaSpec{
					Hard: quotav1alpha1.ResourceList{corev1.ResourceRequestsCPU: resource.MustParse("1")},
				},
			}
			errClient := interceptor.NewClient(fake.NewClientBuilder().Build(), interceptor.Funcs{
				List: func(_ context.Context, _ client.WithWatch, list client.ObjectList, _ ...client.ListOption) error {
					if _, ok := list.(*corev1.PodList); ok {
						return errors.New("pod list boom")
					}
					return nil
				},
			})
			r := newReconciler(errClient)

			_, _, err := r.calculateAndAggregateUsage(ctx, crq, []string{"ns-a"})
			Expect(err).To(HaveOccurred())
		})
	})

	Describe("calculateObjectCount", func() {
		It("returns the count for a supported object-count resource", func() {
			c := fake.NewClientBuilder().
				WithObjects(&corev1.ConfigMap{ObjectMeta: metav1.ObjectMeta{Name: "cm1", Namespace: "ns-a"}}).
				Build()
			r := newReconciler(c)
			r.ObjectCountCalculator = objectcount.NewObjectCountCalculator(c, logger)

			q, err := r.calculateObjectCount(ctx, "ns-a", usage.ResourceConfigMaps)
			Expect(err).NotTo(HaveOccurred())
			Expect(q.Value()).To(Equal(int64(1)))
		})

		It("returns an error when the object-count calculator fails", func() {
			errClient := interceptor.NewClient(fake.NewClientBuilder().Build(), interceptor.Funcs{
				List: func(_ context.Context, _ client.WithWatch, _ client.ObjectList, _ ...client.ListOption) error {
					return errors.New("configmap list boom")
				},
			})
			r := newReconciler(errClient)
			r.ObjectCountCalculator = objectcount.NewObjectCountCalculator(errClient, logger)

			_, err := r.calculateObjectCount(ctx, "ns-a", usage.ResourceConfigMaps)
			Expect(err).To(HaveOccurred())
		})
	})

	Describe("findQuotasForObject", func() {
		It("returns nil for a cluster-scoped object", func() {
			r := newReconciler(&fakeClient{})
			Expect(r.findQuotasForObject(ctx, &corev1.PersistentVolume{})).To(BeNil())
		})

		It("returns nil when the namespace lookup fails", func() {
			r := newReconciler(&fakeClient{
				getFunc: func(_ context.Context, _ client.ObjectKey, _ client.Object) error {
					return errors.New("namespace get boom")
				},
			})
			obj := &corev1.Pod{ObjectMeta: metav1.ObjectMeta{Name: "p", Namespace: "x"}}
			Expect(r.findQuotasForObject(ctx, obj)).To(BeNil())
		})
	})
})
