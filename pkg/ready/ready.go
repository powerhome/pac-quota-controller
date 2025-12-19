package ready

import (
	"fmt"
	"net/http"

	"github.com/gin-gonic/gin"
	"go.uber.org/zap"
)

// ReadinessChecker defines the interface for readiness checks
type ReadinessChecker interface {
	IsReady() bool
	GetReadinessStatus() ReadinessStatus
}

// ReadinessStatus represents the readiness status of a component
type ReadinessStatus struct {
	Ready   bool           `json:"ready"`
	Status  string         `json:"status"`
	Details map[string]any `json:"details,omitempty"`
}

// ReadinessManager manages readiness checks for the application
type ReadinessManager struct {
	checkers []ReadinessChecker
	logger   *zap.Logger
}

// NewReadinessManager creates a new readiness manager
func NewReadinessManager(logger *zap.Logger) *ReadinessManager {
	if logger == nil {
		logger = zap.NewNop()
	}
	return &ReadinessManager{
		checkers: make([]ReadinessChecker, 0),
		logger:   logger.Named("readiness-manager"),
	}
}

// AddChecker adds a readiness checker to the manager
func (r *ReadinessManager) AddChecker(checker ReadinessChecker) {
	r.checkers = append(r.checkers, checker)
}

// IsReady checks if all registered readiness checkers are ready
func (r *ReadinessManager) IsReady() bool {
	for _, checker := range r.checkers {
		if !checker.IsReady() {
			return false
		}
	}
	return true
}

// GetReadinessStatus returns the overall readiness status
func (r *ReadinessManager) GetReadinessStatus() ReadinessStatus {
	status := ReadinessStatus{
		Ready:   true,
		Status:  "ready",
		Details: make(map[string]any),
	}

	for i, checker := range r.checkers {
		checkerStatus := checker.GetReadinessStatus()
		status.Details[fmt.Sprintf("checker_%d", i)] = checkerStatus

		if !checkerStatus.Ready {
			status.Ready = false
			status.Status = "not ready"
		}
	}

	return status
}

// ReadyHandler returns a gin handler for readiness checks
func (r *ReadinessManager) ReadyHandler() gin.HandlerFunc {
	return func(c *gin.Context) {
		status := r.GetReadinessStatus()

		if status.Ready {
			c.JSON(http.StatusOK, status)
		} else {
			c.JSON(http.StatusServiceUnavailable, status)
		}
	}
}

// SimpleReadinessChecker is a basic readiness checker implementation
type SimpleReadinessChecker struct {
	ready bool
	name  string
}

// NewSimpleReadinessChecker creates a new simple readiness checker
func NewSimpleReadinessChecker(name string) *SimpleReadinessChecker {
	return &SimpleReadinessChecker{
		ready: false, // Start as not ready
		name:  name,
	}
}

// IsReady returns the readiness status
func (s *SimpleReadinessChecker) IsReady() bool {
	return s.ready
}

// GetReadinessStatus returns the readiness status details
func (s *SimpleReadinessChecker) GetReadinessStatus() ReadinessStatus {
	status := "ready"
	if !s.ready {
		status = "not ready"
	}

	return ReadinessStatus{
		Ready:  s.ready,
		Status: status,
		Details: map[string]any{
			"name": s.name,
		},
	}
}

// SetReady sets the readiness status
func (s *SimpleReadinessChecker) SetReady(ready bool) {
	s.ready = ready
}
