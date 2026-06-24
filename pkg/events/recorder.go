package events

import (
	"fmt"

	"go.uber.org/zap"
	"k8s.io/apimachinery/pkg/api/resource"
	"k8s.io/client-go/tools/events"

	quotav1alpha1 "github.com/powerhome/pac-quota-controller/api/v1alpha1"
)

const (
	// Event reasons for ClusterResourceQuota
	ReasonQuotaExceeded     = "QuotaExceeded"
	ReasonNamespaceAdded    = "NamespaceAdded"
	ReasonNamespaceRemoved  = "NamespaceRemoved"
	ReasonCalculationFailed = "CalculationFailed"
	ReasonInvalidSelector   = "InvalidSelector"

	// Event types
	EventTypeNormal  = "Normal"
	EventTypeWarning = "Warning"
)

// EventRecorder wraps the Kubernetes event recorder with PAC-specific functionality
type EventRecorder struct {
	recorder events.EventRecorder
	logger   *zap.Logger
}

// NewEventRecorder creates a new EventRecorder
func NewEventRecorder(recorder events.EventRecorder, logger *zap.Logger) *EventRecorder {
	if logger == nil {
		logger = zap.NewNop()
	}
	return &EventRecorder{
		recorder: recorder,
		logger:   logger.Named("event-recorder"),
	}
}

// QuotaExceeded records an event when quota is exceeded
func (r *EventRecorder) QuotaExceeded(crq *quotav1alpha1.ClusterResourceQuota, resourceExceeded string,
	requested, limit resource.Quantity) {
	message := fmt.Sprintf("Resource %s has exceeded quota: current %s, limit %s",
		resourceExceeded, requested.String(), limit.String())
	r.recordEvent(crq, EventTypeWarning, ReasonQuotaExceeded, message)
}

// NamespaceAdded records an event when a namespace enters quota scope
func (r *EventRecorder) NamespaceAdded(crq *quotav1alpha1.ClusterResourceQuota, namespace string) {
	message := fmt.Sprintf("Namespace %s added to quota scope", namespace)
	r.recordEvent(crq, EventTypeNormal, ReasonNamespaceAdded, message)
}

// NamespaceRemoved records an event when a namespace leaves quota scope
func (r *EventRecorder) NamespaceRemoved(crq *quotav1alpha1.ClusterResourceQuota, namespace string) {
	message := fmt.Sprintf("Namespace %s removed from quota scope", namespace)
	r.recordEvent(crq, EventTypeNormal, ReasonNamespaceRemoved, message)
}

// CalculationFailed records an event when resource calculation fails
func (r *EventRecorder) CalculationFailed(crq *quotav1alpha1.ClusterResourceQuota, err error) {
	message := fmt.Sprintf("Failed to calculate resource usage: %v", err)
	r.recordEvent(crq, EventTypeWarning, ReasonCalculationFailed, message)
}

// InvalidSelector records an event when namespace selector is invalid
func (r *EventRecorder) InvalidSelector(crq *quotav1alpha1.ClusterResourceQuota, err error) {
	message := fmt.Sprintf("Invalid namespace selector: %v", err)
	r.recordEvent(crq, EventTypeWarning, ReasonInvalidSelector, message)
}

// recordEvent records an event with PAC-specific labels using the current pod as the event target
func (r *EventRecorder) recordEvent(crq *quotav1alpha1.ClusterResourceQuota,
	eventType, reason, message string) {

	r.recorder.Eventf(crq, nil, eventType, reason, reason, message)
}
