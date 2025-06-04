package tls

import (
	"crypto/tls"

	"github.com/powerhome/pac-quota-controller/pkg/config"
	"sigs.k8s.io/controller-runtime/pkg/log"
)

var setupLog = log.Log.WithName("setup.tls")

// ConfigureTLS returns TLS options based on configuration
func ConfigureTLS(config *config.Config) []func(*tls.Config) {
	var tlsOpts []func(*tls.Config)

	// If the enable-http2 flag is false (the default), http/2 should be disabled
	// due to its vulnerabilities. More specifically, disabling http/2 will
	// prevent from being vulnerable to the HTTP/2 Stream Cancellation and
	// Rapid Reset CVEs. For more information see:
	// - https://github.com/advisories/GHSA-qppj-fm5r-hxr3
	// - https://github.com/advisories/GHSA-4374-p667-p6c8
	disableHTTP2 := func(c *tls.Config) {
		setupLog.Info("disabling http/2")
		c.NextProtos = []string{"http/1.1"}
	}

	if !config.EnableHTTP2 {
		tlsOpts = append(tlsOpts, disableHTTP2)
	}

	return tlsOpts
}
