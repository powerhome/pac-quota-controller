package server

import (
	"context"
	"crypto/tls"
	"fmt"
	"net/http"
	"sync/atomic"
	"time"

	"github.com/gin-gonic/gin"
	"go.uber.org/zap"
	"k8s.io/client-go/kubernetes"
	"sigs.k8s.io/controller-runtime/pkg/client"

	"github.com/powerhome/pac-quota-controller/pkg/config"
	"github.com/powerhome/pac-quota-controller/pkg/health"
	"github.com/powerhome/pac-quota-controller/pkg/kubernetes/quota"
	pkglogger "github.com/powerhome/pac-quota-controller/pkg/logger"
	"github.com/powerhome/pac-quota-controller/pkg/metrics"
	"github.com/powerhome/pac-quota-controller/pkg/ready"
	"github.com/powerhome/pac-quota-controller/pkg/webhook/certwatcher"
	"github.com/powerhome/pac-quota-controller/pkg/webhook/v1alpha1"
)

// GinWebhookServer represents a Gin-based webhook server
type GinWebhookServer struct {
	engine      *gin.Engine
	server      *http.Server
	logger      *zap.Logger
	port        int
	certWatcher *certwatcher.CertWatcher
	// Health and readiness managers
	healthManager    *health.HealthManager
	readyManager     *ready.ReadinessManager
	readinessChecker *ready.SimpleReadinessChecker

	// Store webhook handlers to update CRQ client later
	podHandler       *v1alpha1.PodWebhook
	pvcHandler       *v1alpha1.PersistentVolumeClaimWebhook
	crqHandler       *v1alpha1.ClusterResourceQuotaWebhook
	namespaceHandler *v1alpha1.NamespaceWebhook
	serviceHandler   *v1alpha1.ServiceWebhook

	// Object count handler
	objectCountHandler *v1alpha1.ObjectCountWebhook

	k8sClient     kubernetes.Interface
	runtimeClient client.Client

	// cacheSynced flips to true once the manager's informer cache has finished
	// initial sync. /readyz gates on this so the apiserver doesn't route
	// admission traffic to a webhook whose CRQ lookups would silently fail-open
	// against a cold cache.
	cacheSynced atomic.Bool
}

// NewGinWebhookServer creates a new Gin-based webhook server
func NewGinWebhookServer(
	cfg *config.Config,
	kubeClient kubernetes.Interface,
	runtimeClient client.Client,
	logger *zap.Logger) *GinWebhookServer {
	const debugLevel = "debug"

	if logger == nil {
		logger = pkglogger.L()
	}

	// Configure Gin mode based on log level
	if cfg.LogLevel == debugLevel {
		gin.SetMode(gin.DebugMode)
	} else {
		gin.SetMode(gin.ReleaseMode)
	}

	// Create Gin engine with custom configuration
	engine := gin.New()

	// Add recovery and logger middleware
	engine.Use(gin.Recovery())
	engine.Use(RequestLogger(logger))

	server := &GinWebhookServer{
		engine:           engine,
		logger:           logger.Named("webhook-server"),
		port:             cfg.WebhookPort,
		server:           &http.Server{},
		readyManager:     ready.NewReadinessManager(logger),
		healthManager:    health.NewHealthManager(logger),
		readinessChecker: ready.NewSimpleReadinessChecker("webhook-server"),
		k8sClient:        kubeClient,
		runtimeClient:    runtimeClient,
	}

	// Setup routes
	server.setupRoutes()

	return server
}

