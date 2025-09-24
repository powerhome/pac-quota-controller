package ready

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/assert"
	"go.uber.org/zap"
)

func TestReadinessManager(t *testing.T) {
	logger := zap.NewNop()
	manager := NewReadinessManager(logger)

	t.Run("should be ready when no checkers", func(t *testing.T) {
		assert.True(t, manager.IsReady())
		status := manager.GetReadinessStatus()
		assert.True(t, status.Ready)
		assert.Equal(t, "ready", status.Status)
	})

	t.Run("should be ready when all checkers are ready", func(t *testing.T) {
		checker1 := NewSimpleReadinessChecker("checker1")
		checker2 := NewSimpleReadinessChecker("checker2")

		checker1.SetReady(true)
		checker2.SetReady(true)

		manager.AddChecker(checker1)
		manager.AddChecker(checker2)

		assert.True(t, manager.IsReady())
		status := manager.GetReadinessStatus()
		assert.True(t, status.Ready)
		assert.Equal(t, "ready", status.Status)
		assert.Len(t, status.Details, 2)
	})

	t.Run("should not be ready when any checker is not ready", func(t *testing.T) {
		manager := NewReadinessManager(logger)
		checker1 := NewSimpleReadinessChecker("checker1")
		checker2 := NewSimpleReadinessChecker("checker2")

		checker1.SetReady(true)
		checker2.SetReady(false) // Not ready

		manager.AddChecker(checker1)
		manager.AddChecker(checker2)

		assert.False(t, manager.IsReady())
		status := manager.GetReadinessStatus()
		assert.False(t, status.Ready)
		assert.Equal(t, "not ready", status.Status)
	})
}

func TestReadyHandler(t *testing.T) {
	logger := zap.NewNop()
	manager := NewReadinessManager(logger)

	// Set up Gin
	gin.SetMode(gin.TestMode)
	router := gin.New()
	router.GET("/readyz", manager.ReadyHandler())

	t.Run("should return 200 when ready", func(t *testing.T) {
		checker := NewSimpleReadinessChecker("test")
		checker.SetReady(true)
		manager.AddChecker(checker)

		w := httptest.NewRecorder()
		req, _ := http.NewRequest("GET", "/readyz", nil)
		router.ServeHTTP(w, req)

		assert.Equal(t, http.StatusOK, w.Code)

		var response ReadinessStatus
		err := json.Unmarshal(w.Body.Bytes(), &response)
		assert.NoError(t, err)
		assert.True(t, response.Ready)
	})

	t.Run("should return 503 when not ready", func(t *testing.T) {
		manager := NewReadinessManager(logger)
		checker := NewSimpleReadinessChecker("test")
		checker.SetReady(false)
		manager.AddChecker(checker)

		router := gin.New()
		router.GET("/readyz", manager.ReadyHandler())

		w := httptest.NewRecorder()
		req, _ := http.NewRequest("GET", "/readyz", nil)
		router.ServeHTTP(w, req)

		assert.Equal(t, http.StatusServiceUnavailable, w.Code)

		var response ReadinessStatus
		err := json.Unmarshal(w.Body.Bytes(), &response)
		assert.NoError(t, err)
		assert.False(t, response.Ready)
	})
}

func TestSimpleReadinessChecker(t *testing.T) {
	t.Run("should not be ready by default", func(t *testing.T) {
		checker := NewSimpleReadinessChecker("test")
		assert.False(t, checker.IsReady())

		status := checker.GetReadinessStatus()
		assert.False(t, status.Ready)
		assert.Equal(t, "not ready", status.Status)
		assert.Equal(t, "test", status.Details["name"])
	})

	t.Run("should be ready when set", func(t *testing.T) {
		checker := NewSimpleReadinessChecker("test")
		checker.SetReady(true)

		assert.True(t, checker.IsReady())

		status := checker.GetReadinessStatus()
		assert.True(t, status.Ready)
		assert.Equal(t, "ready", status.Status)
	})
}
