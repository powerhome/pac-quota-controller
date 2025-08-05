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

package webhook

import (
	"path/filepath"

	"github.com/powerhome/pac-quota-controller/pkg/config"
	"github.com/powerhome/pac-quota-controller/pkg/webhook/certwatcher"
	"github.com/powerhome/pac-quota-controller/pkg/webhook/server"
	"go.uber.org/zap"
	"k8s.io/client-go/kubernetes"
	"sigs.k8s.io/controller-runtime/pkg/client"
)

// SetupGinWebhookServer configures the Gin-based webhook server with certificate watching
func SetupGinWebhookServer(
	cfg *config.Config,
	k8sClient kubernetes.Interface,
	runtimeClient client.Client,
	log *zap.Logger,
) (*server.GinWebhookServer, *certwatcher.CertWatcher) {
	// Create the Gin webhook server
	webhookServer := server.NewGinWebhookServer(cfg, k8sClient, runtimeClient, log)

	// Setup certificate watcher if certificates are provided
	if len(cfg.WebhookCertPath) > 0 {
		log.Info("Initializing webhook certificate watcher using provided certificates",
			zap.String("webhook-cert-path", cfg.WebhookCertPath),
			zap.String("webhook-cert-name", cfg.WebhookCertName),
			zap.String("webhook-cert-key", cfg.WebhookCertKey))

		var err error
		webhookCertWatcher, err := certwatcher.NewCertWatcher(
			filepath.Join(cfg.WebhookCertPath, cfg.WebhookCertName),
			filepath.Join(cfg.WebhookCertPath, cfg.WebhookCertKey),
			log,
		)
		if err != nil {
			log.Error("Failed to initialize webhook certificate watcher", zap.Error(err))
			log.Info("Continuing without certificate watcher - server will run without TLS")
			return webhookServer, nil
		}

		// Configure the server with certificate watcher
		if err := webhookServer.SetupCertificateWatcher(cfg); err != nil {
			log.Error("Failed to setup certificate watcher", zap.Error(err))
		}

		return webhookServer, webhookCertWatcher
	}

	// No certificates provided, return server without certificate watcher
	return webhookServer, nil
}
