package main

import (
	"fmt"
	"os"

	// Import all Kubernetes client auth plugins (e.g. Azure, GCP, OIDC, etc.)
	// to ensure that exec-entrypoint and run can make use of them.
	_ "k8s.io/client-go/plugin/pkg/client/auth"

	"github.com/spf13/cobra"
	"go.uber.org/zap"
	"k8s.io/client-go/kubernetes"
	ctrl "sigs.k8s.io/controller-runtime"
	zapctrl "sigs.k8s.io/controller-runtime/pkg/log/zap"

	"github.com/powerhome/pac-quota-controller/cmd/version"
	"github.com/powerhome/pac-quota-controller/pkg/config"
	pkglogger "github.com/powerhome/pac-quota-controller/pkg/logger"
	"github.com/powerhome/pac-quota-controller/pkg/manager"
	"github.com/powerhome/pac-quota-controller/pkg/webhook"
)

// nolint:gocyclo
func main() {
	// Create root command
	rootCmd := &cobra.Command{
		Use:   "controller-manager",
		Short: "Cluster Resource Quota controller manager",
		Long:  "Manages ClusterResourceQuota resources that provide quota limits across multiple namespaces",
		Run: func(cmd *cobra.Command, args []string) {
			// Initialize configuration
			cfg := config.InitConfig()

			// Set up logging
			pkglogger.Initialize(cfg)
			logger := pkglogger.L()
			defer func() {
				if err := logger.Sync(); err != nil {
					logger.Error("Failed to sync logger", zap.Error(err))
				}
			}()

			// Configure controller-runtime logger to use zap for consistent JSON formatting
			ctrl.SetLogger(zapctrl.New(zapctrl.UseDevMode(false), zapctrl.JSONEncoder()))

			// Use controller-runtime's signal handler — cancels context on SIGTERM/SIGINT
			ctx := ctrl.SetupSignalHandler()

			// Initialize scheme
			scheme := manager.InitScheme()

			// Create controller manager
			mgr, err := manager.SetupManager(cfg, scheme)
			if err != nil {
				logger.Error("unable to start manager", zap.Error(err))
				os.Exit(1)
			}

			// Set up controllers
			if err := manager.SetupControllers(ctx, mgr, cfg, logger); err != nil {
				logger.Error("unable to set up controllers", zap.Error(err))
				os.Exit(1)
			}

			// Create kubernetes clientset for webhook server
			clientset, err := kubernetes.NewForConfig(mgr.GetConfig())
			if err != nil {
				logger.Error("unable to create kubernetes clientset", zap.Error(err))
				os.Exit(1)
			}

			// Set up Gin webhook server with manager's client for CRQ operations
			webhookServer, webhookCertWatcher := webhook.SetupGinWebhookServer(cfg, clientset, mgr.GetClient(), logger)

			// Start webhook server and cert watcher in background goroutines.
			// They respect context cancellation via <-ctx.Done() for graceful shutdown.
			go func() {
				if err := webhookServer.Start(ctx); err != nil {
					logger.Error("webhook server failed", zap.Error(err))
				}
			}()

			if webhookCertWatcher != nil {
				go func() {
					if err := webhookCertWatcher.Start(ctx); err != nil {
						logger.Error("webhook certificate watcher failed", zap.Error(err))
					}
				}()
			}

			// Log when manager is elected as leader
			go func() {
				<-mgr.Elected()
				logger.Info("Controller manager elected as leader and ready to process resources")
			}()

			// Start the manager synchronously. This blocks until the context is cancelled
			// (SIGTERM/SIGINT) or the manager fails (e.g. leader election lost).
			// When it returns, the process exits — no zombie state possible.
			logger.Info("Starting controller manager")
			if err := mgr.Start(ctx); err != nil {
				logger.Error("controller manager failed", zap.Error(err))
				os.Exit(1)
			}
		},
	}

	// Add version command
	rootCmd.AddCommand(version.NewVersionCmd())

	// Setup flags
	config.SetupFlags(rootCmd)

	// Execute the root command
	if err := rootCmd.Execute(); err != nil {
		fmt.Println(err)
		os.Exit(1)
	}
}
