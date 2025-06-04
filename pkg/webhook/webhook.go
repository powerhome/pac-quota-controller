package webhook

import (
	"crypto/tls"
	"os"
	"path/filepath"

	"github.com/powerhome/pac-quota-controller/pkg/config"
	"sigs.k8s.io/controller-runtime/pkg/certwatcher"
	"sigs.k8s.io/controller-runtime/pkg/log"
	"sigs.k8s.io/controller-runtime/pkg/webhook"
)

var setupLog = log.Log.WithName("setup.webhook")

// SetupWebhookServer configures the webhook server
func SetupWebhookServer(config *config.Config, tlsOpts []func(*tls.Config)) (webhook.Server, *certwatcher.CertWatcher) {
	// Start with base TLS options
	webhookTLSOpts := tlsOpts
	var webhookCertWatcher *certwatcher.CertWatcher

	if len(config.WebhookCertPath) > 0 {
		setupLog.Info("Initializing webhook certificate watcher using provided certificates",
			"webhook-cert-path", config.WebhookCertPath,
			"webhook-cert-name", config.WebhookCertName,
			"webhook-cert-key", config.WebhookCertKey)

		var err error
		webhookCertWatcher, err = certwatcher.New(
			filepath.Join(config.WebhookCertPath, config.WebhookCertName),
			filepath.Join(config.WebhookCertPath, config.WebhookCertKey),
		)
		if err != nil {
			setupLog.Error(err, "Failed to initialize webhook certificate watcher")
			os.Exit(1)
		}

		webhookTLSOpts = append(webhookTLSOpts, func(config *tls.Config) {
			config.GetCertificate = webhookCertWatcher.GetCertificate
		})
	}

	webhookServer := webhook.NewServer(webhook.Options{
		TLSOpts: webhookTLSOpts,
	})

	return webhookServer, webhookCertWatcher
}
