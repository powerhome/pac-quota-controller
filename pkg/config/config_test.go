package config

import (
	"os"
	"testing"

	"github.com/spf13/cobra"
	"github.com/spf13/viper"
	"github.com/stretchr/testify/assert"
)

func TestInitConfig(t *testing.T) {
	// Reset viper before the test to ensure clean state
	viper.Reset()

	// Test with default values
	cfg := InitConfig()
	assert.Equal(t, "0", cfg.MetricsAddr)
	assert.Equal(t, ":8081", cfg.ProbeAddr)
	assert.Equal(t, false, cfg.EnableLeaderElection)
	assert.Equal(t, true, cfg.SecureMetrics)
	assert.Equal(t, "tls.crt", cfg.WebhookCertName)
	assert.Equal(t, "tls.key", cfg.WebhookCertKey)
	assert.Equal(t, "tls.crt", cfg.MetricsCertName)
	assert.Equal(t, "tls.key", cfg.MetricsCertKey)
	assert.Equal(t, false, cfg.EnableHTTP2)
	assert.Equal(t, "info", cfg.LogLevel)
	assert.Equal(t, "json", cfg.LogFormat)

	// Test with environment variables
	if err := os.Setenv("METRICS_BIND_ADDRESS", ":8443"); err != nil {
		t.Fatalf("failed to set env: %v", err)
	}
	if err := os.Setenv("HEALTH_PROBE_BIND_ADDRESS", ":9090"); err != nil {
		t.Fatalf("failed to set env: %v", err)
	}
	if err := os.Setenv("LEADER_ELECT", "true"); err != nil {
		t.Fatalf("failed to set env: %v", err)
	}
	if err := os.Setenv("METRICS_SECURE", "false"); err != nil {
		t.Fatalf("failed to set env: %v", err)
	}
	if err := os.Setenv("WEBHOOK_CERT_PATH", "/certs/webhook"); err != nil {
		t.Fatalf("failed to set env: %v", err)
	}
	if err := os.Setenv("WEBHOOK_CERT_NAME", "cert.pem"); err != nil {
		t.Fatalf("failed to set env: %v", err)
	}
	if err := os.Setenv("WEBHOOK_CERT_KEY", "key.pem"); err != nil {
		t.Fatalf("failed to set env: %v", err)
	}
	if err := os.Setenv("METRICS_CERT_PATH", "/certs/metrics"); err != nil {
		t.Fatalf("failed to set env: %v", err)
	}
	if err := os.Setenv("METRICS_CERT_NAME", "metrics.crt"); err != nil {
		t.Fatalf("failed to set env: %v", err)
	}
	if err := os.Setenv("METRICS_CERT_KEY", "metrics.key"); err != nil {
		t.Fatalf("failed to set env: %v", err)
	}
	if err := os.Setenv("ENABLE_HTTP2", "true"); err != nil {
		t.Fatalf("failed to set env: %v", err)
	}
	if err := os.Setenv("LOG_LEVEL", "debug"); err != nil {
		t.Fatalf("failed to set env: %v", err)
	}
	if err := os.Setenv("LOG_FORMAT", "console"); err != nil {
		t.Fatalf("failed to set env: %v", err)
	}

	// Reset viper to pick up the new environment variables
	viper.Reset()

	// Initialize again with environment variables
	cfg = InitConfig()
	assert.Equal(t, ":8443", cfg.MetricsAddr)
	assert.Equal(t, ":9090", cfg.ProbeAddr)
	assert.Equal(t, true, cfg.EnableLeaderElection)
	assert.Equal(t, false, cfg.SecureMetrics)
	assert.Equal(t, "/certs/webhook", cfg.WebhookCertPath)
	assert.Equal(t, "cert.pem", cfg.WebhookCertName)
	assert.Equal(t, "key.pem", cfg.WebhookCertKey)
	assert.Equal(t, "/certs/metrics", cfg.MetricsCertPath)
	assert.Equal(t, "metrics.crt", cfg.MetricsCertName)
	assert.Equal(t, "metrics.key", cfg.MetricsCertKey)
	assert.Equal(t, true, cfg.EnableHTTP2)
	assert.Equal(t, "debug", cfg.LogLevel)
	assert.Equal(t, "console", cfg.LogFormat)

	// Clean up
	if err := os.Unsetenv("METRICS_BIND_ADDRESS"); err != nil {
		t.Fatalf("failed to unset env: %v", err)
	}
	if err := os.Unsetenv("HEALTH_PROBE_BIND_ADDRESS"); err != nil {
		t.Fatalf("failed to unset env: %v", err)
	}
	if err := os.Unsetenv("LEADER_ELECT"); err != nil {
		t.Fatalf("failed to unset env: %v", err)
	}
	if err := os.Unsetenv("METRICS_SECURE"); err != nil {
		t.Fatalf("failed to unset env: %v", err)
	}
	if err := os.Unsetenv("WEBHOOK_CERT_PATH"); err != nil {
		t.Fatalf("failed to unset env: %v", err)
	}
	if err := os.Unsetenv("WEBHOOK_CERT_NAME"); err != nil {
		t.Fatalf("failed to unset env: %v", err)
	}
	if err := os.Unsetenv("WEBHOOK_CERT_KEY"); err != nil {
		t.Fatalf("failed to unset env: %v", err)
	}
	if err := os.Unsetenv("METRICS_CERT_PATH"); err != nil {
		t.Fatalf("failed to unset env: %v", err)
	}
	if err := os.Unsetenv("METRICS_CERT_NAME"); err != nil {
		t.Fatalf("failed to unset env: %v", err)
	}
	if err := os.Unsetenv("METRICS_CERT_KEY"); err != nil {
		t.Fatalf("failed to unset env: %v", err)
	}
	if err := os.Unsetenv("ENABLE_HTTP2"); err != nil {
		t.Fatalf("failed to unset env: %v", err)
	}
	if err := os.Unsetenv("LOG_LEVEL"); err != nil {
		t.Fatalf("failed to unset env: %v", err)
	}
	if err := os.Unsetenv("LOG_FORMAT"); err != nil {
		t.Fatalf("failed to unset env: %v", err)
	}
}

func TestSetupFlags(t *testing.T) {
	// Reset viper before the test
	viper.Reset()

	// Create a new cobra command
	cmd := &cobra.Command{
		Use:   "test",
		Short: "Test command for SetupFlags",
		Run:   func(cmd *cobra.Command, args []string) {},
	}

	// Setup flags
	SetupFlags(cmd)

	// Check that all flags were registered
	flags := cmd.Flags()
	assert.True(t, flags.HasAvailableFlags())

	// Check a few specific flags
	metricsAddr, _ := flags.GetString("metrics-bind-address")
	assert.Equal(t, "0", metricsAddr)

	probeAddr, _ := flags.GetString("health-probe-bind-address")
	assert.Equal(t, ":8081", probeAddr)

	leaderElect, _ := flags.GetBool("leader-elect")
	assert.Equal(t, false, leaderElect)

	secureMetrics, _ := flags.GetBool("metrics-secure")
	assert.Equal(t, true, secureMetrics)

	logLevel, _ := flags.GetString("log-level")
	assert.Equal(t, "info", logLevel)

	logFormat, _ := flags.GetString("log-format")
	assert.Equal(t, "json", logFormat)
}
