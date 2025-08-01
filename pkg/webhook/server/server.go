/*
Copyright 2025.

Licensed under the Apache License, Version 2.0 (the "License");
you may not use this file except in compliance with the License.
You may obtain a copy of the License at

    http://www.apache.org/licenses/LICENSE-2.0

Unless required by applicable law or agreed to in writing, software
distributed under the License is distributed on an "AS IS" BASIS,
WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
See the License for the specific language governing permissions and
limitations under the License.
*/

package server

import (
	"context"
	"crypto/tls"
	"fmt"
	"net/http"
	"os"
	"os/signal"
	"syscall"
	"time"

	"github.com/gin-gonic/gin"
	"go.uber.org/zap"
	"k8s.io/client-go/kubernetes"
	"sigs.k8s.io/controller-runtime/pkg/client"

	"github.com/powerhome/pac-quota-controller/pkg/config"
	"github.com/powerhome/pac-quota-controller/pkg/health"
	"github.com/powerhome/pac-quota-controller/pkg/kubernetes/quota"
	"github.com/powerhome/pac-quota-controller/pkg/ready"
	"github.com/powerhome/pac-quota-controller/pkg/webhook/certwatcher"
	"github.com/powerhome/pac-quota-controller/pkg/webhook/v1alpha1"
)

// GinWebhookServer represents a Gin-based webhook server
type GinWebhookServer struct {
	engine           *gin.Engine
	server           *http.Server
	log              *zap.Logger
	port             int
	certWatcher      *certwatcher.CertWatcher
	readyManager     *ready.ReadinessManager
	readinessChecker *ready.SimpleReadinessChecker
	// Store webhook handlers to update CRQ client later
	podHandler *v1alpha1.PodWebhook
	pvcHandler *v1alpha1.PersistentVolumeClaimWebhook
}

// NewGinWebhookServer creates a new Gin-based webhook server
func NewGinWebhookServer(cfg *config.Config, c kubernetes.Interface, log *zap.Logger) *GinWebhookServer {
	// Set Gin mode
	if cfg.LogLevel == "debug" {
		gin.SetMode(gin.DebugMode)
	} else {
		gin.SetMode(gin.ReleaseMode)
	}

	engine := gin.New()

	// Add middleware
	engine.Use(gin.Recovery())
	engine.Use(gin.Logger())

	// Create server
	server := &GinWebhookServer{
		engine: engine,
		log:    log,
		port:   cfg.WebhookPort,
	}

	// Setup routes
	server.setupRoutes(c)

	// Create HTTP server
	server.server = &http.Server{
		Addr:    fmt.Sprintf(":%d", cfg.WebhookPort),
		Handler: engine,
	}

	return server
}

// SetupCertificateWatcher configures certificate watching for the server
func (s *GinWebhookServer) SetupCertificateWatcher(cfg *config.Config) error {
	if len(cfg.WebhookCertPath) == 0 {
		s.log.Info("No certificate path provided, skipping certificate watcher setup")
		return nil
	}

	s.log.Info("Initializing webhook certificate watcher using provided certificates",
		zap.String("webhook-cert-path", cfg.WebhookCertPath),
		zap.String("webhook-cert-name", cfg.WebhookCertName),
		zap.String("webhook-cert-key", cfg.WebhookCertKey))

	var err error
	s.certWatcher, err = certwatcher.NewCertWatcher(
		cfg.WebhookCertPath+"/"+cfg.WebhookCertName,
		cfg.WebhookCertPath+"/"+cfg.WebhookCertKey,
		s.log,
	)
	if err != nil {
		s.log.Error("Failed to initialize webhook certificate watcher", zap.Error(err))
		return fmt.Errorf("failed to initialize webhook certificate watcher: %w", err)
	}

	// Configure TLS with certificate watcher
	tlsConfig := &tls.Config{
		GetCertificate: s.certWatcher.GetCertificate,
	}
	s.server.TLSConfig = tlsConfig

	s.log.Info("Certificate watcher configured successfully")
	return nil
}

