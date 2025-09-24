package health

import (
	"fmt"
	"net/http"

	"github.com/gin-gonic/gin"
	"go.uber.org/zap"
)

// HealthChecker defines the interface for health checks
type HealthChecker interface {
	IsHealthy() bool
	GetHealthStatus() HealthStatus
}

// HealthStatus represents the health status of a component
type HealthStatus struct {
	Healthy bool           `json:"healthy"`
	Status  string         `json:"status"`
	Details map[string]any `json:"details,omitempty"`
}

// HealthManager manages health checks for the application
type HealthManager struct {
	checkers []HealthChecker
	log      *zap.Logger
}

// NewHealthManager creates a new health manager
func NewHealthManager(log *zap.Logger) *HealthManager {
	return &HealthManager{
		checkers: make([]HealthChecker, 0),
		log:      log,
	}
}

// AddChecker adds a health checker to the manager
func (h *HealthManager) AddChecker(checker HealthChecker) {
	h.checkers = append(h.checkers, checker)
}

// IsHealthy checks if all registered health checkers are healthy
func (h *HealthManager) IsHealthy() bool {
	for _, checker := range h.checkers {
		if !checker.IsHealthy() {
			return false
		}
	}
	return true
}

// GetHealthStatus returns the overall health status
func (h *HealthManager) GetHealthStatus() HealthStatus {
	status := HealthStatus{
		Healthy: true,
		Status:  "healthy",
		Details: make(map[string]any),
	}

	for i, checker := range h.checkers {
		checkerStatus := checker.GetHealthStatus()
		status.Details[fmt.Sprintf("checker_%d", i)] = checkerStatus

		if !checkerStatus.Healthy {
			status.Healthy = false
			status.Status = "unhealthy"
		}
	}

	return status
}

// HealthHandler returns a gin handler for health checks
func (h *HealthManager) HealthHandler() gin.HandlerFunc {
	return func(c *gin.Context) {
		status := h.GetHealthStatus()

		if status.Healthy {
			c.JSON(http.StatusOK, status)
		} else {
			c.JSON(http.StatusServiceUnavailable, status)
		}
	}
}

// SimpleHealthChecker is a basic health checker implementation
type SimpleHealthChecker struct {
	healthy bool
	name    string
}

// NewSimpleHealthChecker creates a new simple health checker
func NewSimpleHealthChecker(name string) *SimpleHealthChecker {
	return &SimpleHealthChecker{
		healthy: true,
		name:    name,
	}
}

// IsHealthy returns the health status
func (s *SimpleHealthChecker) IsHealthy() bool {
	return s.healthy
}

// GetHealthStatus returns the health status details
func (s *SimpleHealthChecker) GetHealthStatus() HealthStatus {
	status := "healthy"
	if !s.healthy {
		status = "unhealthy"
	}

	return HealthStatus{
		Healthy: s.healthy,
		Status:  status,
		Details: map[string]any{
			"name": s.name,
		},
	}
}

// SetHealth sets the health status
func (s *SimpleHealthChecker) SetHealth(healthy bool) {
	s.healthy = healthy
}
