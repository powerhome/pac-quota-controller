package events

import (
	"context"
	"sort"
	"time"

	"go.uber.org/zap"
	corev1 "k8s.io/api/core/v1"
	"sigs.k8s.io/controller-runtime/pkg/client"
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
	return &EventCleanupManager{
		client: k8sClient,
		config: config,
		logger: logger,
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
		zap.Duration("maxAge", m.config.MaxAge),
		zap.Int("maxEventsPerCRQ", m.config.MaxEventsPerCRQ))

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
	// Find all PAC quota events from controller
	controllerEvents, err := m.getPACEvents(ctx)
	if err != nil {
		return err
	}

	// Find all PAC quota events from webhook
	webhookEvents, err := m.getPACEvents(ctx)
	if err != nil {
		return err
	}

	// Combine all events
	allEvents := append(controllerEvents.Items, webhookEvents.Items...)

	if len(allEvents) == 0 {
		m.logger.Debug("No PAC quota events found for cleanup")
		return nil
	}

	// Group events by CRQ
	eventsByCRQ := make(map[string][]corev1.Event)
	for _, event := range allEvents {
		crqName := event.Labels[LabelCRQName]
		if crqName == "" {
			// Skip events without CRQ label (shouldn't happen with our events)
			continue
		}
		eventsByCRQ[crqName] = append(eventsByCRQ[crqName], event)
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

// getPACEvents retrieves PAC quota events by source
func (m *EventCleanupManager) getPACEvents(ctx context.Context) (*corev1.EventList, error) {
	events := &corev1.EventList{}
	listOpts := []client.ListOption{
		client.MatchingLabels{LabelEventSource: "controller"},
	}

	if err := m.client.List(ctx, events, listOpts...); err != nil {
		return nil, err
	}

	return events, nil
}

// cleanupEventsForCRQ cleans up events for a specific CRQ
func (m *EventCleanupManager) cleanupEventsForCRQ(ctx context.Context, crqName string,
	events []corev1.Event, cutoff time.Time) int {

	var toDelete []corev1.Event
	var validEvents []corev1.Event

	// First pass: remove events older than MaxAge
	for _, event := range events {
		eventTime := event.LastTimestamp.Time
		if eventTime.IsZero() {
			// Fallback to FirstTimestamp if LastTimestamp is not set
			eventTime = event.FirstTimestamp.Time
		}

		if eventTime.Before(cutoff) {
			toDelete = append(toDelete, event)
		} else {
			validEvents = append(validEvents, event)
		}
	}

	// Second pass: if we still have too many events, keep only the most recent
	if len(validEvents) > m.config.MaxEventsPerCRQ {
		// Sort by timestamp (most recent first)
		sort.Slice(validEvents, func(i, j int) bool {
			timeI := validEvents[i].LastTimestamp.Time
			if timeI.IsZero() {
				timeI = validEvents[i].FirstTimestamp.Time
			}
			timeJ := validEvents[j].LastTimestamp.Time
			if timeJ.IsZero() {
				timeJ = validEvents[j].FirstTimestamp.Time
			}
			return timeI.After(timeJ)
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
				zap.String("crq", crqName),
				zap.String("reason", event.Reason))
		} else {
			deletedCount++
			m.logger.Debug("Deleted old event",
				zap.String("event", event.Name),
				zap.String("crq", crqName),
				zap.String("reason", event.Reason),
				zap.Duration("age", time.Since(event.LastTimestamp.Time)))
		}
	}

	if deletedCount > 0 {
		m.logger.Debug("Cleaned up events for CRQ",
			zap.String("crq", crqName),
			zap.Int("deletedCount", deletedCount),
			zap.Int("remainingCount", len(events)-deletedCount))
	}

	return deletedCount
}

// GetCleanupStats returns statistics about the cleanup operation (for testing/monitoring)
func (m *EventCleanupManager) GetCleanupStats(ctx context.Context) (map[string]int, error) {
	stats := make(map[string]int)

	// Count controller events
	controllerEvents, err := m.getPACEvents(ctx)
	if err != nil {
		return nil, err
	}
	stats["controller_events"] = len(controllerEvents.Items)

	// Count webhook events
	webhookEvents, err := m.getPACEvents(ctx)
	if err != nil {
		return nil, err
	}
	stats["webhook_events"] = len(webhookEvents.Items)

	// Count by CRQ
	allEvents := append(controllerEvents.Items, webhookEvents.Items...)
	crqCounts := make(map[string]int)
	for _, event := range allEvents {
		crqName := event.Labels[LabelCRQName]
		if crqName != "" {
			crqCounts[crqName]++
		}
	}
	stats["total_events"] = len(allEvents)
	stats["crqs_with_events"] = len(crqCounts)

	return stats, nil
}
