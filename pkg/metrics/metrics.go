package metrics

import (
	"net/http"

	"sync"

	"github.com/powerhome/pac-quota-controller/pkg/config"
	"github.com/prometheus/client_golang/prometheus"
	"go.uber.org/zap"
)

var (
	CRQUsage = prometheus.NewGaugeVec(
		prometheus.GaugeOpts{
			Name: "pac_quota_controller_crq_usage",
			Help: "Current usage of a resource for a ClusterResourceQuota in a namespace.",
		},
		[]string{"crq_name", "namespace", "resource"},
	)
	CRQTotalUsage = prometheus.NewGaugeVec(
		prometheus.GaugeOpts{
			Name: "pac_quota_controller_crq_total_usage",
			Help: "Aggregated usage of a resource across all namespaces for a ClusterResourceQuota.",
		},
		[]string{"crq_name", "resource"},
	)
	WebhookValidationCount = prometheus.NewCounterVec(
		prometheus.CounterOpts{
			Name: "pac_quota_controller_webhook_validation_total",
			Help: "Total number of webhook validation requests.",
		},
		[]string{"webhook", "operation"},
	)
	WebhookValidationDuration = prometheus.NewHistogramVec(
		prometheus.HistogramOpts{
			Name: "pac_quota_controller_webhook_validation_duration_seconds",
			Help: "Duration of webhook validation requests.",
		},
		[]string{"webhook", "operation"},
	)
	WebhookAdmissionDecision = prometheus.NewCounterVec(
		prometheus.CounterOpts{
			Name: "pac_quota_controller_webhook_admission_decision_total",
			Help: "Total number of webhook admission decisions (allowed/denied).",
		},
		[]string{"webhook", "operation", "decision"},
	)
	// Custom registry for webhook metrics only
	WebhookRegistry = prometheus.NewRegistry()
	registerOnce    sync.Once
)

func RegisterWebhookMetrics() {
	registerOnce.Do(func() {
		WebhookRegistry.MustRegister(CRQUsage)
		WebhookRegistry.MustRegister(CRQTotalUsage)
		WebhookRegistry.MustRegister(WebhookValidationCount)
		WebhookRegistry.MustRegister(WebhookValidationDuration)
		WebhookRegistry.MustRegister(WebhookAdmissionDecision)
	})
}

// SetupStandaloneMetricsServer creates a standalone HTTP server for metrics
func SetupStandaloneMetricsServer(cfg *config.Config, log *zap.Logger) (*http.Server, error) {
	// Create a simple HTTP server for metrics
	server := &http.Server{
		Addr: cfg.MetricsAddr,
	}

	// Set up basic metrics endpoint
	http.HandleFunc("/metrics", func(w http.ResponseWriter, r *http.Request) {
		if _, err := w.Write([]byte("# Metrics endpoint\n")); err != nil {
			http.Error(w, "Failed to write response", http.StatusInternalServerError)
			return
		}
		w.Header().Set("Content-Type", "text/plain")
	})

	log.Info("Standalone metrics server configured", zap.String("address", cfg.MetricsAddr))
	return server, nil
}
