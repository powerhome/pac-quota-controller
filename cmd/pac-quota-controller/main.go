package main

import (
	"context"
	"flag"
	"net/http"
	"os"
	"os/signal"
	"syscall"
	"time"

	"github.com/powerhome/pac-quota-controller/internal/config"
	"github.com/powerhome/pac-quota-controller/internal/handlers"
	"github.com/powerhome/pac-quota-controller/pkg/logging"
	"github.com/prometheus/client_golang/prometheus/promhttp"
	"github.com/spf13/cobra"
	"go.uber.org/zap"
)

var (
	version = "dev"
	cfg     *config.Config
	logger  = logging.NewLogger()
	rootCmd = &cobra.Command{
		Use:   "pac-quota-controller",
		Short: "PAC Resource Sharing Validation Webhook",
		Long: `A webhook service for validating resource sharing requests in the powerhome ecosystem.
This service provides validation endpoints for resource sharing operations.`,
		Version: version,
		Run: func(cmd *cobra.Command, args []string) {
			// Initialize logger
			if err := logging.InitLogger(cfg.LogLevel); err != nil {
				logger.Fatal("Failed to initialize logger", zap.Error(err))
			}
			defer func() {
				if err := logging.Sync(); err != nil {
					// Since we're in defer, we can only log the error, not return it
					logger.Error("Failed to sync logger", zap.Error(err))
				}
			}()

			// Start the server
			if err := startServer(cfg); err != nil {
				logger.Fatal("Failed to start server", zap.Error(err))
			}
		},
	}
)

func init() {
	// Initialize configuration
	var err error
	cfg, err = config.Load()
	if err != nil {
		logger.Error("Failed to load configuration: " + err.Error() + "\n")
		os.Exit(1)
	}

	// Add flags here
	rootCmd.PersistentFlags().StringP("config", "c", "", "config file (default is $HOME/.pac-quota-controller.yaml)")
	flag.StringVar(&cfg.Port, "port", "443", "port to listen on")
	flag.StringVar(&cfg.TLSCert, "tls-cert-file", "/etc/webhook/certs/tls.crt", "TLS certificate file")
	flag.StringVar(&cfg.TLSKey, "tls-key-file", "/etc/webhook/certs/tls.key", "TLS key file")
	flag.StringVar(&cfg.LogLevel, "log-level", "info", "log level (debug, info, warn, error, fatal)")
	flag.Parse()
}

func startServer(cfg *config.Config) error {
	// Create HTTP server
	addr := ":" + cfg.Port
	mux := http.NewServeMux()

	// Register handlers
	mux.HandleFunc("/validate", handlers.HandleWebhook)
	mux.Handle("/metrics", promhttp.Handler())
	mux.HandleFunc("/healthz", handlers.HandleHealthz)
	mux.HandleFunc("/readyz", handlers.HandleReadyz)

	// Create server with appropriate timeouts
	server := &http.Server{
		Addr:         addr,
		Handler:      mux,
		ReadTimeout:  10 * time.Second,
		WriteTimeout: 10 * time.Second,
		IdleTimeout:  120 * time.Second,
	}

	// Start server in a goroutine
	go func() {
		logger.Info("Starting server", zap.String("port", cfg.Port))
		var err error

		// Check if TLS certificate files exist and are accessible
		_, certErr := os.Stat(cfg.TLSCert)
		_, keyErr := os.Stat(cfg.TLSKey)

		if certErr == nil && keyErr == nil {
			logger.Info("Using TLS certificates", zap.String("cert", cfg.TLSCert), zap.String("key", cfg.TLSKey))
			err = server.ListenAndServeTLS(cfg.TLSCert, cfg.TLSKey)
		} else {
			// Fall back to HTTP if certificates are not available
			logger.Warn("TLS certificates not found, falling back to HTTP",
				zap.String("cert", cfg.TLSCert),
				zap.String("key", cfg.TLSKey),
				zap.Error(certErr),
				zap.Error(keyErr))
			err = server.ListenAndServe()
		}

		if err != nil && err != http.ErrServerClosed {
			logger.Fatal("Server failed", zap.Error(err))
		}
	}()

	// Wait for interrupt signal
	quit := make(chan os.Signal, 1)
	signal.Notify(quit, syscall.SIGINT, syscall.SIGTERM)
	sig := <-quit
	logger.Info("Shutting down server...", zap.String("signal", sig.String()))

	// Graceful shutdown
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	if err := server.Shutdown(ctx); err != nil {
		logger.Error("Server forced to shutdown", zap.Error(err))
		return err
	}

	logger.Info("Server exited gracefully")
	return nil
}

func main() {
	if err := rootCmd.Execute(); err != nil {
		logger.Error("Failed to execute command: " + err.Error() + "\n")
		os.Exit(1)
	}
}
