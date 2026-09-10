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

// Guards against re-adding a namespace label to webhook metrics.
func TestWebhookMetricLabelsExcludeNamespace(t *testing.T) {
	WebhookValidationCount.WithLabelValues("clusterresourcequota", "CREATE").Inc()
	WebhookValidationDuration.WithLabelValues("clusterresourcequota", "CREATE").Observe(0)
	WebhookAdmissionDecision.WithLabelValues("clusterresourcequota", "CREATE", "allowed").Inc()
}