// SetupCertificateWatcher configures certificate watching for the server
func (s *GinWebhookServer) SetupCertificateWatcher(cfg *config.Config) error {
	if len(cfg.WebhookCertPath) == 0 {
		s.logger.Info("No certificate path provided, skipping certificate watcher setup")
		return nil
	}

	s.logger.Info("Initializing webhook certificate watcher using provided certificates",
		zap.String("webhook-cert-path", cfg.WebhookCertPath),
		zap.String("webhook-cert-name", cfg.WebhookCertName),
		zap.String("webhook-cert-key", cfg.WebhookCertKey))

	var err error
	s.certWatcher, err = certwatcher.NewCertWatcher(
		cfg.WebhookCertPath+"/"+cfg.WebhookCertName,
		cfg.WebhookCertPath+"/"+cfg.WebhookCertKey,
		s.logger,
	)
	if err != nil {
		s.logger.Error("Failed to initialize webhook certificate watcher", zap.Error(err))
		return fmt.Errorf("failed to initialize webhook certificate watcher: %w", err)
	}

	// Configure TLS with certificate watcher
	tlsConfig := &tls.Config{
		GetCertificate: s.certWatcher.GetCertificate,
	}
	s.server.TLSConfig = tlsConfig

	s.logger.Info("Certificate watcher configured successfully")
	return nil
}

// setupRoutes configures all webhook routes
func (s *GinWebhookServer) setupRoutes() {
	// Health and readiness check endpoints

	// Add default health and readiness checkers
	s.healthManager.AddChecker(health.NewSimpleHealthChecker("webhook-server"))
	s.readinessChecker = ready.NewSimpleReadinessChecker("webhook-server")
	s.readyManager.AddChecker(s.readinessChecker)
	// Surface the "no runtime client → no CRQ enforcement" degraded state via
	// /readyz so the orchestrator can pull traffic instead of silently fail-open.
	s.readyManager.AddChecker(&crqClientReadinessChecker{server: s})
	// Block /readyz until the manager's informer cache has reported initial
	// sync. Without this the apiserver can route admission traffic to a webhook
	// whose CRQ list is empty, producing silent fail-open.
	s.readyManager.AddChecker(&cacheSyncReadinessChecker{server: s})

	s.engine.GET("/healthz", s.healthManager.HealthHandler())
	s.engine.GET("/readyz", s.readyManager.ReadyHandler())

	// Register custom metrics into controller-runtime registry (served by manager metrics server)
	metrics.RegisterWebhookMetrics()

	// Create CRQ client for custom resource operations
	var crqClient *quota.CRQClient
	if s.runtimeClient != nil {
		crqClient = quota.NewCRQClient(s.runtimeClient, s.logger)
		s.logger.Info("CRQ client created successfully for webhook validation")
	} else {
		s.logger.Warn("Dynamic client is nil, CRQ operations will not be available")
	}

	s.logger.Info("Setting up ClusterResourceQuota webhook")

	s.crqHandler = v1alpha1.NewClusterResourceQuotaWebhook(s.k8sClient, crqClient, s.logger)
	s.engine.POST("/validate-quota-powerapp-cloud-v1alpha1-clusterresourcequota", s.crqHandler.Handle)

	s.logger.Info("Setting up namespace webhook")

	s.namespaceHandler = v1alpha1.NewNamespaceWebhook(s.k8sClient, crqClient, s.logger)
	s.engine.POST("/validate--v1-namespace", s.namespaceHandler.Handle)

	s.logger.Info("Setting up pod webhook")

	s.podHandler = v1alpha1.NewPodWebhook(crqClient, s.logger)
	s.engine.POST("/validate--v1-pod", s.podHandler.Handle)

	s.logger.Info("Setting up service webhook")

	s.serviceHandler = v1alpha1.NewServiceWebhook(crqClient, s.logger)
	s.engine.POST("/validate--v1-service", s.serviceHandler.Handle)

	s.logger.Info("Setting up PVC webhook")

	s.pvcHandler = v1alpha1.NewPersistentVolumeClaimWebhook(crqClient, s.logger)
	s.engine.POST("/validate--v1-persistentvolumeclaim", s.pvcHandler.Handle)

	s.logger.Info("Setting up objectcount webhook")

	s.objectCountHandler = v1alpha1.NewObjectCountWebhook(crqClient, s.logger)
	s.engine.POST("/validate-objectcount-v1", s.objectCountHandler.Handle)

	s.logger.Info("All webhook handlers configured with CRQ client support")

}

