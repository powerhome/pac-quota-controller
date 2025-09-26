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
	podHandler       *v1alpha1.PodWebhook
	pvcHandler       *v1alpha1.PersistentVolumeClaimWebhook
	crqHandler       *v1alpha1.ClusterResourceQuotaWebhook
	namespaceHandler *v1alpha1.NamespaceWebhook
	serviceHandler   *v1alpha1.ServiceWebhook

	k8sClient     kubernetes.Interface
	runtimeClient client.Client
}

// NewGinWebhookServer creates a new Gin-based webhook server
func NewGinWebhookServer(
	cfg *config.Config,
	kubeClient kubernetes.Interface,
	runtimeClient client.Client,
	logger *zap.Logger) *GinWebhookServer {
	const debugLevel = "debug"

	if logger == nil {
		logger = zap.NewNop()
	}

	// Configure Gin mode based on log level
	if cfg.LogLevel == debugLevel {
		gin.SetMode(gin.DebugMode)
	} else {
		gin.SetMode(gin.ReleaseMode)
	}

	// Create Gin engine with custom configuration
	engine := gin.New()

	// Add recovery middleware
	engine.Use(gin.Recovery())

	// Only add Gin logger in debug mode
	if cfg.LogLevel == debugLevel {
		engine.Use(gin.Logger())
	}

	server := &GinWebhookServer{
		engine:           engine,
		log:              logger,
		port:             cfg.WebhookPort,
		server:           &http.Server{},
		readyManager:     ready.NewReadinessManager(logger),
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
		if s.log != nil {
			s.log.Info("No certificate path provided, skipping certificate watcher setup")
		}
		return nil
	}

	if s.log != nil {
		s.log.Info("Initializing webhook certificate watcher using provided certificates",
			zap.String("webhook-cert-path", cfg.WebhookCertPath),
			zap.String("webhook-cert-name", cfg.WebhookCertName),
			zap.String("webhook-cert-key", cfg.WebhookCertKey))
	}

	var err error
	s.certWatcher, err = certwatcher.NewCertWatcher(
		cfg.WebhookCertPath+"/"+cfg.WebhookCertName,
		cfg.WebhookCertPath+"/"+cfg.WebhookCertKey,
		s.log,
	)
	if err != nil {
		if s.log != nil {
			s.log.Error("Failed to initialize webhook certificate watcher", zap.Error(err))
		}
		return fmt.Errorf("failed to initialize webhook certificate watcher: %w", err)
	}

	// Configure TLS with certificate watcher
	tlsConfig := &tls.Config{
		GetCertificate: s.certWatcher.GetCertificate,
	}
	s.server.TLSConfig = tlsConfig

	if s.log != nil {
		s.log.Info("Certificate watcher configured successfully")
	}
	return nil
}

// setupRoutes configures all webhook routes
func (s *GinWebhookServer) setupRoutes() {
	// Health and readiness check endpoints
	healthManager := health.NewHealthManager(s.log)
	s.readyManager = ready.NewReadinessManager(s.log)

	// Add default health and readiness checkers
	healthManager.AddChecker(health.NewSimpleHealthChecker("webhook-server"))
	s.readinessChecker = ready.NewSimpleReadinessChecker("webhook-server")
	s.readyManager.AddChecker(s.readinessChecker)

	s.engine.GET("/healthz", healthManager.HealthHandler())
	s.engine.GET("/readyz", s.readyManager.ReadyHandler())

	// Create CRQ client for custom resource operations
	var crqClient *quota.CRQClient
	if s.runtimeClient != nil {
		crqClient = quota.NewCRQClient(s.runtimeClient)
		if s.log != nil {
			s.log.Info("CRQ client created successfully for webhook validation")
		}
	} else {
		if s.log != nil {
			s.log.Warn("Dynamic client is nil, CRQ operations will not be available")
		}
	}

	if s.log != nil {
		s.log.Info("Setting up ClusterResourceQuota webhook")
	}
	s.crqHandler = v1alpha1.NewClusterResourceQuotaWebhook(s.k8sClient, crqClient, s.log)
	s.engine.POST("/validate-quota-powerapp-cloud-v1alpha1-clusterresourcequota", s.crqHandler.Handle)

	if s.log != nil {
		s.log.Info("Setting up namespace webhook")
	}
	s.namespaceHandler = v1alpha1.NewNamespaceWebhook(s.k8sClient, crqClient, s.log)
	s.engine.POST("/validate--v1-namespace", s.namespaceHandler.Handle)

	if s.log != nil {
		s.log.Info("Setting up pod webhook")
	}
	s.podHandler = v1alpha1.NewPodWebhook(s.k8sClient, crqClient, s.log)
	s.engine.POST("/validate--v1-pod", s.podHandler.Handle)

	if s.log != nil {
		s.log.Info("Setting up service webhook")
	}
	s.serviceHandler = v1alpha1.NewServiceWebhook(s.k8sClient, crqClient, s.log)
	s.engine.POST("/validate--v1-service", s.serviceHandler.Handle)

	if s.log != nil {
		s.log.Info("Setting up PVC webhook")
	}
	s.pvcHandler = v1alpha1.NewPersistentVolumeClaimWebhook(s.k8sClient, crqClient, s.log)
	s.engine.POST("/validate--v1-persistentvolumeclaim", s.pvcHandler.Handle)

	if s.log != nil {
		s.log.Info("Setting up objectcount webhook")
	}
	// Setup objectcount webhook handler
	if s.log != nil {
		s.log.Info("Setting up objectcount webhook")
	}
	objectCountHandler := v1alpha1.NewObjectCountWebhook(s.k8sClient, crqClient, s.log)
	s.engine.POST("/validate-objectcount-v1", objectCountHandler.Handle)

	if s.log != nil {
		s.log.Info("All webhook handlers configured with CRQ client support")
	}
}

