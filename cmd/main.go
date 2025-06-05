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
	"crypto/tls"
	"fmt"
	"os"
	"path/filepath"
	"runtime"
	"strings"

	// Import all Kubernetes client auth plugins (e.g. Azure, GCP, OIDC, etc.)
	// to ensure that exec-entrypoint and run can make use of them.
	_ "k8s.io/client-go/plugin/pkg/client/auth"

	"github.com/spf13/cobra"
	"github.com/spf13/viper"
	"go.uber.org/zap"
	"go.uber.org/zap/zapcore"
	k8sruntime "k8s.io/apimachinery/pkg/runtime"
	utilruntime "k8s.io/apimachinery/pkg/util/runtime"
	clientgoscheme "k8s.io/client-go/kubernetes/scheme"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/certwatcher"
	"sigs.k8s.io/controller-runtime/pkg/healthz"
	ctrlzap "sigs.k8s.io/controller-runtime/pkg/log/zap"
	"sigs.k8s.io/controller-runtime/pkg/metrics/filters"
	metricsserver "sigs.k8s.io/controller-runtime/pkg/metrics/server"
	"sigs.k8s.io/controller-runtime/pkg/webhook"

	quotav1alpha1 "github.com/powerhome/pac-quota-controller/api/v1alpha1"
	"github.com/powerhome/pac-quota-controller/internal/controller"
	// +kubebuilder:scaffold:imports
)

var (
	scheme   = k8sruntime.NewScheme()
	setupLog = ctrl.Log.WithName("setup")
)

func init() {
	utilruntime.Must(clientgoscheme.AddToScheme(scheme))

	utilruntime.Must(quotav1alpha1.AddToScheme(scheme))
	// +kubebuilder:scaffold:scheme
}

// Config holds the controller configuration
type Config struct {
	MetricsAddr          string
	MetricsCertPath      string
	MetricsCertName      string
	MetricsCertKey       string
	WebhookCertPath      string
	WebhookCertName      string
	WebhookCertKey       string
	EnableLeaderElection bool
	ProbeAddr            string
	SecureMetrics        bool
	EnableHTTP2          bool
	LogLevel             string
	LogFormat            string
}

// initConfig initializes viper configuration with environment variables support
func initConfig() *Config {
	viper.SetEnvKeyReplacer(strings.NewReplacer("-", "_"))
	viper.AutomaticEnv()

	// Define defaults
	viper.SetDefault("metrics-bind-address", "0")
	viper.SetDefault("health-probe-bind-address", ":8081")
	viper.SetDefault("leader-elect", false)
	viper.SetDefault("metrics-secure", true)
	viper.SetDefault("webhook-cert-name", "tls.crt")
	viper.SetDefault("webhook-cert-key", "tls.key")
	viper.SetDefault("metrics-cert-name", "tls.crt")
	viper.SetDefault("metrics-cert-key", "tls.key")
	viper.SetDefault("enable-http2", false)
	viper.SetDefault("log-level", "info")
	viper.SetDefault("log-format", "json")

	return &Config{
		MetricsAddr:          viper.GetString("metrics-bind-address"),
		ProbeAddr:            viper.GetString("health-probe-bind-address"),
		EnableLeaderElection: viper.GetBool("leader-elect"),
		SecureMetrics:        viper.GetBool("metrics-secure"),
		WebhookCertPath:      viper.GetString("webhook-cert-path"),
		WebhookCertName:      viper.GetString("webhook-cert-name"),
		WebhookCertKey:       viper.GetString("webhook-cert-key"),
		MetricsCertPath:      viper.GetString("metrics-cert-path"),
		MetricsCertName:      viper.GetString("metrics-cert-name"),
		MetricsCertKey:       viper.GetString("metrics-cert-key"),
		EnableHTTP2:          viper.GetBool("enable-http2"),
		LogLevel:             viper.GetString("log-level"),
		LogFormat:            viper.GetString("log-format"),
	}
}

