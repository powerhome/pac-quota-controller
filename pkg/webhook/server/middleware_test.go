package server

import (
	"net/http"
	"net/http/httptest"

	"github.com/gin-gonic/gin"
	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	"github.com/powerhome/pac-quota-controller/pkg/kubernetes/quota"
	"go.uber.org/zap"
)

var _ = Describe("Middleware", func() {
	var (
		engine *gin.Engine
		logger *zap.Logger
	)

	BeforeEach(func() {
		gin.SetMode(gin.TestMode)
		logger = zap.NewNop()
		engine = gin.New()
		engine.Use(RequestLogger(logger))
	})

	Describe("RequestLogger", func() {
		It("should inject correlation ID into both Gin context and Request context", func() {
			var ginContextID string
			var requestContextID string

			engine.POST("/test", func(c *gin.Context) {
				// Check Gin context
				if val, exists := c.Get(string(quota.CorrelationIDKey)); exists {
					ginContextID = val.(string)
				}

				// Check Request context (standard context.Context)
				requestContextID = quota.GetCorrelationID(c.Request.Context())

				c.Status(http.StatusOK)
			})

			w := httptest.NewRecorder()
			req, _ := http.NewRequest("POST", "/test", nil)
			req.Header.Set("X-Correlation-ID", "test-correlation-id")
			engine.ServeHTTP(w, req)

			Expect(w.Code).To(Equal(http.StatusOK))
			Expect(ginContextID).To(Equal("test-correlation-id"))
			Expect(requestContextID).To(Equal("test-correlation-id"))
			Expect(w.Header().Get("X-Correlation-ID")).To(Equal("test-correlation-id"))
		})

		It("should generate a new correlation ID if X-Correlation-ID header is missing", func() {
			var requestContextID string

			engine.POST("/test-gen", func(c *gin.Context) {
				requestContextID = quota.GetCorrelationID(c.Request.Context())
				c.Status(http.StatusOK)
			})

			w := httptest.NewRecorder()
			req, _ := http.NewRequest("POST", "/test-gen", nil)
			engine.ServeHTTP(w, req)

			Expect(w.Code).To(Equal(http.StatusOK))
			Expect(requestContextID).NotTo(BeEmpty())
			Expect(w.Header().Get("X-Correlation-ID")).To(Equal(requestContextID))
		})
	})
})
