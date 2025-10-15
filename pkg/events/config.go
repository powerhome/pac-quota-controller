package events

import (
	"fmt"
	"os"
	"time"

	"gopkg.in/yaml.v3"
)

// EventConfig represents the full event configuration from file
type EventConfig struct {
	Cleanup   CleanupConfigFile   `yaml:"cleanup"`
	Recording RecordingConfigFile `yaml:"recording"`
}

// CleanupConfigFile represents the cleanup configuration from file
type CleanupConfigFile struct {
	TTL             string `yaml:"ttl"`
	MaxEventsPerCRQ int    `yaml:"maxEventsPerCRQ"`
	Interval        string `yaml:"interval"`
}

// RecordingConfigFile represents the recording configuration from file
type RecordingConfigFile struct {
	ControllerComponent string      `yaml:"controllerComponent"`
	WebhookComponent    string      `yaml:"webhookComponent"`
	Backoff             BackoffFile `yaml:"backoff"`
}

// BackoffFile represents the backoff configuration from file
type BackoffFile struct {
	BaseInterval string `yaml:"baseInterval"`
	MaxInterval  string `yaml:"maxInterval"`
}

// LoadEventCleanupConfig loads event cleanup configuration from multiple sources:
// 1. Environment variables (highest priority)
// 2. Configuration file (if exists)
// 3. Default values (lowest priority)
func LoadEventCleanupConfig(
	configPath string,
	envTTL string,
	envMaxEvents int,
	envInterval string) (CleanupConfig, error) {
	config := DefaultCleanupConfig()

	// Try to load from config file if it exists
	if configPath != "" {
		if _, err := os.Stat(configPath); err == nil {
			fileConfig, err := loadEventConfigFromFile(configPath)
			if err != nil {
				return config, fmt.Errorf("failed to load event config from file %s: %w", configPath, err)
			}

			// Update config with file values
			if fileConfig.Cleanup.TTL != "" {
				ttl, err := time.ParseDuration(fileConfig.Cleanup.TTL)
				if err != nil {
					return config, fmt.Errorf("invalid TTL duration in config file: %w", err)
				}
				config.MaxAge = ttl
			}

			if fileConfig.Cleanup.MaxEventsPerCRQ > 0 {
				config.MaxEventsPerCRQ = fileConfig.Cleanup.MaxEventsPerCRQ
			}

			if fileConfig.Cleanup.Interval != "" {
				interval, err := time.ParseDuration(fileConfig.Cleanup.Interval)
				if err != nil {
					return config, fmt.Errorf("invalid cleanup interval duration in config file: %w", err)
				}
				config.CleanupInterval = interval
			}
		}
	}

	// Override with environment variables (highest priority)
	if envTTL != "" {
		ttl, err := time.ParseDuration(envTTL)
		if err != nil {
			return config, fmt.Errorf("invalid TTL duration from environment: %w", err)
		}
		config.MaxAge = ttl
	}

	if envMaxEvents > 0 {
		config.MaxEventsPerCRQ = envMaxEvents
	}

	if envInterval != "" {
		interval, err := time.ParseDuration(envInterval)
		if err != nil {
			return config, fmt.Errorf("invalid cleanup interval duration from environment: %w", err)
		}
		config.CleanupInterval = interval
	}

	return config, nil
}

// loadEventConfigFromFile loads configuration from a YAML file
func loadEventConfigFromFile(path string) (*EventConfig, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, fmt.Errorf("failed to read config file: %w", err)
	}

	var config EventConfig
	if err := yaml.Unmarshal(data, &config); err != nil {
		return nil, fmt.Errorf("failed to parse YAML config: %w", err)
	}

	return &config, nil
}
