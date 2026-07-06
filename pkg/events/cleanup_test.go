package events

import (
	"context"
	"time"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	promtestutil "github.com/prometheus/client_golang/prometheus/testutil"
	"go.uber.org/zap"
	corev1 "k8s.io/api/core/v1"
	eventsv1 "k8s.io/api/events/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/types"
	"sigs.k8s.io/controller-runtime/pkg/client"
	clientfake "sigs.k8s.io/controller-runtime/pkg/client/fake"

	"github.com/powerhome/pac-quota-controller/pkg/metrics"
)

func newCleanupTestScheme() *runtime.Scheme {
	s := runtime.NewScheme()
	_ = eventsv1.AddToScheme(s)
	return s
}

// makePACEvent builds an event as the controller records it: regarding the CRQ,
// with no PAC labels (the events.k8s.io recorder does not set any).
func makePACEvent(name, crq string, at time.Time) *eventsv1.Event {
	return &eventsv1.Event{
		ObjectMeta: metav1.ObjectMeta{Name: name, Namespace: "default"},
		Regarding:  corev1.ObjectReference{Kind: crqEventKind, Name: crq},
		EventTime:  metav1.MicroTime{Time: at},
		Reason:     "QuotaExceeded",
		Type:       "Warning",
		Note:       "test event",
	}
}

var _ = Describe("EventCleanupManager.cleanup", func() {
	var (
		ctx    context.Context
		logger *zap.Logger
	)

	BeforeEach(func() {
		ctx = context.Background()
		logger = zap.NewNop()
	})

	assertGone := func(fc client.Client, name string) {
		err := fc.Get(ctx, types.NamespacedName{Name: name, Namespace: "default"}, &eventsv1.Event{})
		Expect(client.IgnoreNotFound(err)).To(Succeed())
		Expect(err).To(HaveOccurred(), "expected %s to be deleted", name)
	}
	assertExists := func(fc client.Client, name string) {
		Expect(fc.Get(ctx, types.NamespacedName{Name: name, Namespace: "default"}, &eventsv1.Event{})).
			To(Succeed(), "expected %s to be kept", name)
	}

	It("deletes expired CRQ events and counts them, keeping fresh ones", func() {
		old := time.Now().Add(-48 * time.Hour) // older than MaxAge

		fc := clientfake.NewClientBuilder().
			WithScheme(newCleanupTestScheme()).
			WithObjects(
				makePACEvent("evt-old-1", "quota-a", old),
				makePACEvent("evt-old-2", "quota-a", old),
				makePACEvent("evt-fresh", "quota-a", time.Now()),
			).
			Build()

		mgr := NewEventCleanupManager(fc, CleanupConfig{
			MaxAge:          24 * time.Hour,
			MaxEventsPerCRQ: 100,
			CleanupInterval: time.Hour,
			Enabled:         true,
		}, logger)

		pre := promtestutil.ToFloat64(metrics.EventsCleanedTotal)
		Expect(mgr.cleanup(ctx)).To(Succeed())

		assertGone(fc, "evt-old-1")
		assertGone(fc, "evt-old-2")
		assertExists(fc, "evt-fresh")
		Expect(promtestutil.ToFloat64(metrics.EventsCleanedTotal) - pre).
			To(Equal(float64(2)))
	})

	It("trims to MaxEventsPerCRQ, keeping the newest events", func() {
		now := time.Now()

		fc := clientfake.NewClientBuilder().
			WithScheme(newCleanupTestScheme()).
			WithObjects(
				makePACEvent("evt-oldest", "quota-b", now.Add(-3*time.Hour)),
				makePACEvent("evt-middle", "quota-b", now.Add(-2*time.Hour)),
				makePACEvent("evt-newest", "quota-b", now.Add(-1*time.Hour)),
			).
			Build()

		mgr := NewEventCleanupManager(fc, CleanupConfig{
			MaxAge:          24 * time.Hour, // none are expired by age
			MaxEventsPerCRQ: 2,
			CleanupInterval: time.Hour,
			Enabled:         true,
		}, logger)

		Expect(mgr.cleanup(ctx)).To(Succeed())

		assertGone(fc, "evt-oldest")
		assertExists(fc, "evt-middle")
		assertExists(fc, "evt-newest")
	})

	It("groups by CRQ so one CRQ's trim does not affect another", func() {
		now := time.Now()

		fc := clientfake.NewClientBuilder().
			WithScheme(newCleanupTestScheme()).
			WithObjects(
				makePACEvent("a-1", "quota-a", now.Add(-2*time.Hour)),
				makePACEvent("a-2", "quota-a", now.Add(-1*time.Hour)),
				makePACEvent("b-1", "quota-b", now.Add(-1*time.Hour)),
			).
			Build()

		mgr := NewEventCleanupManager(fc, CleanupConfig{
			MaxAge:          24 * time.Hour,
			MaxEventsPerCRQ: 1,
			CleanupInterval: time.Hour,
			Enabled:         true,
		}, logger)

		Expect(mgr.cleanup(ctx)).To(Succeed())

		assertGone(fc, "a-1") // quota-a trimmed to its newest
		assertExists(fc, "a-2")
		assertExists(fc, "b-1") // quota-b untouched (only one event)
	})
})
