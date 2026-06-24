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
			"HEALTH_PROBE_BIND_ADDRESS",
			"LEADER_ELECT",
			"WEBHOOK_CERT_PATH",
			"WEBHOOK_CERT_NAME",
			"WEBHOOK_CERT_KEY",
			"ENABLE_HTTP2",
			"PPROF_BIND_ADDRESS",
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
		Expect(cfg.ProbeAddr).To(Equal(":8081"))
		Expect(cfg.EnableLeaderElection).To(BeFalse())
		Expect(cfg.WebhookCertName).To(Equal("tls.crt"))
		Expect(cfg.WebhookCertKey).To(Equal("tls.key"))
		Expect(cfg.EnableHTTP2).To(BeFalse())
		Expect(cfg.PprofBindAddress).To(Equal("0"))
		Expect(cfg.LogLevel).To(Equal("info"))
		Expect(cfg.LogFormat).To(Equal("json"))
	})

	It("should read values from environment variables", func() {
		// Set environment variables
		envVars := map[string]string{
			"HEALTH_PROBE_BIND_ADDRESS": ":9090",
			"LEADER_ELECT":              "true",
			"WEBHOOK_CERT_PATH":         "/certs/webhook",
			"WEBHOOK_CERT_NAME":         "cert.pem",
			"WEBHOOK_CERT_KEY":          "key.pem",
			"ENABLE_HTTP2":              "true",
			"PPROF_BIND_ADDRESS":        ":6060",
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
		Expect(cfg.WebhookCertPath).To(Equal("/certs/webhook"))
		Expect(cfg.WebhookCertName).To(Equal("cert.pem"))
		Expect(cfg.WebhookCertKey).To(Equal("key.pem"))
		Expect(cfg.EnableHTTP2).To(BeTrue())
		Expect(cfg.PprofBindAddress).To(Equal(":6060"))
		Expect(cfg.LogLevel).To(Equal("debug"))
		Expect(cfg.LogFormat).To(Equal("console"))
	})
})

var _ = Describe("InitConfig extended fields", func() {
	BeforeEach(func() {
		viper.Reset()
	})
	AfterEach(func() {
		viper.Reset()
	})

	It("parses excluded-namespaces, trimming spaces and skipping empties", func() {
		Expect(os.Setenv("EXCLUDED_NAMESPACES", "ns1, ns2 ,, ns3")).To(Succeed())
		DeferCleanup(func() { _ = os.Unsetenv("EXCLUDED_NAMESPACES") })

		viper.Reset()
		cfg := InitConfig()
		Expect(cfg.ExcludedNamespaces).To(Equal([]string{"ns1", "ns2", "ns3"}))
	})

	It("leaves excluded-namespaces empty when unset", func() {
		viper.Reset()
		cfg := InitConfig()
		Expect(cfg.ExcludedNamespaces).To(BeEmpty())
	})

	It("defaults the leader-election timings", func() {
		viper.Reset()
		cfg := InitConfig()
		Expect(cfg.LeaderElectionLeaseDuration).To(Equal(60))
		Expect(cfg.LeaderElectionRenewDeadline).To(Equal(40))
		Expect(cfg.LeaderElectionRetryPeriod).To(Equal(10))
	})

	It("defaults the events configuration", func() {
		viper.Reset()
		cfg := InitConfig()
		Expect(cfg.EventsEnable).To(BeTrue())
		Expect(cfg.EventsTTL).To(Equal("24h"))
		Expect(cfg.EventsMaxEventsPerCRQ).To(Equal(100))
		Expect(cfg.EventsCleanupInterval).To(Equal("1h"))
	})

	It("reads events configuration from the environment", func() {
		envVars := map[string]string{
			"EVENTS_ENABLE":             "false",
			"EVENTS_TTL":                "48h",
			"EVENTS_MAX_EVENTS_PER_CRQ": "50",
			"EVENTS_CLEANUP_INTERVAL":   "30m",
		}
		for k, v := range envVars {
			Expect(os.Setenv(k, v)).To(Succeed())
		}
		DeferCleanup(func() {
			for k := range envVars {
				_ = os.Unsetenv(k)
			}
		})

		viper.Reset()
		cfg := InitConfig()
		Expect(cfg.EventsEnable).To(BeFalse())
		Expect(cfg.EventsTTL).To(Equal("48h"))
		Expect(cfg.EventsMaxEventsPerCRQ).To(Equal(50))
		Expect(cfg.EventsCleanupInterval).To(Equal("30m"))
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

		probeAddr, err := flags.GetString("health-probe-bind-address")
		Expect(err).NotTo(HaveOccurred())
		Expect(probeAddr).To(Equal(":8081"))

		leaderElect, err := flags.GetBool("leader-elect")
		Expect(err).NotTo(HaveOccurred())
		Expect(leaderElect).To(BeFalse())

		logLevel, err := flags.GetString("log-level")
		Expect(err).NotTo(HaveOccurred())
		Expect(logLevel).To(Equal("info"))

		logFormat, err := flags.GetString("log-format")
		Expect(err).NotTo(HaveOccurred())
		Expect(logFormat).To(Equal("json"))

		pprofBindAddress, err := flags.GetString("pprof-bind-address")
		Expect(err).NotTo(HaveOccurred())
		Expect(pprofBindAddress).To(Equal("0"))
	})
})