// setupFlags binds cobra flags to viper
func setupFlags(cmd *cobra.Command) {
	cmd.Flags().String("metrics-bind-address", "0", "The address the metrics endpoint binds to. "+
		"Use :8443 for HTTPS or :8080 for HTTP, or leave as 0 to disable the metrics service.")
	cmd.Flags().String("health-probe-bind-address", ":8081", "The address the probe endpoint binds to.")
	cmd.Flags().Bool("leader-elect", false,
		"Enable leader election for controller manager. "+
			"Enabling this will ensure there is only one active controller manager.")
	cmd.Flags().Bool("metrics-secure", true,
		"If set, the metrics endpoint is served securely via HTTPS. Use --metrics-secure=false to use HTTP instead.")
	cmd.Flags().String("webhook-cert-path", "", "The directory that contains the webhook certificate.")
	cmd.Flags().String("webhook-cert-name", "tls.crt", "The name of the webhook certificate file.")
	cmd.Flags().String("webhook-cert-key", "tls.key", "The name of the webhook key file.")
	cmd.Flags().String("metrics-cert-path", "",
		"The directory that contains the metrics server certificate.")
	cmd.Flags().String("metrics-cert-name", "tls.crt", "The name of the metrics server certificate file.")
	cmd.Flags().String("metrics-cert-key", "tls.key", "The name of the metrics server key file.")
	cmd.Flags().Bool("enable-http2", false,
		"If set, HTTP/2 will be enabled for the metrics and webhook servers")
	cmd.Flags().String("log-level", "info", "Log level (debug, info, warn, error)")
	cmd.Flags().String("log-format", "json", "Log format (json or console)")

	// Bind flags to viper
	if err := viper.BindPFlags(cmd.Flags()); err != nil {
		setupLog.Error(err, "unable to bind flags to viper")
		os.Exit(1)
	}
}

// setupLogger configures the zap logger based on provided configuration
func setupLogger(config *Config) *zap.Logger {
	// Set the log level
	var level zapcore.Level
	switch strings.ToLower(config.LogLevel) {
	case "debug":
		level = zapcore.DebugLevel
	case "info":
		level = zapcore.InfoLevel
	case "warn":
		level = zapcore.WarnLevel
	case "error":
		level = zapcore.ErrorLevel
	default:
		level = zapcore.InfoLevel
	}

	// Create encoder configuration
	encoderConfig := zap.NewProductionEncoderConfig()
	encoderConfig.TimeKey = "timestamp"
	encoderConfig.EncodeTime = zapcore.ISO8601TimeEncoder

	// Configure the encoder based on the format
	var encoder zapcore.Encoder
	if config.LogFormat == "console" {
		encoder = zapcore.NewConsoleEncoder(encoderConfig)
	} else {
		encoder = zapcore.NewJSONEncoder(encoderConfig)
	}

	// Create the core
	core := zapcore.NewCore(encoder, zapcore.AddSync(os.Stdout), level)

	// Create the logger
	return zap.New(core)
}

// Build information. Populated at build-time by GoReleaser.
var (
	version = "dev"
	commit  = "none"
	date    = "unknown"
	builtBy = "unknown"
)

func newVersionCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "version",
		Short: "Print version information",
		Run: func(cmd *cobra.Command, args []string) {
			fmt.Printf("Version:\t%s\n", version)
			fmt.Printf("Git Commit:\t%s\n", commit)
			fmt.Printf("Build Date:\t%s\n", date)
			fmt.Printf("Built By:\t%s\n", builtBy)
			fmt.Printf("Go Version:\t%s\n", runtime.Version())
			fmt.Printf("Platform:\t%s/%s\n", runtime.GOOS, runtime.GOARCH)
		},
	}
}

