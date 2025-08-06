package webhook

import (
	"os"
	"path/filepath"
	"time"

	"github.com/powerhome/pac-quota-controller/pkg/config"
	"go.uber.org/zap"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/client-go/kubernetes"
	k8sfake "k8s.io/client-go/kubernetes/fake"
	"sigs.k8s.io/controller-runtime/pkg/client"
	ctrlclientfake "sigs.k8s.io/controller-runtime/pkg/client/fake"

	quotav1alpha1 "github.com/powerhome/pac-quota-controller/api/v1alpha1"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
)

var _ = Describe("Webhook", func() {
	var (
		fakeClient        kubernetes.Interface
		fakeRuntimeClient client.Client
		logger            *zap.Logger
		cfg               *config.Config
		tempDir           string
	)

	const debugLevel = "debug"

	BeforeEach(func() {
		fakeClient = k8sfake.NewSimpleClientset()
		scheme := runtime.NewScheme()
		_ = quotav1alpha1.AddToScheme(scheme)
		fakeRuntimeClient = ctrlclientfake.NewClientBuilder().WithScheme(scheme).Build()
		logger, _ = zap.NewDevelopment()
		cfg = &config.Config{
			WebhookPort:     8443,
			WebhookCertPath: "",
			WebhookCertName: "tls.crt",
			WebhookCertKey:  "tls.key",
			LogLevel:        "info",
			LogFormat:       "json",
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
			server, certWatcher := SetupGinWebhookServer(cfg, fakeClient, fakeRuntimeClient, logger)

			Expect(server).NotTo(BeNil())
			Expect(certWatcher).To(BeNil())
		})

		It("should setup webhook server with certificates", func() {
			// Create certificate files
			certFile := filepath.Join(tempDir, "tls.crt")
			keyFile := filepath.Join(tempDir, "tls.key")

			// Create dummy certificate files
			err := os.WriteFile(certFile, []byte("dummy cert"), 0600)
			Expect(err).NotTo(HaveOccurred())
			err = os.WriteFile(keyFile, []byte("dummy key"), 0600)
			Expect(err).NotTo(HaveOccurred())

			cfg.WebhookCertPath = tempDir
			server, certWatcher := SetupGinWebhookServer(cfg, fakeClient, fakeRuntimeClient, logger)

			Expect(server).NotTo(BeNil())
			// Certificate watcher should be nil since dummy files can't be decoded
			Expect(certWatcher).To(BeNil())
		})

		It("should handle certificate watcher setup failure", func() {
			// Set certificate path to non-existent directory
			cfg.WebhookCertPath = "/non/existent/path"

			server, certWatcher := SetupGinWebhookServer(cfg, fakeClient, fakeRuntimeClient, logger)

			Expect(server).NotTo(BeNil())
			Expect(certWatcher).To(BeNil())
		})

		It("should handle debug log level", func() {
			cfg.LogLevel = debugLevel
			server, certWatcher := SetupGinWebhookServer(cfg, fakeClient, fakeRuntimeClient, logger)

			Expect(server).NotTo(BeNil())
			Expect(certWatcher).To(BeNil())
		})

		It("should handle nil client", func() {
			server, certWatcher := SetupGinWebhookServer(cfg, nil, nil, logger)

			Expect(server).NotTo(BeNil())
			Expect(certWatcher).To(BeNil())
		})

		It("should handle nil logger", func() {
			server, certWatcher := SetupGinWebhookServer(cfg, fakeClient, fakeRuntimeClient, nil)

			Expect(server).NotTo(BeNil())
			Expect(certWatcher).To(BeNil())
		})

		It("should handle empty certificate path", func() {
			cfg.WebhookCertPath = ""
			server, certWatcher := SetupGinWebhookServer(cfg, fakeClient, fakeRuntimeClient, logger)

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

			server, certWatcher := SetupGinWebhookServer(cfg, fakeClient, fakeRuntimeClient, logger)

			Expect(server).NotTo(BeNil())
			Expect(certWatcher).To(BeNil())
		})

		It("should configure webhook initialization with proper timing", func() {
			server, _ := SetupGinWebhookServer(cfg, fakeClient, fakeRuntimeClient, logger)

			Expect(server).NotTo(BeNil())

			// Test that the server is properly configured and can be marked ready
			server.MarkReady()

			// Add a small delay to simulate real initialization timing
			time.Sleep(10 * time.Millisecond)

			// Server should still be valid after timing delays
			Expect(server).NotTo(BeNil())
		})
	})
})
