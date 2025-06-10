package webhook

import (
	"crypto/tls"
	"os"
	"path/filepath"

	"github.com/powerhome/pac-quota-controller/pkg/config"
	"sigs.k8s.io/controller-runtime/pkg/certwatcher"
	logf "sigs.k8s.io/controller-runtime/pkg/log"
	"sigs.k8s.io/controller-runtime/pkg/webhook"
)

var setupLog = logf.Log.WithName("setup.webhook")

// SetupWebhookServer configures the webhook server
func SetupWebhookServer(cfg *config.Config, tlsOpts []func(*tls.Config)) (webhook.Server, *certwatcher.CertWatcher) {
	// Start with base TLS options
	webhookTLSOpts := tlsOpts
	var webhookCertWatcher *certwatcher.CertWatcher

	if len(cfg.WebhookCertPath) > 0 {
		setupLog.Info("Initializing webhook certificate watcher using provided certificates",
			"webhook-cert-path", cfg.WebhookCertPath,
			"webhook-cert-name", cfg.WebhookCertName,
			"webhook-cert-key", cfg.WebhookCertKey)

		var err error
		webhookCertWatcher, err = certwatcher.New(
			filepath.Join(cfg.WebhookCertPath, cfg.WebhookCertName),
			filepath.Join(cfg.WebhookCertPath, cfg.WebhookCertKey),
		)
		if err != nil {
			setupLog.Error(err, "Failed to initialize webhook certificate watcher")
			os.Exit(1)
		}

		webhookTLSOpts = append(webhookTLSOpts, func(config *tls.Config) {
			config.GetCertificate = webhookCertWatcher.GetCertificate
		})
	}

	// Ensure the webhook server listens on the configured port and all interfaces (default 9443)
	webhookServer := webhook.NewServer(webhook.Options{
		Port:    cfg.WebhookPort,
		TLSOpts: webhookTLSOpts,
	})

	return webhookServer, webhookCertWatcher
}
