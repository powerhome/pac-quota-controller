package metrics

import (
	"crypto/tls"
	"testing"

	"github.com/powerhome/pac-quota-controller/pkg/config"
	"github.com/stretchr/testify/assert"
)

func TestSetupMetricsServer_Insecure(t *testing.T) {
	cfg := &config.Config{
		MetricsAddr:     ":8080",
		SecureMetrics:   false,
		MetricsCertPath: "",
	}
	options, watcher, err := SetupMetricsServer(cfg, nil)
	assert.NoError(t, err)
	assert.Equal(t, ":8080", options.BindAddress)
	assert.False(t, options.SecureServing)
	assert.Nil(t, watcher)
}

func TestSetupMetricsServer_Secure_NoCerts(t *testing.T) {
	cfg := &config.Config{
		MetricsAddr:     ":8443",
		SecureMetrics:   true,
		MetricsCertPath: "",
	}
	options, watcher, err := SetupMetricsServer(cfg, []func(*tls.Config){})
	assert.NoError(t, err)
	assert.Equal(t, ":8443", options.BindAddress)
	assert.True(t, options.SecureServing)
	assert.Nil(t, watcher)
	assert.NotNil(t, options.FilterProvider)
}

func TestSetupMetricsServer_WithCerts_InvalidPath(t *testing.T) {
	cfg := &config.Config{
		MetricsAddr:     ":8443",
		SecureMetrics:   true,
		MetricsCertPath: "/invalid/path",
		MetricsCertName: "cert.pem",
		MetricsCertKey:  "key.pem",
	}
	_, _, err := SetupMetricsServer(cfg, nil)
	assert.Error(t, err)
}
