package webhook

import (
	"os"
	"path/filepath"

	"github.com/powerhome/pac-quota-controller/pkg/config"
	"go.uber.org/zap"
	"k8s.io/client-go/kubernetes"
	"k8s.io/client-go/kubernetes/fake"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
)

var _ = Describe("Webhook", func() {
	var (
		fakeClient kubernetes.Interface
		logger     *zap.Logger
		cfg        *config.Config
		tempDir    string
	)

	BeforeEach(func() {
		fakeClient = fake.NewSimpleClientset()
		logger, _ = zap.NewDevelopment()
		cfg = &config.Config{
			WebhookPort:     8443,
			WebhookCertPath: "",
			WebhookCertName: "tls.crt",
			WebhookCertKey:  "tls.key",
			LogLevel:        "info",
		}

		// Create temp directory for certificate tests
		tempDir, _ = os.MkdirTemp("", "webhook-test")
	})

	AfterEach(func() {
		if tempDir != "" {
			err := os.RemoveAll(tempDir)
			Expect(err).NotTo(HaveOccurred())
		}
	})

	Describe("SetupGinWebhookServer", func() {
		It("should setup webhook server without certificates", func() {
			server, certWatcher := SetupGinWebhookServer(cfg, fakeClient, logger)

			Expect(server).NotTo(BeNil())
			Expect(certWatcher).To(BeNil())
		})

		It("should setup webhook server with certificates", func() {
			// Create dummy certificate files
			certPath := filepath.Join(tempDir, "tls.crt")
			keyPath := filepath.Join(tempDir, "tls.key")

			err := os.WriteFile(certPath, []byte("dummy cert"), 0644)
			Expect(err).NotTo(HaveOccurred())
			err = os.WriteFile(keyPath, []byte("dummy key"), 0644)
			Expect(err).NotTo(HaveOccurred())

			cfg.WebhookCertPath = tempDir

			server, certWatcher := SetupGinWebhookServer(cfg, fakeClient, logger)

			Expect(server).NotTo(BeNil())
			// CertWatcher will be nil since the certificate files are invalid
			Expect(certWatcher).To(BeNil())
		})

		It("should handle certificate watcher setup failure", func() {
			// Set certificate path to non-existent directory
			cfg.WebhookCertPath = "/non/existent/path"

			server, certWatcher := SetupGinWebhookServer(cfg, fakeClient, logger)

			Expect(server).NotTo(BeNil())
			Expect(certWatcher).To(BeNil())
		})

		It("should handle debug log level", func() {
			cfg.LogLevel = "debug"
			server, certWatcher := SetupGinWebhookServer(cfg, fakeClient, logger)

			Expect(server).NotTo(BeNil())
			Expect(certWatcher).To(BeNil())
		})

		It("should handle nil client", func() {
			server, certWatcher := SetupGinWebhookServer(cfg, nil, logger)

			Expect(server).NotTo(BeNil())
			Expect(certWatcher).To(BeNil())
		})

		It("should handle nil logger", func() {
			server, certWatcher := SetupGinWebhookServer(cfg, fakeClient, nil)

			Expect(server).NotTo(BeNil())
			Expect(certWatcher).To(BeNil())
		})

		It("should handle empty certificate path", func() {
			cfg.WebhookCertPath = ""
			server, certWatcher := SetupGinWebhookServer(cfg, fakeClient, logger)

			Expect(server).NotTo(BeNil())
			Expect(certWatcher).To(BeNil())
		})

		It("should handle certificate setup error", func() {
			// Create invalid certificate files
			certPath := filepath.Join(tempDir, "tls.crt")
			keyPath := filepath.Join(tempDir, "tls.key")

			// Create files but make them invalid
			err := os.WriteFile(certPath, []byte("invalid cert"), 0644)
			Expect(err).NotTo(HaveOccurred())
			err = os.WriteFile(keyPath, []byte("invalid key"), 0644)
			Expect(err).NotTo(HaveOccurred())

			cfg.WebhookCertPath = tempDir

			server, certWatcher := SetupGinWebhookServer(cfg, fakeClient, logger)

			Expect(server).NotTo(BeNil())
			Expect(certWatcher).To(BeNil())
		})
	})
})