// nolint:gocyclo
func main() {
	// Create root command
	rootCmd := &cobra.Command{
		Use:   "controller-manager",
		Short: "Cluster Resource Quota controller manager",
		Long:  "Manages ClusterResourceQuota resources that provide quota limits across multiple namespaces",
		Run: func(cmd *cobra.Command, args []string) {
			config := initConfig()
			logger := setupLogger(config)
			defer func() {
				if err := logger.Sync(); err != nil {
					setupLog.Error(err, "Failed to sync logger")
				}
			}()

			// Convert uber/zap logger to controller-runtime logger
			encoderConfig := zap.NewProductionEncoderConfig()
			encoder := zapcore.NewJSONEncoder(encoderConfig)
			zapLogger := ctrlzap.New(ctrlzap.UseDevMode(false), ctrlzap.Encoder(encoder))
			ctrl.SetLogger(zapLogger)

			var tlsOpts []func(*tls.Config)

			// if the enable-http2 flag is false (the default), http/2 should be disabled
			// due to its vulnerabilities. More specifically, disabling http/2 will
			// prevent from being vulnerable to the HTTP/2 Stream Cancellation and
			// Rapid Reset CVEs. For more information see:
			// - https://github.com/advisories/GHSA-qppj-fm5r-hxr3
			// - https://github.com/advisories/GHSA-4374-p667-p6c8
			disableHTTP2 := func(c *tls.Config) {
				setupLog.Info("disabling http/2")
				c.NextProtos = []string{"http/1.1"}
			}

			if !config.EnableHTTP2 {
				tlsOpts = append(tlsOpts, disableHTTP2)
			}

			// Create watchers for metrics and webhooks certificates
			var metricsCertWatcher, webhookCertWatcher *certwatcher.CertWatcher

			// Initial webhook TLS options
			webhookTLSOpts := tlsOpts

			if len(config.WebhookCertPath) > 0 {
				setupLog.Info("Initializing webhook certificate watcher using provided certificates",
					"webhook-cert-path", config.WebhookCertPath,
					"webhook-cert-name", config.WebhookCertName,
					"webhook-cert-key", config.WebhookCertKey)

				var err error
				webhookCertWatcher, err = certwatcher.New(
					filepath.Join(config.WebhookCertPath, config.WebhookCertName),
					filepath.Join(config.WebhookCertPath, config.WebhookCertKey),
				)
				if err != nil {
					setupLog.Error(err, "Failed to initialize webhook certificate watcher")
					os.Exit(1)
				}

				webhookTLSOpts = append(webhookTLSOpts, func(config *tls.Config) {
					config.GetCertificate = webhookCertWatcher.GetCertificate
				})
			}

			webhookServer := webhook.NewServer(webhook.Options{
				TLSOpts: webhookTLSOpts,
			})

			// Metrics endpoint is enabled in 'config/default/kustomization.yaml'. The Metrics options configure the server.
			// More info:
			// - https://pkg.go.dev/sigs.k8s.io/controller-runtime@v0.21.0/pkg/metrics/server
			// - https://book.kubebuilder.io/reference/metrics.html
			metricsServerOptions := metricsserver.Options{
				BindAddress:   config.MetricsAddr,
				SecureServing: config.SecureMetrics,
				TLSOpts:       tlsOpts,
			}

			if config.SecureMetrics {
				// FilterProvider is used to protect the metrics endpoint with authn/authz.
				// These configurations ensure that only authorized users and service accounts
				// can access the metrics endpoint. The RBAC are configured in 'config/rbac/kustomization.yaml'. More info:
				// https://pkg.go.dev/sigs.k8s.io/controller-runtime@v0.21.0/pkg/metrics/filters#WithAuthenticationAndAuthorization
				metricsServerOptions.FilterProvider = filters.WithAuthenticationAndAuthorization
			}

			// If the certificate is not specified, controller-runtime will automatically
			// generate self-signed certificates for the metrics server. While convenient for development and testing,
			// this setup is not recommended for production.
			//
			// TODO(user): If you enable certManager, uncomment the following lines:
			// - [METRICS-WITH-CERTS] at config/default/kustomization.yaml to generate and use certificates
			// managed by cert-manager for the metrics server.
			// - [PROMETHEUS-WITH-CERTS] at config/prometheus/kustomization.yaml for TLS certification.
			if len(config.MetricsCertPath) > 0 {
				setupLog.Info("Initializing metrics certificate watcher using provided certificates",
					"metrics-cert-path", config.MetricsCertPath,
					"metrics-cert-name", config.MetricsCertName,
					"metrics-cert-key", config.MetricsCertKey)

				var err error
				metricsCertWatcher, err = certwatcher.New(
					filepath.Join(config.MetricsCertPath, config.MetricsCertName),
					filepath.Join(config.MetricsCertPath, config.MetricsCertKey),
				)
				if err != nil {
					setupLog.Error(err, "to initialize metrics certificate watcher", "error", err)
					os.Exit(1)
				}

				metricsServerOptions.TLSOpts = append(metricsServerOptions.TLSOpts, func(config *tls.Config) {
					config.GetCertificate = metricsCertWatcher.GetCertificate
				})
			}

			mgr, err := ctrl.NewManager(ctrl.GetConfigOrDie(), ctrl.Options{
				Scheme:                 scheme,
				Metrics:                metricsServerOptions,
				WebhookServer:          webhookServer,
				HealthProbeBindAddress: config.ProbeAddr,
				LeaderElection:         config.EnableLeaderElection,
				LeaderElectionID:       "81307769.powerapp.cloud",
				// LeaderElectionReleaseOnCancel defines if the leader should step down voluntarily
				// when the Manager ends. This requires the binary to immediately end when the
				// Manager is stopped, otherwise, this setting is unsafe. Setting this significantly
				// speeds up voluntary leader transitions as the new leader don't have to wait
				// LeaseDuration time first.
				//
				// In the default scaffold provided, the program ends immediately after
				// the manager stops, so would be fine to enable this option. However,
				// if you are doing or is intended to do any operation such as perform cleanups
				// after the manager stops then its usage might be unsafe.
				// LeaderElectionReleaseOnCancel: true,
			})
			if err != nil {
				setupLog.Error(err, "unable to start manager")
				os.Exit(1)
			}

			if err := (&controller.ClusterResourceQuotaReconciler{
				Client: mgr.GetClient(),
				Scheme: mgr.GetScheme(),
			}).SetupWithManager(mgr); err != nil {
				setupLog.Error(err, "unable to create controller", "controller", "ClusterResourceQuota")
				os.Exit(1)
			}
			// +kubebuilder:scaffold:builder

			if metricsCertWatcher != nil {
				setupLog.Info("Adding metrics certificate watcher to manager")
				if err := mgr.Add(metricsCertWatcher); err != nil {
					setupLog.Error(err, "unable to add metrics certificate watcher to manager")
					os.Exit(1)
				}
			}

			if webhookCertWatcher != nil {
				setupLog.Info("Adding webhook certificate watcher to manager")
				if err := mgr.Add(webhookCertWatcher); err != nil {
					setupLog.Error(err, "unable to add webhook certificate watcher to manager")
					os.Exit(1)
				}
			}

			if err := mgr.AddHealthzCheck("healthz", healthz.Ping); err != nil {
				setupLog.Error(err, "unable to set up health check")
				os.Exit(1)
			}
			if err := mgr.AddReadyzCheck("readyz", healthz.Ping); err != nil {
				setupLog.Error(err, "unable to set up ready check")
				os.Exit(1)
			}

			setupLog.Info("starting manager")
			if err := mgr.Start(ctrl.SetupSignalHandler()); err != nil {
				setupLog.Error(err, "problem running manager")
				os.Exit(1)
			}
		},
	}

	// Add version command
	rootCmd.AddCommand(newVersionCmd())

	// Setup flags
	setupFlags(rootCmd)

	// Execute the root command
	if err := rootCmd.Execute(); err != nil {
		fmt.Println(err)
		os.Exit(1)
	}
}