// Start starts the webhook server
func (s *GinWebhookServer) Start(ctx context.Context) error {
	s.logger.Info("Starting Gin webhook server", zap.Int("port", s.port))

	// Start certificate watcher if configured
	if err := s.startCertWatcher(ctx); err != nil {
		return err
	}

	// Configure the server
	s.configureServer()

	// Start the server and wait for it to be ready
	serverStarted := s.startServerInBackground()
	if err := s.waitForServerReady(ctx, serverStarted); err != nil {
		return err
	}

	// Mark the server as ready and wait for shutdown
	s.MarkReady()
	s.logger.Info("Webhook server startup initiated successfully")

	// Wait for context cancellation
	<-ctx.Done()

	// Perform graceful shutdown. Pass no ctx — `shutdown` runs on a fresh
	// background timeout precisely because the request ctx is now done.
	return s.shutdown()
}

// startCertWatcher starts the certificate watcher if configured
func (s *GinWebhookServer) startCertWatcher(ctx context.Context) error {
	if s.certWatcher == nil {
		return nil
	}

	s.logger.Info("Starting certificate watcher")

	if err := s.certWatcher.Start(ctx); err != nil {
		s.logger.Error("Failed to start certificate watcher", zap.Error(err))

		return fmt.Errorf("failed to start certificate watcher: %w", err)
	}

	s.logger.Info("Certificate watcher started successfully")

	return nil
}

// configureServer sets up the server address and TLS configuration
func (s *GinWebhookServer) configureServer() {
	s.server.Addr = fmt.Sprintf(":%d", s.port)
	s.server.Handler = s.engine

	if s.certWatcher != nil {
		s.server.TLSConfig = &tls.Config{
			GetCertificate: s.certWatcher.GetCertificate,
		}
		s.logger.Info("TLS configuration set up using certificate watcher")

	}
}

// startServerInBackground starts the HTTP server in a goroutine
func (s *GinWebhookServer) startServerInBackground() <-chan error {
	serverStarted := make(chan error, 1)
	go func() {
		var err error
		if s.server.TLSConfig != nil {
			s.logger.Info("Starting server with TLS")

			err = s.server.ListenAndServeTLS("", "")
		} else {
			s.logger.Info("Starting server without TLS")
			err = s.server.ListenAndServe()
		}

		if err != nil && err != http.ErrServerClosed {
			s.logger.Error("Webhook server error", zap.Error(err))
			serverStarted <- err
		} else {
			serverStarted <- nil
		}
	}()
	return serverStarted
}

// waitForServerReady waits for the server to be ready to accept connections
func (s *GinWebhookServer) waitForServerReady(ctx context.Context, serverStarted <-chan error) error {
	isReady := false
	maxRetries := 30 // 15 seconds max wait time (30 * 500ms)
	backoff := 500 * time.Millisecond
	maxBackoff := 2 * time.Second

	for i := 0; i < maxRetries && !isReady; i++ {
		select {
		case err := <-serverStarted:
			if err != nil {
				return fmt.Errorf("webhook server failed to start: %w", err)
			}
			return fmt.Errorf("webhook server stopped unexpectedly")
		case <-ctx.Done():
			return s.handleContextCancelled(ctx)
		case <-time.After(backoff):
			if s.isServerReady() {
				isReady = true
				s.logger.Info("Webhook server is ready to accept connections")
			} else {
				backoff = s.calculateNextBackoff(backoff, maxBackoff)
			}
		}
	}

	if !isReady {
		return fmt.Errorf("webhook server failed to become ready within timeout period")
	}
	return nil
}

// handleContextCancelled handles context cancellation during startup
func (s *GinWebhookServer) handleContextCancelled(ctx context.Context) error {
	s.logger.Info("Context cancelled before server ready, shutting down")

	if s.certWatcher != nil {
		s.certWatcher.Stop()
	}

	shutdownCtx, cancel := context.WithTimeout(ctx, 5*time.Second)
	defer cancel()
	return s.server.Shutdown(shutdownCtx)
}

// calculateNextBackoff calculates the next backoff duration with exponential backoff
func (s *GinWebhookServer) calculateNextBackoff(currentBackoff, maxBackoff time.Duration) time.Duration {
	nextBackoff := currentBackoff * 2
	if nextBackoff > maxBackoff {
		return maxBackoff
	}
	return nextBackoff
}

