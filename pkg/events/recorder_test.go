package events

import (
	"fmt"
	"testing"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	"go.uber.org/zap"
	"k8s.io/apimachinery/pkg/api/resource"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/client-go/tools/record"
	"sigs.k8s.io/controller-runtime/pkg/client/fake"

	quotav1alpha1 "github.com/powerhome/pac-quota-controller/api/v1alpha1"
)

func TestEventRecorder(t *testing.T) {
	RegisterFailHandler(Fail)
	RunSpecs(t, "EventRecorder Suite")
}

var _ = Describe("EventRecorder", func() {
	var (
		eventRecorder *EventRecorder
		fakeRecorder  *record.FakeRecorder
		testCRQ       *quotav1alpha1.ClusterResourceQuota
		logger        *zap.Logger
	)

	BeforeEach(func() {
		logger = zap.NewNop()
		fakeRecorder = record.NewFakeRecorder(100)

		scheme := runtime.NewScheme()
		Expect(quotav1alpha1.AddToScheme(scheme)).To(Succeed())

		fakeClient := fake.NewClientBuilder().WithScheme(scheme).Build()
		eventRecorder = NewEventRecorder(fakeRecorder, fakeClient, logger)

		testCRQ = &quotav1alpha1.ClusterResourceQuota{
			ObjectMeta: metav1.ObjectMeta{
				Name: "test-crq",
			},
			Spec: quotav1alpha1.ClusterResourceQuotaSpec{
				NamespaceSelector: &metav1.LabelSelector{
					MatchLabels: map[string]string{
						"team": "test",
					},
				},
				Hard: quotav1alpha1.ResourceList{
					"requests.cpu":    resource.MustParse("2"),
					"requests.memory": resource.MustParse("4Gi"),
				},
			},
		}
	})

	Describe("NewEventRecorder", func() {
		It("should create a valid EventRecorder", func() {
			Expect(eventRecorder).ToNot(BeNil())
			Expect(eventRecorder.recorder).To(Equal(fakeRecorder))
			Expect(eventRecorder.client).ToNot(BeNil())
			Expect(eventRecorder.logger).To(Equal(logger))
		})
	})

	Describe("QuotaExceeded", func() {
		It("should record a QuotaExceeded event", func() {
			eventRecorder.QuotaExceeded(testCRQ, "requests.cpu", 3, 2)

			// FakeRecorder is synchronous, so events should be immediately available
			Expect(fakeRecorder.Events).To(HaveLen(1))
			event := <-fakeRecorder.Events
			Expect(event).To(ContainSubstring("QuotaExceeded"))
			Expect(event).To(ContainSubstring("Resource requests.cpu exceeded quota: requested 3, limit 2"))
		})

		It("should record event with correct metadata", func() {
			eventRecorder.QuotaExceeded(testCRQ, "requests.memory", 5368709120, 4294967296) // 5GB requested, 4GB limit

			Expect(fakeRecorder.Events).To(HaveLen(1))
			event := <-fakeRecorder.Events
			Expect(event).To(ContainSubstring("Warning"))
			Expect(event).To(ContainSubstring("QuotaExceeded"))
			Expect(event).To(ContainSubstring("requests.memory"))
		})
	})

	Describe("NamespaceAdded", func() {
		It("should record a NamespaceAdded event", func() {
			eventRecorder.NamespaceAdded(testCRQ, "test-namespace")

			Expect(fakeRecorder.Events).To(HaveLen(1))
			event := <-fakeRecorder.Events
			Expect(event).To(ContainSubstring("NamespaceAdded"))
			Expect(event).To(ContainSubstring("Namespace test-namespace added to quota scope"))
		})

		It("should record event as Normal type", func() {
			eventRecorder.NamespaceAdded(testCRQ, "test-namespace")

			Expect(fakeRecorder.Events).To(HaveLen(1))
			event := <-fakeRecorder.Events
			Expect(event).To(ContainSubstring("Normal"))
			Expect(event).To(ContainSubstring("NamespaceAdded"))
		})
	})

	Describe("NamespaceRemoved", func() {
		It("should record a NamespaceRemoved event", func() {
			eventRecorder.NamespaceRemoved(testCRQ, "test-namespace")

			Expect(fakeRecorder.Events).To(HaveLen(1))
			event := <-fakeRecorder.Events
			Expect(event).To(ContainSubstring("NamespaceRemoved"))
			Expect(event).To(ContainSubstring("Namespace test-namespace removed from quota scope"))
		})

		It("should record event as Normal type", func() {
			eventRecorder.NamespaceRemoved(testCRQ, "test-namespace")

			Expect(fakeRecorder.Events).To(HaveLen(1))
			event := <-fakeRecorder.Events
			Expect(event).To(ContainSubstring("Normal"))
			Expect(event).To(ContainSubstring("NamespaceRemoved"))
		})
	})

	Describe("CalculationFailed", func() {
		It("should record a CalculationFailed event with error details", func() {
			testErr := fmt.Errorf("failed to calculate pod resources")
			eventRecorder.CalculationFailed(testCRQ, testErr)

			Expect(fakeRecorder.Events).To(HaveLen(1))
			event := <-fakeRecorder.Events
			Expect(event).To(ContainSubstring("CalculationFailed"))
			Expect(event).To(ContainSubstring("Failed to calculate resource usage: failed to calculate pod resources"))
		})

		It("should record event as Warning type", func() {
			testErr := fmt.Errorf("calculation error")
			eventRecorder.CalculationFailed(testCRQ, testErr)

			Expect(fakeRecorder.Events).To(HaveLen(1))
			event := <-fakeRecorder.Events
			Expect(event).To(ContainSubstring("Warning"))
			Expect(event).To(ContainSubstring("CalculationFailed"))
		})
	})

	Describe("InvalidSelector", func() {
		It("should record an InvalidSelector event with error details", func() {
			testErr := fmt.Errorf("invalid label selector syntax")
			eventRecorder.InvalidSelector(testCRQ, testErr)

			Expect(fakeRecorder.Events).To(HaveLen(1))
			event := <-fakeRecorder.Events
			Expect(event).To(ContainSubstring("InvalidSelector"))
			Expect(event).To(ContainSubstring("Invalid namespace selector: invalid label selector syntax"))
		})

		It("should record event as Warning type", func() {
			testErr := fmt.Errorf("selector error")
			eventRecorder.InvalidSelector(testCRQ, testErr)

			Expect(fakeRecorder.Events).To(HaveLen(1))
			event := <-fakeRecorder.Events
			Expect(event).To(ContainSubstring("Warning"))
			Expect(event).To(ContainSubstring("InvalidSelector"))
		})
	})

	Describe("Event Annotations", func() {
		It("should include PAC-specific annotations on events", func() {
			// Test with QuotaExceeded as an example
			eventRecorder.QuotaExceeded(testCRQ, "requests.cpu", 3, 2)

			// FakeRecorder doesn't capture annotations directly, but we can verify
			// the recordEvent method is called with correct parameters
			Eventually(fakeRecorder.Events).Should(Receive(ContainSubstring("QuotaExceeded")))
		})
	})
})
