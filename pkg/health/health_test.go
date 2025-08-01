/*
Copyright 2025.

Licensed under the Apache License, Version 2.0 (the "License");
you may not use this file except in compliance with the License.
You may obtain a copy of the License at

    http://www.apache.org/licenses/LICENSE-2.0

Unless required by applicable law or agreed to in writing, software
distributed under the License is distributed on an "AS IS" BASIS,
WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
See the License for the specific language governing permissions and
limitations under the License.
*/

package health

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/assert"
	"go.uber.org/zap"
)

func TestHealthManager(t *testing.T) {
	logger := zap.NewNop()
	manager := NewHealthManager(logger)

	t.Run("should be healthy when no checkers", func(t *testing.T) {
		assert.True(t, manager.IsHealthy())
		status := manager.GetHealthStatus()
		assert.True(t, status.Healthy)
		assert.Equal(t, "healthy", status.Status)
	})

	t.Run("should be healthy when all checkers are healthy", func(t *testing.T) {
		checker1 := NewSimpleHealthChecker("checker1")
		checker2 := NewSimpleHealthChecker("checker2")

		manager.AddChecker(checker1)
		manager.AddChecker(checker2)

		assert.True(t, manager.IsHealthy())
		status := manager.GetHealthStatus()
		assert.True(t, status.Healthy)
		assert.Equal(t, "healthy", status.Status)
		assert.Len(t, status.Details, 2)
	})

	t.Run("should be unhealthy when any checker is unhealthy", func(t *testing.T) {
		manager := NewHealthManager(logger)
		checker1 := NewSimpleHealthChecker("checker1")
		checker2 := NewSimpleHealthChecker("checker2")

		checker2.SetHealth(false)

		manager.AddChecker(checker1)
		manager.AddChecker(checker2)

		assert.False(t, manager.IsHealthy())
		status := manager.GetHealthStatus()
		assert.False(t, status.Healthy)
		assert.Equal(t, "unhealthy", status.Status)
	})
}

func TestHealthHandler(t *testing.T) {
	logger := zap.NewNop()
	manager := NewHealthManager(logger)

	// Set up Gin
	gin.SetMode(gin.TestMode)
	router := gin.New()
	router.GET("/healthz", manager.HealthHandler())

	t.Run("should return 200 when healthy", func(t *testing.T) {
		checker := NewSimpleHealthChecker("test")
		manager.AddChecker(checker)

		w := httptest.NewRecorder()
		req, _ := http.NewRequest("GET", "/healthz", nil)
		router.ServeHTTP(w, req)

		assert.Equal(t, http.StatusOK, w.Code)

		var response HealthStatus
		err := json.Unmarshal(w.Body.Bytes(), &response)
		assert.NoError(t, err)
		assert.True(t, response.Healthy)
	})

	t.Run("should return 503 when unhealthy", func(t *testing.T) {
		manager := NewHealthManager(logger)
		checker := NewSimpleHealthChecker("test")
		checker.SetHealth(false)
		manager.AddChecker(checker)

		router := gin.New()
		router.GET("/healthz", manager.HealthHandler())

		w := httptest.NewRecorder()
		req, _ := http.NewRequest("GET", "/healthz", nil)
		router.ServeHTTP(w, req)

		assert.Equal(t, http.StatusServiceUnavailable, w.Code)

		var response HealthStatus
		err := json.Unmarshal(w.Body.Bytes(), &response)
		assert.NoError(t, err)
		assert.False(t, response.Healthy)
	})
}

func TestSimpleHealthChecker(t *testing.T) {
	t.Run("should be healthy by default", func(t *testing.T) {
		checker := NewSimpleHealthChecker("test")
		assert.True(t, checker.IsHealthy())

		status := checker.GetHealthStatus()
		assert.True(t, status.Healthy)
		assert.Equal(t, "healthy", status.Status)
		assert.Equal(t, "test", status.Details["name"])
	})

	t.Run("should be unhealthy when set", func(t *testing.T) {
		checker := NewSimpleHealthChecker("test")
		checker.SetHealth(false)

		assert.False(t, checker.IsHealthy())

		status := checker.GetHealthStatus()
		assert.False(t, status.Healthy)
		assert.Equal(t, "unhealthy", status.Status)
	})
}
