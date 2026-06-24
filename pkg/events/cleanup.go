package events

import (
	"context"
	"sort"
	"time"

	"go.uber.org/zap"
	eventsv1 "k8s.io/api/events/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"sigs.k8s.io/controller-runtime/pkg/client"

	"github.com/powerhome/pac-quota-controller/pkg/metrics"
)

// eventTime returns the most recent observation time for an Event, falling
// back through Series.LastObservedTime, EventTime, and the deprecated
// LastTimestamp/FirstTimestamp fields for events translated from core/v1.
func eventTime(e *eventsv1.Event) time.Time {
	if e.Series != nil && !e.Series.LastObservedTime.IsZero() {
		return e.Series.LastObservedTime.Time
	}
	if !e.EventTime.IsZero() {
		return e.EventTime.Time
	}
	if !e.DeprecatedLastTimestamp.IsZero() {
		return e.DeprecatedLastTimestamp.Time
	}
	return e.DeprecatedFirstTimestamp.Time
}

const (
	// crqEventKind is the Regarding kind on every event the controller records.
	crqEventKind = "ClusterResourceQuota"
)

// CleanupConfig holds configuration for event cleanup
type CleanupConfig struct {
	// MaxAge is the maximum age for events before cleanup
	MaxAge time.Duration
	// MaxEventsPerCRQ is the maximum number of events to keep per CRQ
	MaxEventsPerCRQ int
	// CleanupInterval is how often to run cleanup
	CleanupInterval time.Duration
	// Enabled controls whether cleanup is active
	Enabled bool
}

// DefaultCleanupConfig returns default cleanup configuration
func DefaultCleanupConfig() CleanupConfig {
	return CleanupConfig{
		MaxAge:          24 * time.Hour, // Keep events for 24 hours
		MaxEventsPerCRQ: 100,            // Keep max 100 events per CRQ
		CleanupInterval: 1 * time.Hour,  // Run cleanup every hour
		Enabled:         true,
	}
}

// EventCleanupManager manages automatic cleanup of PAC quota events
type EventCleanupManager struct {
	client client.Client
	config CleanupConfig
	logger *zap.Logger
}

// NewEventCleanupManager creates a new cleanup manager
func NewEventCleanupManager(k8sClient client.Client, config CleanupConfig, logger *zap.Logger) *EventCleanupManager {
	if logger == nil {
		logger = zap.NewNop()
	}
	return &EventCleanupManager{
		client: k8sClient,
		config: config,
		logger: logger.Named("event-cleanup"),
	}
}

// Start begins the cleanup process
func (m *EventCleanupManager) Start(ctx context.Context) {
	if !m.config.Enabled {
		m.logger.Info("Event cleanup disabled")
		return
	}

	m.logger.Info("Starting event cleanup manager",
		zap.Duration("interval", m.config.CleanupInterval),
		zap.Duration("max_age", m.config.MaxAge),
		zap.Int("max_events_per_crq", m.config.MaxEventsPerCRQ))

	ticker := time.NewTicker(m.config.CleanupInterval)
	defer ticker.Stop()

	// Run initial cleanup
	if err := m.cleanup(ctx); err != nil {
		m.logger.Error("Failed initial event cleanup", zap.Error(err))
	}

	for {
		select {
		case <-ctx.Done():
			m.logger.Info("Event cleanup manager stopping")
			return
		case <-ticker.C:
			if err := m.cleanup(ctx); err != nil {
				m.logger.Error("Failed to cleanup events", zap.Error(err))
			}
		}
	}
}

// cleanup performs the actual event cleanup
func (m *EventCleanupManager) cleanup(ctx context.Context) error {
	allEvents, err := m.getPACEvents(ctx)
	if err != nil {
		return err
	}

	if len(allEvents) == 0 {
		m.logger.Debug("No PAC quota events found for cleanup")
		return nil
	}

	// Group events by the CRQ they were recorded against.
	eventsByCRQ := make(map[string][]eventsv1.Event)
	for _, event := range allEvents {
		eventsByCRQ[event.Regarding.Name] = append(eventsByCRQ[event.Regarding.Name], event)
	}

	var deletedCount int
	cutoff := time.Now().Add(-m.config.MaxAge)

	// Process each CRQ's events
	for crqName, crqEvents := range eventsByCRQ {
		deleted := m.cleanupEventsForCRQ(ctx, crqName, crqEvents, cutoff)
		deletedCount += deleted
	}

	if deletedCount > 0 {
		m.logger.Info("Cleaned up old events", zap.Int("count", deletedCount))
	}

	return nil
}

// getPACEvents lists every event the controller recorded against a CRQ.
func (m *EventCleanupManager) getPACEvents(ctx context.Context) ([]eventsv1.Event, error) {
	list := &eventsv1.EventList{}
	if err := m.client.List(ctx, list, client.InNamespace(metav1.NamespaceDefault)); err != nil {
		return nil, err
	}

	pac := make([]eventsv1.Event, 0, len(list.Items))
	for i := range list.Items {
		if list.Items[i].Regarding.Kind == crqEventKind {
			pac = append(pac, list.Items[i])
		}
	}
	return pac, nil
}

// cleanupEventsForCRQ cleans up events for a specific CRQ
func (m *EventCleanupManager) cleanupEventsForCRQ(ctx context.Context, crqName string,
	events []eventsv1.Event, cutoff time.Time) int {

	var toDelete []eventsv1.Event
	var validEvents []eventsv1.Event

	// First pass: remove events older than MaxAge
	for _, event := range events {
		if eventTime(&event).Before(cutoff) {
			toDelete = append(toDelete, event)
		} else {
			validEvents = append(validEvents, event)
		}
	}

	// Second pass: if we still have too many events, keep only the most recent
	if len(validEvents) > m.config.MaxEventsPerCRQ {
		sort.Slice(validEvents, func(i, j int) bool {
			return eventTime(&validEvents[i]).After(eventTime(&validEvents[j]))
		})

		// Mark excess events for deletion
		excess := validEvents[m.config.MaxEventsPerCRQ:]
		toDelete = append(toDelete, excess...)
	}

	// Delete the events
	deletedCount := 0
	for _, event := range toDelete {
		if err := m.client.Delete(ctx, &event); err != nil {
			m.logger.Error("Failed to delete event",
				zap.Error(err),
				zap.String("event", event.Name),
				zap.String("crq_name", crqName),
				zap.String("reason", event.Reason))
		} else {
			deletedCount++
			metrics.EventsCleanedTotal.Inc()
			m.logger.Debug("Deleted old event",
				zap.String("event", event.Name),
				zap.String("crq_name", crqName),
				zap.String("reason", event.Reason),
				zap.Duration("age", time.Since(eventTime(&event))))
		}
	}

	if deletedCount > 0 {
		m.logger.Debug("Cleaned up events for CRQ",
			zap.String("crq_name", crqName),
			zap.Int("deleted_count", deletedCount),
			zap.Int("remaining_count", len(events)-deletedCount))
	}

	return deletedCount
}

// GetCleanupStats returns statistics about the cleanup operation (for testing/monitoring)
func (m *EventCleanupManager) GetCleanupStats(ctx context.Context) (map[string]int, error) {
	allEvents, err := m.getPACEvents(ctx)
	if err != nil {
		return nil, err
	}

	crqCounts := make(map[string]int)
	for _, event := range allEvents {
		crqCounts[event.Regarding.Name]++
	}

	return map[string]int{
		"total_events":     len(allEvents),
		"crqs_with_events": len(crqCounts),
	}, nil
}