// setupRoutes configures all webhook routes
func (s *GinWebhookServer) setupRoutes(k8sClient kubernetes.Interface) {
	// Health and readiness check endpoints
	healthManager := health.NewHealthManager(s.log)
	s.readyManager = ready.NewReadinessManager(s.log)

	// Add default health and readiness checkers
	healthManager.AddChecker(health.NewSimpleHealthChecker("webhook-server"))
	s.readinessChecker = ready.NewSimpleReadinessChecker("webhook-server")
	s.readyManager.AddChecker(s.readinessChecker)

	s.engine.GET("/healthz", healthManager.HealthHandler())
	s.engine.GET("/readyz", s.readyManager.ReadyHandler())

	// Webhook endpoints
	crqHandler := v1alpha1.NewClusterResourceQuotaWebhook(k8sClient, s.log)
	s.engine.POST("/validate-quota-powerapp-cloud-v1alpha1-clusterresourcequota", crqHandler.Handle)

	namespaceHandler := v1alpha1.NewNamespaceWebhook(k8sClient, s.log)
	s.engine.POST("/validate--v1-namespace", namespaceHandler.Handle)

	s.podHandler = v1alpha1.NewPodWebhook(k8sClient, s.log)
	s.engine.POST("/validate--v1-pod", s.podHandler.Handle)

	s.pvcHandler = v1alpha1.NewPersistentVolumeClaimWebhook(k8sClient, s.log)
	s.engine.POST("/validate--v1-persistentvolumeclaim", s.pvcHandler.Handle)
}

// Start starts the webhook server
func (s *GinWebhookServer) Start(ctx context.Context) error {
	s.log.Info("Starting Gin webhook server", zap.Int("port", s.port))

	// Start certificate watcher if configured
	if s.certWatcher != nil {
		s.log.Info("Starting certificate watcher")
		if err := s.certWatcher.Start(ctx); err != nil {
			s.log.Error("Failed to start certificate watcher", zap.Error(err))
			return fmt.Errorf("failed to start certificate watcher: %w", err)
		}
	}

	// Start server in a goroutine
	go func() {
		var err error
		if s.server.TLSConfig != nil {
			s.log.Info("Starting server with TLS")
			err = s.server.ListenAndServeTLS("", "")
		} else {
			s.log.Info("Starting server without TLS")
			err = s.server.ListenAndServe()
		}
		if err != nil && err != http.ErrServerClosed {
			s.log.Error("Webhook server error", zap.Error(err))
		}
	}()

	// Wait for context cancellation
	<-ctx.Done()

	s.log.Info("Shutting down webhook server")

	// Stop certificate watcher if running
	if s.certWatcher != nil {
		s.log.Info("Stopping certificate watcher")
		s.certWatcher.Stop()
	}

	// Create a context with timeout for graceful shutdown
	shutdownCtx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	return s.server.Shutdown(shutdownCtx)
}

// MarkReady marks the webhook server as ready
func (s *GinWebhookServer) MarkReady() {
	if s.readinessChecker != nil {
		s.log.Info("Marking webhook server as ready")
		s.readinessChecker.SetReady(true)
	}
}

// StartWithSignalHandler starts the server with signal handling
func (s *GinWebhookServer) StartWithSignalHandler() error {
	// Create context that listens for the interrupt signal from the OS
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()

	return s.Start(ctx)
}

// GetCertWatcher returns the certificate watcher for external management
func (s *GinWebhookServer) GetCertWatcher() *certwatcher.CertWatcher {
	return s.certWatcher
}

// SetCRQClient sets the CRQ client for webhook handlers that need it
func (s *GinWebhookServer) SetCRQClient(controllerClient client.Client) {
	if s.podHandler != nil {
		crqClient := quota.NewCRQClient(controllerClient)
		s.podHandler.SetCRQClient(crqClient)
		s.log.Info("Set CRQ client for pod webhook")
	}
	if s.pvcHandler != nil {
		crqClient := quota.NewCRQClient(controllerClient)
		s.pvcHandler.SetCRQClient(crqClient)
		s.log.Info("Set CRQ client for PVC webhook")
	}
}
