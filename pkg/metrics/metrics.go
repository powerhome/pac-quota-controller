package metrics

import (
	"crypto/tls"
	"path/filepath"

	"github.com/powerhome/pac-quota-controller/pkg/config"
	"sigs.k8s.io/controller-runtime/pkg/certwatcher"
	logf "sigs.k8s.io/controller-runtime/pkg/log"
	"sigs.k8s.io/controller-runtime/pkg/metrics/filters"
	metricsserver "sigs.k8s.io/controller-runtime/pkg/metrics/server"
)

var setupLog = logf.Log.WithName("setup.metrics")

// SetupMetricsServer configures the metrics server options
func SetupMetricsServer(
	cfg *config.Config,
	tlsOpts []func(*tls.Config),
) (metricsserver.Options, *certwatcher.CertWatcher, error) {
	// Metrics endpoint is enabled in 'config/default/kustomization.yaml'. The Metrics options configure the server.
	// More info:
	// - https://pkg.go.dev/sigs.k8s.io/controller-runtime@v0.21.0/pkg/metrics/server
	// - https://book.kubebuilder.io/reference/metrics.html
	metricsServerOptions := metricsserver.Options{
		BindAddress:   cfg.MetricsAddr,
		SecureServing: cfg.SecureMetrics,
		TLSOpts:       tlsOpts,
	}

	if cfg.SecureMetrics {
		// FilterProvider is used to protect the metrics endpoint with authn/authz.
		// These configurations ensure that only authorized users and service accounts
		// can access the metrics endpoint. The RBAC are configured in 'config/rbac/kustomization.yaml'. More info:
		// https://pkg.go.dev/sigs.k8s.io/controller-runtime@v0.21.0/pkg/metrics/filters#WithAuthenticationAndAuthorization
		metricsServerOptions.FilterProvider = filters.WithAuthenticationAndAuthorization
	}

	// If the certificate is not specified, controller-runtime will automatically
	// generate self-signed certificates for the metrics server.
	var metricsCertWatcher *certwatcher.CertWatcher

	if len(cfg.MetricsCertPath) > 0 {
		setupLog.Info("Initializing metrics certificate watcher using provided certificates",
			"metrics-cert-path", cfg.MetricsCertPath,
			"metrics-cert-name", cfg.MetricsCertName,
			"metrics-cert-key", cfg.MetricsCertKey)

		var err error
		metricsCertWatcher, err = certwatcher.New(
			filepath.Join(cfg.MetricsCertPath, cfg.MetricsCertName),
			filepath.Join(cfg.MetricsCertPath, cfg.MetricsCertKey),
		)
		if err != nil {
			setupLog.Error(err, "failed to initialize metrics certificate watcher")
			return metricsserver.Options{}, nil, err
		}

		metricsServerOptions.TLSOpts = append(metricsServerOptions.TLSOpts, func(config *tls.Config) {
			config.GetCertificate = metricsCertWatcher.GetCertificate
		})
	}

	return metricsServerOptions, metricsCertWatcher, nil
}
