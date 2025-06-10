package config

import (
	"os"
	"strings"

	"github.com/spf13/cobra"
	"github.com/spf13/viper"
	logf "sigs.k8s.io/controller-runtime/pkg/log"
)

var setupLog = logf.Log.WithName("setup.config")

// Config holds the controller configuration
type Config struct {
	MetricsAddr          string
	MetricsCertPath      string
	MetricsCertName      string
	MetricsCertKey       string
	WebhookCertPath      string
	WebhookCertName      string
	WebhookCertKey       string
	WebhookPort          int
	EnableLeaderElection bool
	ProbeAddr            string
	SecureMetrics        bool
	EnableHTTP2          bool
	LogLevel             string
	LogFormat            string
}

// setDefaults configures the default values for configuration parameters
func setDefaults() {
	viper.SetDefault("metrics-bind-address", "0")
	viper.SetDefault("health-probe-bind-address", ":8081")
	viper.SetDefault("leader-elect", false)
	viper.SetDefault("metrics-secure", true)
	viper.SetDefault("webhook-cert-name", "tls.crt")
	viper.SetDefault("webhook-cert-key", "tls.key")
	viper.SetDefault("webhook-port", 9443)
	viper.SetDefault("metrics-cert-name", "tls.crt")
	viper.SetDefault("metrics-cert-key", "tls.key")
	viper.SetDefault("enable-http2", false)
	viper.SetDefault("log-level", "info")
	viper.SetDefault("log-format", "json")
}

// InitConfig initializes viper configuration with environment variables support
func InitConfig() *Config {
	viper.SetEnvKeyReplacer(strings.NewReplacer("-", "_"))
	viper.AutomaticEnv()

	// Define defaults
	setDefaults()

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
		WebhookPort:          viper.GetInt("webhook-port"),
	}
}

// SetupFlags binds cobra flags to viper
func SetupFlags(cmd *cobra.Command) {
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
	cmd.Flags().Int("webhook-port", 9443, "The port the webhook server listens on.")

	// Bind flags to viper
	if err := viper.BindPFlags(cmd.Flags()); err != nil {
		setupLog.Error(err, "unable to bind flags to viper")
		os.Exit(1)
	}
}
