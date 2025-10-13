package events

import (
	"fmt"
	"os"
	"time"

	"go.uber.org/zap"
	"k8s.io/client-go/tools/record"

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
	violationCache map[string]*ViolationTracker
	logger         *zap.Logger
	namespace      string
	podName        string
}

// ViolationTracker tracks webhook violations for exponential backoff
type ViolationTracker struct {
	LastEvent      time.Time
	Count          int
	NextAllowedAt  time.Time
	BackoffSeconds int
}

// NewEventRecorder creates a new EventRecorder
func NewEventRecorder(
	recorder record.EventRecorder,
	namespace string,
	logger *zap.Logger) *EventRecorder {
	return &EventRecorder{
		recorder:       recorder,
		violationCache: make(map[string]*ViolationTracker),
		logger:         logger,
		namespace:      namespace,
		podName:        getPodName(), // Get current pod name
	}
}

// getPodName gets the current pod name from environment or hostname
func getPodName() string {
	// Try to get pod name from downward API environment variable first
	if podName := os.Getenv("POD_NAME"); podName != "" {
		return podName
	}

	// Fallback to hostname (which is usually the pod name in Kubernetes)
	if hostname, err := os.Hostname(); err == nil {
		return hostname
	}

	// Final fallback
	return "pac-quota-controller-manager"
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

	// AnnotatedEventf uses the object NS for event creation
	r.recorder.Eventf(crq, eventType, reason, message)
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