// Start starts the webhook server
func (s *GinWebhookServer) Start(ctx context.Context) error {
	if s.log != nil {
		s.log.Info("Starting Gin webhook server", zap.Int("port", s.port))
	}

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
	if s.log != nil {
		s.log.Info("Webhook server startup initiated successfully")
	}

	// Wait for context cancellation
	<-ctx.Done()

	// Perform graceful shutdown
	return s.shutdown(ctx)
}

// startCertWatcher starts the certificate watcher if configured
func (s *GinWebhookServer) startCertWatcher(ctx context.Context) error {
	if s.certWatcher == nil {
		return nil
	}

	if s.log != nil {
		s.log.Info("Starting certificate watcher")
	}

	if err := s.certWatcher.Start(ctx); err != nil {
		if s.log != nil {
			s.log.Error("Failed to start certificate watcher", zap.Error(err))
		}
		return fmt.Errorf("failed to start certificate watcher: %w", err)
	}

	if s.log != nil {
		s.log.Info("Certificate watcher started successfully")
	}
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
		if s.log != nil {
			s.log.Info("TLS configuration set up using certificate watcher")
		}
	}
}

// startServerInBackground starts the HTTP server in a goroutine
func (s *GinWebhookServer) startServerInBackground() <-chan error {
	serverStarted := make(chan error, 1)
	go func() {
		var err error
		if s.server.TLSConfig != nil {
			if s.log != nil {
				s.log.Info("Starting server with TLS")
			}
			err = s.server.ListenAndServeTLS("", "")
		} else {
			if s.log != nil {
				s.log.Info("Starting server without TLS")
			}
			err = s.server.ListenAndServe()
		}

		if err != nil && err != http.ErrServerClosed {
			if s.log != nil {
				s.log.Error("Webhook server error", zap.Error(err))
			}
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
				if s.log != nil {
					s.log.Info("Webhook server is ready to accept connections")
				}
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
	if s.log != nil {
		s.log.Info("Context cancelled before server ready, shutting down")
	}

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

// shutdown performs graceful shutdown of the server
func (s *GinWebhookServer) shutdown(ctx context.Context) error {
	if s.log != nil {
		s.log.Info("Shutting down webhook server")
	}

	if s.certWatcher != nil {
		if s.log != nil {
			s.log.Info("Stopping certificate watcher")
		}
		s.certWatcher.Stop()
	}

	shutdownCtx, cancel := context.WithTimeout(ctx, 30*time.Second)
	defer cancel()

	return s.server.Shutdown(shutdownCtx)
}

// MarkReady marks the webhook server as ready
func (s *GinWebhookServer) MarkReady() {
	if s.readinessChecker != nil {
		if s.log != nil {
			s.log.Info("Marking webhook server as ready")
		}
		s.readinessChecker.SetReady(true)
	}
}

// StartWithSignalHandler starts the server with signal handling
func (s *GinWebhookServer) StartWithSignalHandler(ctx context.Context) error {
	// Create context that listens for the interrupt signal from the OS
	ctx, stop := signal.NotifyContext(ctx, os.Interrupt, syscall.SIGTERM)
	defer stop()

	return s.Start(ctx)
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
				if s.log != nil {
					s.log.Warn("Error closing response body in isServerReady", zap.Error(err))
				}
			}
		}
	}()

	return resp.StatusCode == http.StatusOK
}
