package metrics

import (
	"net/http"

	"github.com/powerhome/pac-quota-controller/pkg/config"
	"go.uber.org/zap"
)

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

	// Set up health check endpoint
	http.HandleFunc("/healthz", func(w http.ResponseWriter, r *http.Request) {
		// Return JSON response for health check
		w.Header().Set("Content-Type", "application/json")
		if _, err := w.Write([]byte(`{"status":"ok"}`)); err != nil {
			http.Error(w, "Failed to write response", http.StatusInternalServerError)
			return
		}
	})

	log.Info("Standalone metrics server configured", zap.String("address", cfg.MetricsAddr))
	return server, nil
}
