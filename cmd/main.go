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
	"fmt"
	"os"

	// Import all Kubernetes client auth plugins (e.g. Azure, GCP, OIDC, etc.)
	// to ensure that exec-entrypoint and run can make use of them.
	_ "k8s.io/client-go/plugin/pkg/client/auth"

	"github.com/spf13/cobra"

	logf "sigs.k8s.io/controller-runtime/pkg/log"

	"github.com/powerhome/pac-quota-controller/cmd/version"
	"github.com/powerhome/pac-quota-controller/internal/webhook/v1alpha1"
	"github.com/powerhome/pac-quota-controller/pkg/config"
	"github.com/powerhome/pac-quota-controller/pkg/logger"
	"github.com/powerhome/pac-quota-controller/pkg/manager"
	"github.com/powerhome/pac-quota-controller/pkg/metrics"
	"github.com/powerhome/pac-quota-controller/pkg/tls"
	"github.com/powerhome/pac-quota-controller/pkg/webhook"
)

var setupLog = logf.Log.WithName("setup")

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
			zapLogger := logger.SetupLogger(cfg)
			defer func() {
				if err := zapLogger.Sync(); err != nil {
					setupLog.Error(err, "Failed to sync logger")
				}
			}()
			logger.ConfigureControllerRuntime(zapLogger)

			// Initialize scheme
			scheme := manager.InitScheme()

			// Configure TLS options
			tlsOptions := tls.ConfigureTLS(cfg)

			// Set up metrics server
			metricsOpts, metricsCertWatcher, err := metrics.SetupMetricsServer(cfg, tlsOptions)
			if err != nil {
				setupLog.Error(err, "unable to set up metrics server")
				os.Exit(1)
			}
			setupLog.Info("Metrics server configured", "address", metricsOpts.BindAddress)

			// Set up webhook server
			webhookServer, webhookCertWatcher := webhook.SetupWebhookServer(cfg, tlsOptions)

			// Create controller manager
			mgr, err := manager.SetupManager(cfg, scheme, metricsOpts, webhookServer)

			if err != nil {
				setupLog.Error(err, "unable to start manager")
				os.Exit(1)
			}

			// Set up controllers
			if err := manager.SetupControllers(mgr); err != nil {
				setupLog.Error(err, "unable to set up controllers")
				os.Exit(1)
			}

			// Add certificate watchers to manager
			if err := manager.AddCertWatchers(mgr, metricsCertWatcher, webhookCertWatcher); err != nil {
				setupLog.Error(err, "unable to add certificate watchers")
				os.Exit(1)
			}
			if err := v1alpha1.SetupClusterResourceQuotaWebhookWithManager(mgr); err != nil {
				setupLog.Error(err, "unable to set up ClusterResourceQuota webhook")
				os.Exit(1)
			}
			if err := v1alpha1.SetupNamespaceWebhookWithManager(mgr); err != nil {
				setupLog.Error(err, "unable to set up Namespace webhook")
				os.Exit(1)
			}
			setupLog.Info("Webhook configured", "port", cfg.WebhookPort)
			// Start the manager
			manager.Start(mgr)
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
