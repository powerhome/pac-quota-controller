package metrics

import (
	"testing"

	"github.com/prometheus/client_golang/prometheus"
	dto "github.com/prometheus/client_model/go"
)

// hasCRQSeries reports whether any series currently collected by c carries
// crq_name=name, without creating one (unlike WithLabelValues).
func hasCRQSeries(c prometheus.Collector, name string) bool {
	ch := make(chan prometheus.Metric, 16)
	go func() {
		c.Collect(ch)
		close(ch)
	}()
	for m := range ch {
		d := &dto.Metric{}
		_ = m.Write(d)
		for _, l := range d.GetLabel() {
			if l.GetName() == labelCRQName && l.GetValue() == name {
				return true
			}
		}
	}
	return false
}

// A deleted CRQ's gauges must not keep reporting their last value forever.
func TestDeleteCRQMetricsClearsAllGauges(t *testing.T) {
	CRQUsage.WithLabelValues("ghost-crq", "some-ns", "pods").Set(1)
	CRQTotalUsage.WithLabelValues("ghost-crq", "pods").Set(1)
	CRQHard.WithLabelValues("ghost-crq", "pods").Set(1)
	CRQUsed.WithLabelValues("ghost-crq", "pods").Set(1)
	CRQReportOnly.WithLabelValues("ghost-crq").Set(1)

	DeleteCRQMetrics("ghost-crq")

	for _, gv := range []*prometheus.GaugeVec{CRQUsage, CRQTotalUsage, CRQHard, CRQUsed, CRQReportOnly} {
		if hasCRQSeries(gv, "ghost-crq") {
			t.Fatalf("expected no series for ghost-crq after DeleteCRQMetrics")
		}
	}
}

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
