package events

import (
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestLoadEventCleanupConfig(t *testing.T) {
	tests := []struct {
		name              string
		configFile        string
		envTTL            string
		envMaxEvents      int
		envInterval       string
		expectedTTL       time.Duration
		expectedMaxEvents int
		expectedInterval  time.Duration
		expectError       bool
	}{
		{
			name:              "defaults only",
			expectedTTL:       24 * time.Hour,
			expectedMaxEvents: 100,
			expectedInterval:  1 * time.Hour,
		},
		{
			name:              "environment variables override",
			envTTL:            "12h",
			envMaxEvents:      50,
			envInterval:       "30m",
			expectedTTL:       12 * time.Hour,
			expectedMaxEvents: 50,
			expectedInterval:  30 * time.Minute,
		},
		{
			name:        "invalid TTL",
			envTTL:      "invalid",
			expectError: true,
		},
		{
			name:        "invalid interval",
			envInterval: "invalid",
			expectError: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			// Create temporary config file if specified
			var configPath string
			if tt.configFile != "" {
				tmpDir := t.TempDir()
				configPath = filepath.Join(tmpDir, "event-config.yaml")
				err := os.WriteFile(configPath, []byte(tt.configFile), 0644)
				require.NoError(t, err)
			}

			config, err := LoadEventCleanupConfig(configPath, tt.envTTL, tt.envMaxEvents, tt.envInterval)

			if tt.expectError {
				assert.Error(t, err)
				return
			}

			require.NoError(t, err)
			assert.Equal(t, tt.expectedTTL, config.MaxAge)
			assert.Equal(t, tt.expectedMaxEvents, config.MaxEventsPerCRQ)
			assert.Equal(t, tt.expectedInterval, config.CleanupInterval)
		})
	}
}

func TestLoadEventCleanupConfigWithFile(t *testing.T) {
	configContent := `
cleanup:
  ttl: "48h"
  maxEventsPerCRQ: 200
  interval: "2h"
recording:
  controllerComponent: "test-controller"
  webhookComponent: "test-webhook"
  backoff:
    baseInterval: "1m"
    maxInterval: "10m"
`

	tmpDir := t.TempDir()
	configPath := filepath.Join(tmpDir, "event-config.yaml")
	err := os.WriteFile(configPath, []byte(configContent), 0644)
	require.NoError(t, err)

	config, err := LoadEventCleanupConfig(configPath, "", 0, "")
	require.NoError(t, err)

	assert.Equal(t, 48*time.Hour, config.MaxAge)
	assert.Equal(t, 200, config.MaxEventsPerCRQ)
	assert.Equal(t, 2*time.Hour, config.CleanupInterval)
}

func TestLoadEventCleanupConfigFileOverriddenByEnv(t *testing.T) {
	configContent := `
cleanup:
  ttl: "48h"
  maxEventsPerCRQ: 200
  interval: "2h"
`

	tmpDir := t.TempDir()
	configPath := filepath.Join(tmpDir, "event-config.yaml")
	err := os.WriteFile(configPath, []byte(configContent), 0644)
	require.NoError(t, err)

	// Environment variables should override file values
	config, err := LoadEventCleanupConfig(configPath, "6h", 75, "15m")
	require.NoError(t, err)

	assert.Equal(t, 6*time.Hour, config.MaxAge)
	assert.Equal(t, 75, config.MaxEventsPerCRQ)
	assert.Equal(t, 15*time.Minute, config.CleanupInterval)
}
