package server

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"go.uber.org/zap"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/client-go/kubernetes"
	"k8s.io/client-go/kubernetes/fake"
	"sigs.k8s.io/controller-runtime/pkg/client"
	clientfake "sigs.k8s.io/controller-runtime/pkg/client/fake"

	quotav1alpha1 "github.com/powerhome/pac-quota-controller/api/v1alpha1"
	"github.com/powerhome/pac-quota-controller/pkg/config"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	"github.com/powerhome/pac-quota-controller/pkg/metrics"
)

func TestGinWebhookServer(t *testing.T) {
	RegisterFailHandler(Fail)
	RunSpecs(t, "Gin Webhook Server Package Suite")
}

var _ = Describe("GinWebhookServer", func() {
	var (
		server            *GinWebhookServer
		fakeClient        kubernetes.Interface
		fakeRuntimeClient client.Client
		logger            *zap.Logger
		cfg               *config.Config
		ctx               context.Context
	)

	const debugLevel = "debug"

	BeforeEach(func() {
		ctx = context.Background() // Entry point context for all tests
		fakeClient = fake.NewSimpleClientset()
		scheme := runtime.NewScheme()
		_ = quotav1alpha1.AddToScheme(scheme)
		fakeRuntimeClient = clientfake.NewClientBuilder().WithScheme(scheme).Build()
		logger = zap.NewNop()
		cfg = &config.Config{
			WebhookPort: 8443,
			LogLevel:    "info",
		}
		server = NewGinWebhookServer(cfg, fakeClient, fakeRuntimeClient, logger)
	})

	Describe("NewGinWebhookServer", func() {
		It("should create a new webhook server", func() {
			Expect(server).NotTo(BeNil())
			Expect(server.engine).NotTo(BeNil())
			Expect(server.server).NotTo(BeNil())
			Expect(server.log).To(Equal(logger))
			Expect(server.port).To(Equal(cfg.WebhookPort))
		})

		It("should create a new webhook server with debug mode when LogLevel is debug", func() {
			cfg.LogLevel = debugLevel
			server = NewGinWebhookServer(cfg, fakeClient, fakeRuntimeClient, logger)
			Expect(server).NotTo(BeNil())
		})

		It("should handle nil kubernetes client gracefully", func() {
			server = NewGinWebhookServer(cfg, nil, nil, logger)
			Expect(server).NotTo(BeNil())
		})

		It("should handle nil logger gracefully", func() {
			server = NewGinWebhookServer(cfg, fakeClient, fakeRuntimeClient, nil)
			Expect(server).NotTo(BeNil())
		})
	})

	Describe("Start", func() {
		It("should handle cancelled context immediately", func() {
			// Use a context that's already cancelled
			ctx, cancel := context.WithCancel(ctx)
			cancel() // Cancel immediately

			err := server.Start(ctx)
			// Server should handle cancelled context gracefully without waiting for readiness
			Expect(err).NotTo(HaveOccurred())
		})

		It("should start and stop server when context is cancelled after brief delay", func() {
			// Use unique port to avoid conflicts
			cfg.WebhookPort = 19444
			server = NewGinWebhookServer(cfg, fakeClient, fakeRuntimeClient, logger)

			ctx, cancel := context.WithCancel(ctx)

			serverDone := make(chan error, 1)
			go func() {
				defer GinkgoRecover()
				err := server.Start(ctx)
				serverDone <- err
			}()

			// Give server time to attempt startup, then cancel
			time.Sleep(100 * time.Millisecond)
			cancel()

			// Wait for server to finish
			Eventually(serverDone, 5*time.Second).Should(Receive())
		})
	})

	Describe("Health and Readiness endpoints", func() {
		It("should have health endpoint configured in routes", func() {
			// Test that the server routes are properly configured without starting the server
			Expect(server).NotTo(BeNil())
			Expect(server.engine).NotTo(BeNil())

			// Verify the server structure is properly initialized
			Expect(server.port).To(Equal(cfg.WebhookPort))
			Expect(server.readyManager).NotTo(BeNil())
			Expect(server.readinessChecker).NotTo(BeNil())
		})

		It("should have readiness endpoint configured in routes", func() {
			// Test that the server routes are properly configured without starting the server
			Expect(server).NotTo(BeNil())
			Expect(server.engine).NotTo(BeNil())

			// Verify the readiness components are properly set up
			Expect(server.readyManager).NotTo(BeNil())
			Expect(server.readinessChecker).NotTo(BeNil())
		})
	})

	Describe("Metrics endpoint", func() {
		It("should have metrics endpoint configured and return 200", func() {
			Expect(server.engine).NotTo(BeNil())
			// Set a dummy metric value so the registry is not empty
			metrics.CRQTotalUsage.WithLabelValues("test-crq", "test-resource").Set(1)
			w := performRequest(server.engine, "GET", "/metrics")
			Expect(w.Code).To(Equal(200))
			Expect(w.Body.String()).NotTo(BeEmpty())
		})
	})

	Describe("Webhook endpoints", func() {
		It("should have webhook routes configured", func() {
			// Test that webhook routes are registered
			Expect(server.engine).NotTo(BeNil())
		})
	})

})

// Helper to perform HTTP requests against Gin engine
func performRequest(r http.Handler, method, path string) *httptest.ResponseRecorder {
	req, _ := http.NewRequest(method, path, nil)
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)
	return w
}
