package config

import (
	"github.com/spf13/viper"
)

// Config represents the application configuration
type Config struct {
	Port     string `mapstructure:"port"`
	LogLevel string `mapstructure:"log_level"`
	Env      string `mapstructure:"env"`
	TLSCert  string `mapstructure:"tls_cert"`
	TLSKey   string `mapstructure:"tls_key"`
}

// Load loads the configuration from environment variables and config file
func Load() (*Config, error) {
	v := viper.New()

	// Set default values
	v.SetDefault("port", "8080")
	v.SetDefault("log_level", "info")
	v.SetDefault("env", "dev")
	v.SetDefault("tls_cert", "/etc/webhook/certs/tls.crt")
	v.SetDefault("tls_key", "/etc/webhook/certs/tls.key")

	// Bind environment variables
	v.SetEnvPrefix("PAC_QUOTA_CONTROLLER")
	// TODO: Automatically validate env vars
	if err := v.BindEnv("port"); err != nil {
		return nil, err
	}
	if err := v.BindEnv("log_level"); err != nil {
		return nil, err
	}
	if err := v.BindEnv("env"); err != nil {
		return nil, err
	}
	if err := v.BindEnv("tls_cert"); err != nil {
		return nil, err
	}
	if err := v.BindEnv("tls_key"); err != nil {
		return nil, err
	}

	// Read config file if specified
	if configFile := viper.GetString("config"); configFile != "" {
		v.SetConfigFile(configFile)
		if err := v.ReadInConfig(); err != nil {
			return nil, err
		}
	}

	var cfg Config
	if err := v.Unmarshal(&cfg); err != nil {
		return nil, err
	}

	return &cfg, nil
}
