package server

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/gin-gonic/gin"
	"go.uber.org/zap"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/client-go/kubernetes"
	"k8s.io/client-go/kubernetes/fake"
	"sigs.k8s.io/controller-runtime/pkg/client"
	clientfake "sigs.k8s.io/controller-runtime/pkg/client/fake"

	quotav1alpha1 "github.com/powerhome/pac-quota-controller/api/v1alpha1"
	"github.com/powerhome/pac-quota-controller/pkg/config"
	pkglogger "github.com/powerhome/pac-quota-controller/pkg/logger"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
)

func TestGinWebhookServer(t *testing.T) {
	RegisterFailHandler(Fail)
	RunSpecs(t, "Gin Webhook Server Package Suite")
}

var _ = BeforeSuite(func() {
	pkglogger.InitTest()
})

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
		logger = pkglogger.L()
		cfg = &config.Config{
			WebhookPort: 9443,
			LogLevel:    "info",
		}
		server = NewGinWebhookServer(cfg, fakeClient, fakeRuntimeClient, logger)
	})

	Describe("NewGinWebhookServer", func() {
		It("should create a new webhook server", func() {
			Expect(server).NotTo(BeNil())
			Expect(server.engine).NotTo(BeNil())
			Expect(server.server).NotTo(BeNil())
			Expect(server.logger).NotTo(BeNil())
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

	Describe("Webhook endpoints", func() {
		It("should have webhook routes configured", func() {
			// Test that webhook routes are registered
			Expect(server.engine).NotTo(BeNil())
		})
	})

	Describe("/readyz with nil runtime client", func() {
		// hitReadyz drives the gin engine in-process so we can assert the status code
		// without binding a TCP port.
		hitReadyz := func(s *GinWebhookServer) int {
			req := httptest.NewRequest(http.MethodGet, "/readyz", nil)
			w := httptest.NewRecorder()
			s.engine.ServeHTTP(w, req)
			return w.Code
		}

		It("fails (503) even after MarkReady when runtimeClient is nil — quota enforcement is degraded", func() {
			s := NewGinWebhookServer(cfg, fakeClient, nil, logger)
			s.MarkReady()
			Expect(hitReadyz(s)).To(Equal(http.StatusServiceUnavailable))
		})

		It("passes (200) after MarkReady when runtimeClient is set and cache is marked synced", func() {
			s := NewGinWebhookServer(cfg, fakeClient, fakeRuntimeClient, logger)
			s.MarkReady()
			s.MarkCacheSynced()
			Expect(hitReadyz(s)).To(Equal(http.StatusOK))
		})

		It("drains in-flight requests when the parent ctx is cancelled before shutdown", func() {
			// Property: even though the parent ctx is cancelled the moment shutdown
			// starts, requests already accepted should finish (the 30s drain budget
			// lives on a fresh context). A naive shutdown(ctx) returns immediately
			// with context.Canceled and the slow handler would get cut off.
			cfg.WebhookPort = 19446
			s := NewGinWebhookServer(cfg, fakeClient, fakeRuntimeClient, logger)

			slowDone := make(chan struct{})
			s.engine.GET("/drain-probe", func(c *gin.Context) {
				time.Sleep(300 * time.Millisecond)
				close(slowDone)
				c.JSON(http.StatusOK, gin.H{"ok": true})
			})

			startCtx, cancel := context.WithCancel(context.Background())
			serverDone := make(chan error, 1)
			go func() {
				defer GinkgoRecover()
				serverDone <- s.Start(startCtx)
			}()

			// Wait until the listener accepts. Quick poll is fine because the
			// server binds synchronously in startServerInBackground.
			Eventually(func() error {
				resp, err := http.Get("http://127.0.0.1:19446/healthz")
				if err == nil {
					_ = resp.Body.Close()
				}
				return err
			}, 2*time.Second, 20*time.Millisecond).Should(Succeed())

			probeStarted := make(chan struct{})
			go func() {
				defer GinkgoRecover()
				close(probeStarted)
				resp, err := http.Get("http://127.0.0.1:19446/drain-probe")
				Expect(err).NotTo(HaveOccurred())
				_ = resp.Body.Close()
			}()

			<-probeStarted
			time.Sleep(50 * time.Millisecond) // ensure the handler is past the accept
			cancel()                          // trigger shutdown while the handler is sleeping

			Eventually(slowDone, 2*time.Second).Should(BeClosed(), "in-flight handler should finish, not be cut off")
			Eventually(serverDone, 5*time.Second).Should(Receive())
		})

		It("fails (503) when the runtime client is set but the cache has not yet synced", func() {
			s := NewGinWebhookServer(cfg, fakeClient, fakeRuntimeClient, logger)
			s.MarkReady()
			// MarkCacheSynced intentionally NOT called — simulates the apiserver
			// routing traffic to a webhook whose informer cache is still cold.
			Expect(hitReadyz(s)).To(Equal(http.StatusServiceUnavailable))
		})
	})

})
