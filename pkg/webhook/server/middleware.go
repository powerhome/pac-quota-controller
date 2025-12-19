package server

import (
	"context"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/google/uuid"
	"github.com/powerhome/pac-quota-controller/pkg/kubernetes/quota"
	"go.uber.org/zap"
)

// RequestLogger returns a gin.HandlerFunc that logs requests using Zap
func RequestLogger(logger *zap.Logger) gin.HandlerFunc {
	return func(c *gin.Context) {
		start := time.Now()
		path := c.Request.URL.Path
		query := c.Request.URL.RawQuery

		// Generate or use existing correlation ID
		correlationID := c.GetHeader("X-Correlation-ID")
		if correlationID == "" {
			correlationID = uuid.New().String()
		}

		// Inject into context for downstream handlers
		c.Set(string(quota.CorrelationIDKey), correlationID)

		// Also inject into the internal Request context for compatibility with standard context.Context patterns
		ctx := context.WithValue(c.Request.Context(), quota.CorrelationIDKey, correlationID)
		c.Request = c.Request.WithContext(ctx)

		// Also set in response header for traceability
		c.Header("X-Correlation-ID", correlationID)

		c.Next()

		latency := time.Since(start)
		statusCode := c.Writer.Status()
		clientIP := c.ClientIP()
		method := c.Request.Method

		if len(c.Errors) > 0 {
			for _, e := range c.Errors.Errors() {
				logger.Error("Request failed",
					zap.String("correlation_id", correlationID),
					zap.String("error", e),
					zap.Int("status", statusCode),
					zap.String("method", method),
					zap.String("path", path),
					zap.String("query", query),
					zap.String("ip", clientIP),
					zap.Duration("latency", latency),
				)
			}
		} else {
			logger.Info("Request completed",
				zap.String("correlation_id", correlationID),
				zap.Int("status", statusCode),
				zap.String("method", method),
				zap.String("path", path),
				zap.String("query", query),
				zap.String("ip", clientIP),
				zap.Duration("latency", latency),
			)
		}
	}
}
