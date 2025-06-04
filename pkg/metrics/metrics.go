package metrics

import (
	"crypto/tls"
	"os"
	"path/filepath"

	"github.com/powerhome/pac-quota-controller/pkg/config"
	"sigs.k8s.io/controller-runtime/pkg/certwatcher"
	"sigs.k8s.io/controller-runtime/pkg/log"
	"sigs.k8s.io/controller-runtime/pkg/metrics/filters"
	metricsserver "sigs.k8s.io/controller-runtime/pkg/metrics/server"
)

var setupLog = log.Log.WithName("setup.metrics")

// SetupMetricsServer configures the metrics server options
func SetupMetricsServer(config *config.Config, tlsOpts []func(*tls.Config)) (metricsserver.Options, *certwatcher.CertWatcher) {
	// Metrics endpoint is enabled in 'config/default/kustomization.yaml'. The Metrics options configure the server.
	// More info:
	// - https://pkg.go.dev/sigs.k8s.io/controller-runtime@v0.21.0/pkg/metrics/server
	// - https://book.kubebuilder.io/reference/metrics.html
	metricsServerOptions := metricsserver.Options{
		BindAddress:   config.MetricsAddr,
		SecureServing: config.SecureMetrics,
		TLSOpts:       tlsOpts,
	}

	if config.SecureMetrics {
		// FilterProvider is used to protect the metrics endpoint with authn/authz.
		// These configurations ensure that only authorized users and service accounts
		// can access the metrics endpoint. The RBAC are configured in 'config/rbac/kustomization.yaml'. More info:
		// https://pkg.go.dev/sigs.k8s.io/controller-runtime@v0.21.0/pkg/metrics/filters#WithAuthenticationAndAuthorization
		metricsServerOptions.FilterProvider = filters.WithAuthenticationAndAuthorization
	}

	// If the certificate is not specified, controller-runtime will automatically
	// generate self-signed certificates for the metrics server.
	var metricsCertWatcher *certwatcher.CertWatcher

	if len(config.MetricsCertPath) > 0 {
		setupLog.Info("Initializing metrics certificate watcher using provided certificates",
			"metrics-cert-path", config.MetricsCertPath,
			"metrics-cert-name", config.MetricsCertName,
			"metrics-cert-key", config.MetricsCertKey)

		var err error
		metricsCertWatcher, err = certwatcher.New(
			filepath.Join(config.MetricsCertPath, config.MetricsCertName),
			filepath.Join(config.MetricsCertPath, config.MetricsCertKey),
		)
		if err != nil {
			setupLog.Error(err, "failed to initialize metrics certificate watcher")
			os.Exit(1)
		}

		metricsServerOptions.TLSOpts = append(metricsServerOptions.TLSOpts, func(config *tls.Config) {
			config.GetCertificate = metricsCertWatcher.GetCertificate
		})
	}

	return metricsServerOptions, metricsCertWatcher
}
