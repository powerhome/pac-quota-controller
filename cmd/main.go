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

package main

import (
	"context"
	"fmt"
	"os"
	"os/signal"
	"syscall"

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
	"github.com/powerhome/pac-quota-controller/pkg/logger"
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

			// Get the namespace the controller is running in
			podNamespace, found := os.LookupEnv("POD_NAMESPACE")
			if !found {
				fmt.Fprintf(os.Stderr, "POD_NAMESPACE environment variable not set\n")
				os.Exit(1)
			}
			cfg.OwnNamespace = podNamespace

			// Set up logging
			zapLogger := logger.SetupLogger(cfg)
			defer func() {
				if err := zapLogger.Sync(); err != nil {
					zapLogger.Error("Failed to sync logger", zap.Error(err))
				}
			}()

			// Configure controller-runtime logger to use zap for consistent JSON formatting
			ctrl.SetLogger(zapctrl.New(zapctrl.UseDevMode(false), zapctrl.JSONEncoder()))

			// Initialize scheme
			scheme := manager.InitScheme()

			// Create controller manager
			mgr, err := manager.SetupManager(cfg, scheme)
			if err != nil {
				zapLogger.Error("unable to start manager", zap.Error(err))
				os.Exit(1)
			}

			// Set up controllers
			if err := manager.SetupControllers(mgr, cfg); err != nil {
				zapLogger.Error("unable to set up controllers", zap.Error(err))
				os.Exit(1)
			}

			// Create kubernetes clientset for webhook server
			clientset, err := kubernetes.NewForConfig(mgr.GetConfig())
			if err != nil {
				zapLogger.Error("unable to create kubernetes clientset", zap.Error(err))
				os.Exit(1)
			}

			// Set up Gin webhook server
			webhookServer, webhookCertWatcher := webhook.SetupGinWebhookServer(cfg, clientset, zapLogger)

			// Start webhook server
			go func() {
				if err := webhookServer.Start(context.Background()); err != nil {
					zapLogger.Error("webhook server failed", zap.Error(err))
				}
			}()

			// Start certificate watchers if configured
			if webhookCertWatcher != nil {
				go func() {
					if err := webhookCertWatcher.Start(context.Background()); err != nil {
						zapLogger.Error("webhook certificate watcher failed", zap.Error(err))
					}
				}()
			}

			// Set up graceful shutdown
			ctx, cancel := context.WithCancel(context.Background())
			defer cancel()

			// Handle shutdown signals
			sigChan := make(chan os.Signal, 1)
			signal.Notify(sigChan, syscall.SIGINT, syscall.SIGTERM)

			// Start the manager
			zapLogger.Info("Starting controller manager")
			go func() {
				if err := mgr.Start(ctx); err != nil {
					zapLogger.Error("controller manager failed", zap.Error(err))
				}
			}()

			// Add a small delay and then log readiness
			go func() {
				<-mgr.Elected()
				zapLogger.Info("Controller manager elected as leader and ready to process resources")

				// Set CRQ client for webhook server now that the manager is ready
				if webhookServer != nil {
					webhookServer.SetCRQClient(mgr.GetClient())
				}

				// Mark webhook server as ready now that the manager is ready
				if webhookServer != nil {
					webhookServer.MarkReady()
				}
			}()

			// Wait for shutdown signal
			zapLogger.Info("Controller manager startup completed, waiting for shutdown signal")
			<-sigChan
			zapLogger.Info("Received shutdown signal, starting graceful shutdown")

			// Stop certificate watchers
			if webhookCertWatcher != nil {
				webhookCertWatcher.Stop()
			}

			// Stop webhook server by canceling context
			cancel()

			zapLogger.Info("Graceful shutdown completed")
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
