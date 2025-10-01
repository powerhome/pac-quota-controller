package metrics

import (
	"crypto/tls"
	"fmt"
	"net/http"
	"os"
	"path/filepath"
	"sync"

	"github.com/powerhome/pac-quota-controller/pkg/config"
	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/client_golang/prometheus/promhttp"
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

// MetricsServer encapsulates the metrics HTTP server and its lifecycle.
type MetricsServer struct {
	cfg      *config.Config
	log      *zap.Logger
	server   *http.Server
	registry *prometheus.Registry
}

// NewMetricsServer creates a new MetricsServer instance and registers metrics.
//
// The metrics server requires a valid TLS certificate and key to be present at startup.
// These are typically provisioned by cert-manager and mounted into the pod as files.
// If the certificate or key is missing, server startup will fail with a clear error.
func NewMetricsServer(cfg *config.Config, log *zap.Logger) (*MetricsServer, error) {
	RegisterWebhookMetrics()
	ms := &MetricsServer{
		cfg:      cfg,
		log:      log,
		registry: WebhookRegistry,
	}
	if err := ms.setupServer(); err != nil {
		return nil, err
	}
	return ms, nil
}

// Start runs the metrics server in a goroutine.
func (ms *MetricsServer) Start(stopCh <-chan struct{}) {
	go func() {
		<-stopCh
		ms.log.Info("Shutting down metrics server...")
		if err := ms.server.Close(); err != nil {
			ms.log.Error("Error shutting down metrics server", zap.Error(err))
		}
	}()

	go func() {
		ms.log.Info("Starting metrics server", zap.String("address", ms.server.Addr))
		if err := ms.server.ListenAndServeTLS("", ""); err != nil && err != http.ErrServerClosed {
			ms.log.Error("Metrics server failed", zap.Error(err))
		}
	}()
}

// setupServer initializes the HTTPS server for metrics.
// The certificate and key files must exist at startup. These are typically mounted from a cert-manager-managed Secret.
// If the files are missing, this method returns a clear error and the server will not start.
func (ms *MetricsServer) setupServer() error {
	addr := fmt.Sprintf(":%d", ms.cfg.MetricsPort)
	mux := http.NewServeMux()
	mux.Handle("/metrics", promhttp.HandlerFor(ms.registry, promhttp.HandlerOpts{}))

	if !ms.cfg.SecureMetrics {
		ms.server = &http.Server{
			Addr:    addr,
			Handler: mux,
		}
		ms.log.Info("Standalone metrics server configured", zap.String("address", addr))
		return nil
	}

	certPath := ms.cfg.MetricsCertPath
	certFile := filepath.Join(certPath, "tls.crt")
	keyFile := filepath.Join(certPath, "tls.key")

	// Check cert files exist (required for HTTPS)
	if _, err := os.Stat(certFile); err != nil {
		return fmt.Errorf("metrics server certificate file not found: %s", certFile)
	}
	if _, err := os.Stat(keyFile); err != nil {
		return fmt.Errorf("metrics server key file not found: %s", keyFile)
	}

	cert, err := tls.LoadX509KeyPair(certFile, keyFile)
	if err != nil {
		return fmt.Errorf("failed to load metrics server cert/key: %w", err)
	}

	tlsConfig := &tls.Config{
		Certificates: []tls.Certificate{cert},
		MinVersion:   tls.VersionTLS12,
	}

	ms.server = &http.Server{
		Addr:      addr,
		Handler:   mux,
		TLSConfig: tlsConfig,
	}

	ms.log.Info("Standalone metrics server configured", zap.String("address", addr))
	return nil
}
