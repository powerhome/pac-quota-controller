package events

import (
	"context"
	"time"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	"go.uber.org/zap"
	eventsv1 "k8s.io/api/events/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/types"
	"sigs.k8s.io/controller-runtime/pkg/client"
	clientfake "sigs.k8s.io/controller-runtime/pkg/client/fake"
)

func newCleanupTestScheme() *runtime.Scheme {
	s := runtime.NewScheme()
	_ = eventsv1.AddToScheme(s)
	return s
}

func makePACEvent(name, source, crq string, at time.Time) *eventsv1.Event {
	return &eventsv1.Event{
		ObjectMeta: metav1.ObjectMeta{
			Name:      name,
			Namespace: "kube-system",
			Labels: map[string]string{
				LabelEventSource: source,
				LabelCRQName:     crq,
			},
		},
		EventTime: metav1.MicroTime{Time: at},
		Reason:    "QuotaExceeded",
		Type:      "Warning",
		Note:      "test event",
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

	It("deletes expired events from both controller and webhook sources", func() {
		old := time.Now().Add(-48 * time.Hour) // older than MaxAge

		ctrlEvent := makePACEvent("evt-ctrl", "controller", "quota-a", old)
		webhookEvent := makePACEvent("evt-webhook", "webhook", "quota-a", old)

		fc := clientfake.NewClientBuilder().
			WithScheme(newCleanupTestScheme()).
			WithObjects(ctrlEvent, webhookEvent).
			Build()

		mgr := NewEventCleanupManager(fc, CleanupConfig{
			MaxAge:          24 * time.Hour,
			MaxEventsPerCRQ: 100,
			CleanupInterval: time.Hour,
			Enabled:         true,
		}, logger)

		Expect(mgr.cleanup(ctx)).To(Succeed())

		assertGone := func(name string) {
			err := fc.Get(ctx, types.NamespacedName{Name: name, Namespace: "kube-system"}, &eventsv1.Event{})
			Expect(client.IgnoreNotFound(err)).To(Succeed())
			Expect(err).To(HaveOccurred(), "expected %s to be deleted", name)
		}
		assertGone("evt-ctrl")
		assertGone("evt-webhook")
	})
})