// shutdown performs graceful shutdown of the server. It runs on a fresh
// background timeout because the caller's ctx is, by construction, already
// cancelled — that's what triggered the shutdown in the first place.
func (s *GinWebhookServer) shutdown() error {
	s.logger.Info("Shutting down webhook server")

	if s.certWatcher != nil {
		s.logger.Info("Stopping certificate watcher")
		s.certWatcher.Stop()
	}

	shutdownCtx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	return s.server.Shutdown(shutdownCtx)
}

// MarkReady marks the webhook server as ready
func (s *GinWebhookServer) MarkReady() {
	if s.readinessChecker != nil {
		s.logger.Info("Marking webhook server as ready")
		s.readinessChecker.SetReady(true)
	}
}

// MarkCacheSynced flips the cache-sync readiness gate. Callers should invoke
// this only after the manager reports its informer cache has completed initial
// sync (e.g. mgr.GetCache().WaitForCacheSync(ctx) returned true).
func (s *GinWebhookServer) MarkCacheSynced() {
	if !s.cacheSynced.Swap(true) {
		s.logger.Info("Webhook informer cache reported synced; /readyz will now pass")
	}
}

// crqClientReadinessChecker reports the webhook as not-ready while the runtime
// (CRQ) client is unset. Without it the webhook silently fail-opens on every
// admission; /readyz failing pulls the pod out of the Service so the
// orchestrator can surface the misconfiguration.
type crqClientReadinessChecker struct {
	server *GinWebhookServer
}

func (c *crqClientReadinessChecker) IsReady() bool {
	return c.server != nil && c.server.runtimeClient != nil
}

func (c *crqClientReadinessChecker) GetReadinessStatus() ready.ReadinessStatus {
	if c.IsReady() {
		return ready.ReadinessStatus{
			Ready:   true,
			Status:  "ready",
			Details: map[string]any{"name": "crq-client"},
		}
	}
	return ready.ReadinessStatus{
		Ready:   false,
		Status:  "not ready: CRQ client missing - quota enforcement is disabled",
		Details: map[string]any{"name": "crq-client"},
	}
}

// cacheSyncReadinessChecker reports not-ready until the manager's informer
// cache has had a chance to populate. See server.MarkCacheSynced.
type cacheSyncReadinessChecker struct {
	server *GinWebhookServer
}

func (c *cacheSyncReadinessChecker) IsReady() bool {
	return c.server != nil && c.server.cacheSynced.Load()
}

func (c *cacheSyncReadinessChecker) GetReadinessStatus() ready.ReadinessStatus {
	if c.IsReady() {
		return ready.ReadinessStatus{
			Ready:   true,
			Status:  "ready",
			Details: map[string]any{"name": "cache-sync"},
		}
	}
	return ready.ReadinessStatus{
		Ready:   false,
		Status:  "not ready: informer cache has not finished initial sync",
		Details: map[string]any{"name": "cache-sync"},
	}
}

// GetCertWatcher returns the certificate watcher for external management
func (s *GinWebhookServer) GetCertWatcher() *certwatcher.CertWatcher {
	return s.certWatcher
}

// isServerReady checks if the server is ready to accept connections
func (s *GinWebhookServer) isServerReady() bool {
	if s.server == nil || s.server.Addr == "" {
		return false
	}

	// Try to make a simple connection to the server
	addr := s.server.Addr
	if addr[0] == ':' {
		addr = "localhost" + addr
	}

	var scheme string
	if s.server.TLSConfig != nil {
		scheme = "https"
	} else {
		scheme = "http"
	}

	url := fmt.Sprintf("%s://%s/healthz", scheme, addr)
	c := &http.Client{
		Timeout: 100 * time.Millisecond,
		Transport: &http.Transport{
			TLSClientConfig: &tls.Config{InsecureSkipVerify: true},
		},
	}

	resp, err := c.Get(url)
	if err != nil {
		return false
	}
	defer func() {
		if resp != nil {
			if err := resp.Body.Close(); err != nil {
				s.logger.Warn("Error closing response body in isServerReady", zap.Error(err))
			}
		}
	}()

	return resp.StatusCode == http.StatusOK
}
