package metrics

import "testing"

// RegisterWebhookMetrics uses MustRegister, which panics on duplicate registration.
// registerOnce must make repeated calls safe.
func TestRegisterWebhookMetricsIdempotent(t *testing.T) {
	defer func() {
		if r := recover(); r != nil {
			t.Fatalf("RegisterWebhookMetrics panicked on repeat call: %v", r)
		}
	}()
	RegisterWebhookMetrics()
	RegisterWebhookMetrics()
	RegisterWebhookMetrics()
}

// These calls only need to compile. If someone re-adds a per-request `namespace`
// label to a webhook metric, the extra argument fails at compile time — that
// label multiplies into tens of thousands of series on clusters with many
// ephemeral per-PR preview namespaces, so re-adding it must be a deliberate,
// discussed change, not an incidental one.
func TestWebhookMetricLabelsExcludeNamespace(t *testing.T) {
	WebhookValidationCount.WithLabelValues("clusterresourcequota", "CREATE").Inc()
	WebhookValidationDuration.WithLabelValues("clusterresourcequota", "CREATE").Observe(0)
	WebhookAdmissionDecision.WithLabelValues("clusterresourcequota", "CREATE", "allowed").Inc()
}
