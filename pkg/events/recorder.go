package events

import (
	"fmt"
	"time"

	"go.uber.org/zap"
	"k8s.io/client-go/tools/record"
	"sigs.k8s.io/controller-runtime/pkg/client"

	quotav1alpha1 "github.com/powerhome/pac-quota-controller/api/v1alpha1"
)

const (
	// Event reasons for ClusterResourceQuota
	ReasonQuotaExceeded     = "QuotaExceeded"
	ReasonNamespaceAdded    = "NamespaceAdded"
	ReasonNamespaceRemoved  = "NamespaceRemoved"
	ReasonQuotaReconciled   = "QuotaReconciled"
	ReasonCalculationFailed = "CalculationFailed"
	ReasonInvalidSelector   = "InvalidSelector"

	// Event types
	EventTypeNormal  = "Normal"
	EventTypeWarning = "Warning"

	// Event labels for identification and cleanup
	LabelEventSource = "quota.pac.io/event-source"
	LabelEventType   = "quota.pac.io/event-type"
	LabelCRQName     = "quota.pac.io/crq-name"

	// Backoff configuration
	InitialBackoffSeconds = 30
	MaxBackoffSeconds     = 900 // 15 minutes
	BackoffMultiplier     = 2
)

// EventRecorder wraps the Kubernetes event recorder with PAC-specific functionality
type EventRecorder struct {
	recorder       record.EventRecorder
	client         client.Client
	violationCache map[string]*ViolationTracker
	logger         *zap.Logger
}

// ViolationTracker tracks webhook violations for exponential backoff
type ViolationTracker struct {
	LastEvent      time.Time
	Count          int
	NextAllowedAt  time.Time
	BackoffSeconds int
}

// NewEventRecorder creates a new EventRecorder
func NewEventRecorder(recorder record.EventRecorder, k8sClient client.Client, logger *zap.Logger) *EventRecorder {
	return &EventRecorder{
		recorder:       recorder,
		client:         k8sClient,
		violationCache: make(map[string]*ViolationTracker),
		logger:         logger,
	}
}

// QuotaExceeded records an event when quota is exceeded
func (r *EventRecorder) QuotaExceeded(crq *quotav1alpha1.ClusterResourceQuota, resource string,
	requested, limit int64) {
	message := fmt.Sprintf("Resource %s exceeded quota: requested %d, limit %d",
		resource, requested, limit)
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

// QuotaReconciled records a successful reconciliation
func (r *EventRecorder) QuotaReconciled(crq *quotav1alpha1.ClusterResourceQuota, namespacesCount int) {
	message := fmt.Sprintf("Quota reconciled successfully across %d namespaces", namespacesCount)
	r.recordEvent(crq, EventTypeNormal, ReasonQuotaReconciled, message)
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

// recordEvent records an event with PAC-specific labels and uses the standard recorder
func (r *EventRecorder) recordEvent(crq *quotav1alpha1.ClusterResourceQuota,
	eventType, reason, message string) {

	// Use the standard recorder which handles deduplication and proper event management
	r.recorder.AnnotatedEventf(crq, map[string]string{
		LabelEventSource: "controller",
		LabelEventType:   reason,
		LabelCRQName:     crq.Name,
	}, eventType, reason, message)
}

// CleanupExpiredViolations removes old violation tracking entries
func (r *EventRecorder) CleanupExpiredViolations() {
	cutoff := time.Now().Add(-1 * time.Hour) // Clean entries older than 1 hour

	for key, tracker := range r.violationCache {
		if tracker.LastEvent.Before(cutoff) {
			delete(r.violationCache, key)
		}
	}
}

// GetViolationStats returns statistics about violation tracking (for testing/debugging)
func (r *EventRecorder) GetViolationStats() map[string]ViolationTracker {
	stats := make(map[string]ViolationTracker)
	for key, tracker := range r.violationCache {
		stats[key] = *tracker
	}
	return stats
}
