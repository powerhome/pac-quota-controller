package config

import (
	"os"
	"testing"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	"github.com/spf13/cobra"
	"github.com/spf13/viper"
)

func TestConfig(t *testing.T) {
	RegisterFailHandler(Fail)
	RunSpecs(t, "Config Package Suite")
}

var _ = Describe("InitConfig", func() {
	BeforeEach(func() {
		viper.Reset()
	})

	AfterEach(func() {
		// Clean up any environment variables that might have been set
		envVars := []string{
			"METRICS_BIND_ADDRESS",
			"HEALTH_PROBE_BIND_ADDRESS",
			"LEADER_ELECT",
			"METRICS_SECURE",
			"WEBHOOK_CERT_PATH",
			"WEBHOOK_CERT_NAME",
			"WEBHOOK_CERT_KEY",
			"METRICS_CERT_PATH",
			"METRICS_CERT_NAME",
			"METRICS_CERT_KEY",
			"ENABLE_HTTP2",
			"LOG_LEVEL",
			"LOG_FORMAT",
		}
		for _, env := range envVars {
			Expect(os.Unsetenv(env)).To(Succeed())
		}
		viper.Reset()
	})

	It("should initialize with default values", func() {
		cfg := InitConfig()
		Expect(cfg.MetricsAddr).To(Equal(":8443"))
		Expect(cfg.ProbeAddr).To(Equal(":8081"))
		Expect(cfg.EnableLeaderElection).To(BeFalse())
		Expect(cfg.SecureMetrics).To(BeTrue())
		Expect(cfg.WebhookCertName).To(Equal("tls.crt"))
		Expect(cfg.WebhookCertKey).To(Equal("tls.key"))
		Expect(cfg.MetricsCertName).To(Equal("tls.crt"))
		Expect(cfg.MetricsCertKey).To(Equal("tls.key"))
		Expect(cfg.EnableHTTP2).To(BeFalse())
		Expect(cfg.LogLevel).To(Equal("info"))
		Expect(cfg.LogFormat).To(Equal("json"))
	})

	It("should read values from environment variables", func() {
		// Set environment variables
		envVars := map[string]string{
			"HEALTH_PROBE_BIND_ADDRESS": ":9090",
			"LEADER_ELECT":              "true",
			"METRICS_SECURE":            "false",
			"WEBHOOK_CERT_PATH":         "/certs/webhook",
			"WEBHOOK_CERT_NAME":         "cert.pem",
			"WEBHOOK_CERT_KEY":          "key.pem",
			"METRICS_CERT_PATH":         "/certs/metrics",
			"METRICS_CERT_NAME":         "metrics.crt",
			"METRICS_CERT_KEY":          "metrics.key",
			"ENABLE_HTTP2":              "true",
			"LOG_LEVEL":                 "debug",
			"LOG_FORMAT":                "console",
		}

		for key, value := range envVars {
			Expect(os.Setenv(key, value)).To(Succeed())
		}

		viper.Reset()
		cfg := InitConfig()

		Expect(cfg.ProbeAddr).To(Equal(":9090"))
		Expect(cfg.EnableLeaderElection).To(BeTrue())
		Expect(cfg.SecureMetrics).To(BeFalse())
		Expect(cfg.WebhookCertPath).To(Equal("/certs/webhook"))
		Expect(cfg.WebhookCertName).To(Equal("cert.pem"))
		Expect(cfg.WebhookCertKey).To(Equal("key.pem"))
		Expect(cfg.MetricsCertPath).To(Equal("/certs/metrics"))
		Expect(cfg.MetricsCertName).To(Equal("metrics.crt"))
		Expect(cfg.MetricsCertKey).To(Equal("metrics.key"))
		Expect(cfg.EnableHTTP2).To(BeTrue())
		Expect(cfg.LogLevel).To(Equal("debug"))
		Expect(cfg.LogFormat).To(Equal("console"))
	})
})

var _ = Describe("SetupFlags", func() {
	var cmd *cobra.Command

	BeforeEach(func() {
		viper.Reset()
		cmd = &cobra.Command{
			Use:   "test",
			Short: "Test command for SetupFlags",
			Run:   func(cmd *cobra.Command, args []string) {},
		}
		SetupFlags(cmd)
	})

	It("should register all flags with correct defaults", func() {
		flags := cmd.Flags()
		Expect(flags.HasAvailableFlags()).To(BeTrue())

		metricsAddr, err := flags.GetString("metrics-bind-address")
		Expect(err).NotTo(HaveOccurred())
		Expect(metricsAddr).To(Equal("0"))

		probeAddr, err := flags.GetString("health-probe-bind-address")
		Expect(err).NotTo(HaveOccurred())
		Expect(probeAddr).To(Equal(":8081"))

		leaderElect, err := flags.GetBool("leader-elect")
		Expect(err).NotTo(HaveOccurred())
		Expect(leaderElect).To(BeFalse())

		secureMetrics, err := flags.GetBool("metrics-secure")
		Expect(err).NotTo(HaveOccurred())
		Expect(secureMetrics).To(BeTrue())

		logLevel, err := flags.GetString("log-level")
		Expect(err).NotTo(HaveOccurred())
		Expect(logLevel).To(Equal("info"))

		logFormat, err := flags.GetString("log-format")
		Expect(err).NotTo(HaveOccurred())
		Expect(logFormat).To(Equal("json"))
	})
})
