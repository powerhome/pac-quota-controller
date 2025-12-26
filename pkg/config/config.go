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
	MetricsEnable               bool
	EnableHTTP2                 bool
	EnableLeaderElection        bool
	ExcludeNamespaceLabelKey    string
	ExcludedNamespaces          []string
	LeaderElectionLeaseDuration int
	LeaderElectionNamespace     string
	LeaderElectionRenewDeadline int
	LeaderElectionRetryPeriod   int
	LogFormat                   string
	LogLevel                    string
	OwnNamespace                string
	ProbeAddr                   string
	WebhookCertKey              string
	WebhookCertName             string
	WebhookCertPath             string
	WebhookPort                 int
	// Events configuration
	EventsEnable          bool
	EventsConfigPath      string
	EventsTTL             string
	EventsMaxEventsPerCRQ int
	EventsCleanupInterval string
}

// setDefaults configures the default values for configuration parameters
func setDefaults() {
	viper.SetDefault("metrics-enable", true)
	viper.SetDefault("metrics-port", 8443)
	viper.SetDefault("health-probe-bind-address", ":8081")
	viper.SetDefault("leader-elect", false)
	viper.SetDefault("leader-election-lease-duration", 15)
	viper.SetDefault("leader-election-renew-deadline", 10)
	viper.SetDefault("leader-election-retry-period", 2)
	viper.SetDefault("metrics-secure", true)
	viper.SetDefault("webhook-cert-name", "tls.crt")
	viper.SetDefault("webhook-cert-key", "tls.key")
	viper.SetDefault("webhook-port", 9443)
	viper.SetDefault("metrics-cert-name", "tls.crt")
	viper.SetDefault("metrics-cert-key", "tls.key")
	viper.SetDefault("enable-http2", false)
	viper.SetDefault("log-level", "info")
	viper.SetDefault("log-format", "json")
	viper.SetDefault("exclude-namespace-label-key", "pac-quota-controller.powerapp.cloud/exclude")
	viper.SetDefault("excluded-namespaces", "")
	// Events defaults
	viper.SetDefault("events-enable", true)
	viper.SetDefault("events-config-path", "/etc/pac-quota-controller/events/event-config.yaml")
	viper.SetDefault("events-ttl", "24h")
	viper.SetDefault("events-max-events-per-crq", 100)
	viper.SetDefault("events-cleanup-interval", "1h")
}

// InitConfig initializes viper configuration with environment variables support
func InitConfig() *Config {
	viper.SetEnvKeyReplacer(strings.NewReplacer("-", "_"))
	viper.AutomaticEnv()

	// Define defaults
	setDefaults()

	var excluded []string
	if v := viper.GetString("excluded-namespaces"); v != "" {
		for _, ns := range strings.Split(v, ",") {
			ns = strings.TrimSpace(ns)
			if ns != "" {
				excluded = append(excluded, ns)
			}
		}
	}
	return &Config{
		EnableHTTP2:                 viper.GetBool("enable-http2"),
		MetricsEnable:               viper.GetBool("metrics-enable"),
		EnableLeaderElection:        viper.GetBool("leader-elect"),
		ExcludeNamespaceLabelKey:    viper.GetString("exclude-namespace-label-key"),
		ExcludedNamespaces:          excluded,
		LeaderElectionLeaseDuration: viper.GetInt("leader-election-lease-duration"),
		LeaderElectionNamespace:     viper.GetString("leader-election-namespace"),
		LeaderElectionRenewDeadline: viper.GetInt("leader-election-renew-deadline"),
		LeaderElectionRetryPeriod:   viper.GetInt("leader-election-retry-period"),
		LogFormat:                   viper.GetString("log-format"),
		LogLevel:                    viper.GetString("log-level"),
		OwnNamespace:                os.Getenv("POD_NAMESPACE"),
		ProbeAddr:                   viper.GetString("health-probe-bind-address"),
		WebhookCertKey:              viper.GetString("webhook-cert-key"),
		WebhookCertName:             viper.GetString("webhook-cert-name"),
		WebhookCertPath:             viper.GetString("webhook-cert-path"),
		WebhookPort:                 viper.GetInt("webhook-port"),
		// Events configuration
		EventsEnable:          viper.GetBool("events-enable"),
		EventsConfigPath:      viper.GetString("events-config-path"),
		EventsTTL:             viper.GetString("events-ttl"),
		EventsMaxEventsPerCRQ: viper.GetInt("events-max-events-per-crq"),
		EventsCleanupInterval: viper.GetString("events-cleanup-interval"),
	}
}

// SetupFlags binds cobra flags to viper
func SetupFlags(cmd *cobra.Command) {
	cmd.Flags().Bool("metrics-enable", true, "Enable the metrics server.")
	cmd.Flags().String("health-probe-bind-address", ":8081", "The address the probe endpoint binds to.")
	cmd.Flags().Bool("leader-elect", false,
		"Enable leader election for controller manager. "+
			"Enabling this will ensure there is only one active controller manager.")
	cmd.Flags().String("leader-election-namespace", "",
		"Namespace to use for leader election. If empty, uses the controller's namespace.")
	cmd.Flags().Int("leader-election-lease-duration", 15,
		"Duration in seconds that non-leader candidates will wait to force acquire leadership.")
	cmd.Flags().Int("leader-election-renew-deadline", 10,
		"Duration in seconds the leader will retry refreshing leadership before giving up.")
	cmd.Flags().Int("leader-election-retry-period", 2,
		"Duration in seconds the leader election clients should wait between tries of actions.")
	cmd.Flags().Int("metrics-port", 8443, "The port the metrics server listens on.")
	cmd.Flags().Bool("metrics-secure", true,
		"If set, the metrics endpoint is served securely via HTTPS. Use --metrics-secure=false to use HTTP instead.")
	cmd.Flags().String("webhook-cert-path", "", "The directory that contains the webhook certificate.")
	cmd.Flags().String("webhook-cert-name", "tls.crt", "The name of the webhook certificate file.")
	cmd.Flags().String("webhook-cert-key", "tls.key", "The name of the webhook key file.")
	cmd.Flags().String("metrics-cert-path", "",
		"The directory that contains the metrics server certificate (tls.crt/tls.key).")
	cmd.Flags().Bool("enable-http2", false,
		"If set, HTTP/2 will be enabled for the metrics and webhook servers")
	cmd.Flags().String("log-level", "info", "Log level (debug, info, warn, error)")
	cmd.Flags().String("log-format", "json", "Log format (json or console)")
	cmd.Flags().Int("webhook-port", 9443, "The port the webhook server listens on.")
	cmd.Flags().String(
		"exclude-namespace-label-key",
		"pac-quota-controller.powerapp.cloud/exclude",
		"The label key used to mark namespaces for exclusion. Any namespace with this label will be ignored.",
	)
	cmd.Flags().String(
		"excluded-namespaces",
		"",
		"Comma-separated list of namespaces to exclude from reconciliation and webhook validation.",
	)
	// Events configuration flags
	cmd.Flags().Bool("events-enable", true, "Enable Kubernetes Events recording.")
	cmd.Flags().String("events-config-path", "/etc/pac-quota-controller/events/event-config.yaml",
		"Path to the events configuration file.")
	cmd.Flags().String("events-ttl", "24h", "Time-to-live for events before cleanup.")
	cmd.Flags().Int("events-max-events-per-crq", 100, "Maximum number of events to retain per ClusterResourceQuota.")
	cmd.Flags().String("events-cleanup-interval", "1h", "Interval for running event cleanup.")

	// Bind flags to viper
	if err := viper.BindPFlags(cmd.Flags()); err != nil {
		setupLog.Error(err, "unable to bind flags to viper")
		os.Exit(1)
	}
}
