package certwatcher

import (
	"context"
	"crypto/tls"
	"crypto/x509"
	"encoding/pem"
	"fmt"
	"os"
	"path/filepath"
	"sync"
	"time"

	"go.uber.org/zap"
	"gopkg.in/fsnotify.v1"
)

// CertWatcher watches certificate files and automatically reloads them when they change
type CertWatcher struct {
	certPath   string
	keyPath    string
	cert       *tls.Certificate
	certMutex  sync.RWMutex
	watcher    *fsnotify.Watcher
	log        *zap.Logger
	stopChan   chan struct{}
	reloadChan chan struct{}
}

// NewCertWatcher creates a new certificate watcher
func NewCertWatcher(certPath, keyPath string, log *zap.Logger) (*CertWatcher, error) {
	// Don't validate files exist immediately - we'll wait for them with retries

	// Create fsnotify watcher
	watcher, err := fsnotify.NewWatcher()
	if err != nil {
		return nil, fmt.Errorf("failed to create file watcher: %w", err)
	}

	cw := &CertWatcher{
		certPath:   certPath,
		keyPath:    keyPath,
		watcher:    watcher,
		log:        log,
		stopChan:   make(chan struct{}),
		reloadChan: make(chan struct{}, 1), // Buffered to avoid blocking
	}

	return cw, nil
}

// waitForCertificateFiles waits for certificate files to become available with retries
func (cw *CertWatcher) waitForCertificateFiles(ctx context.Context, maxRetries int, retryInterval time.Duration) error {
	for i := 0; i < maxRetries; i++ {
		// Check if both files exist
		if _, err := os.Stat(cw.certPath); err == nil {
			if _, err := os.Stat(cw.keyPath); err == nil {
				// Both files exist, try to load the certificate
				if err := cw.loadCertificate(); err != nil {
					cw.log.Warn("Certificate files exist but failed to load, retrying",
						zap.Int("attempt", i+1),
						zap.Int("maxRetries", maxRetries),
						zap.Error(err))
				} else {
					cw.log.Info("Successfully loaded certificates",
						zap.Int("attempt", i+1))
					return nil
				}
			}
		}

		if i < maxRetries-1 { // Don't sleep on the last iteration
			cw.log.Info("Waiting for certificate files to become available",
				zap.String("certPath", cw.certPath),
				zap.String("keyPath", cw.keyPath),
				zap.Int("attempt", i+1),
				zap.Int("maxRetries", maxRetries),
				zap.Duration("retryInterval", retryInterval))

			select {
			case <-ctx.Done():
				return ctx.Err()
			case <-time.After(retryInterval):
				// Continue to next iteration
			}
		}
	}

	return fmt.Errorf("failed to load certificates after %d attempts: cert=%s, key=%s",
		maxRetries, cw.certPath, cw.keyPath)
}

// loadCertificate loads the certificate and private key from files
func (cw *CertWatcher) loadCertificate() error {
	cw.certMutex.Lock()
	defer cw.certMutex.Unlock()

	// Read certificate file
	certPEM, err := os.ReadFile(cw.certPath)
	if err != nil {
		return fmt.Errorf("failed to read certificate file: %w", err)
	}

	// Read private key file
	keyPEM, err := os.ReadFile(cw.keyPath)
	if err != nil {
		return fmt.Errorf("failed to read private key file: %w", err)
	}

	// Parse certificate
	certBlock, _ := pem.Decode(certPEM)
	if certBlock == nil {
		return fmt.Errorf("failed to decode certificate PEM")
	}

	cert, err := x509.ParseCertificate(certBlock.Bytes)
	if err != nil {
		return fmt.Errorf("failed to parse certificate: %w", err)
	}

	// Parse private key
	keyBlock, _ := pem.Decode(keyPEM)
	if keyBlock == nil {
		return fmt.Errorf("failed to decode private key PEM")
	}

	// Create TLS certificate
	tlsCert, err := tls.X509KeyPair(certPEM, keyPEM)
	if err != nil {
		return fmt.Errorf("failed to create TLS certificate: %w", err)
	}

	cw.cert = &tlsCert
	cw.log.Info("Certificate loaded successfully",
		zap.String("subject", cert.Subject.CommonName),
		zap.Time("notAfter", cert.NotAfter),
		zap.Time("notBefore", cert.NotBefore))

	return nil
}

// Start starts watching for certificate changes
func (cw *CertWatcher) Start(ctx context.Context) error {
	cw.log.Info("Starting certificate watcher",
		zap.String("certPath", cw.certPath),
		zap.String("keyPath", cw.keyPath))

	// Wait for certificates to be available with retries
	maxRetries := 60 // 5 minutes with 5-second intervals
	retryInterval := 5 * time.Second

	if err := cw.waitForCertificateFiles(ctx, maxRetries, retryInterval); err != nil {
		return fmt.Errorf("failed to wait for certificate files: %w", err)
	}

	// Watch the directory containing the certificate files
	certDir := filepath.Dir(cw.certPath)
	if err := cw.watcher.Add(certDir); err != nil {
		return fmt.Errorf("failed to watch certificate directory: %w", err)
	}

	go cw.watchLoop(ctx)
	return nil
}

// watchLoop monitors file system events for certificate changes
func (cw *CertWatcher) watchLoop(ctx context.Context) {
	for {
		select {
		case <-ctx.Done():
			cw.log.Info("Certificate watcher context cancelled")
			return
		case <-cw.stopChan:
			cw.log.Info("Certificate watcher stopped")
			return
		case event := <-cw.watcher.Events:
			cw.handleFileEvent(event)
		case err := <-cw.watcher.Errors:
			cw.log.Error("Certificate watcher error", zap.Error(err))
		}
	}
}

// handleFileEvent processes file system events
func (cw *CertWatcher) handleFileEvent(event fsnotify.Event) {
	// Check if the event is for our certificate or key files
	if event.Name != cw.certPath && event.Name != cw.keyPath {
		return
	}

	// Only reload on write events
	if event.Op&fsnotify.Write == 0 {
		return
	}

	cw.log.Info("Certificate file changed, reloading",
		zap.String("file", event.Name),
		zap.String("operation", event.Op.String()))

	// Add a small delay to ensure the file write is complete
	time.Sleep(100 * time.Millisecond)

	// Reload certificate
	if err := cw.loadCertificate(); err != nil {
		cw.log.Error("Failed to reload certificate", zap.Error(err))
		return
	}

	// Signal reload (for external consumers)
	select {
	case cw.reloadChan <- struct{}{}:
	default:
		// Channel is full, skip
	}
}

// GetCertificate returns the current certificate for TLS configuration
func (cw *CertWatcher) GetCertificate(clientHello *tls.ClientHelloInfo) (*tls.Certificate, error) {
	cw.certMutex.RLock()
	defer cw.certMutex.RUnlock()

	if cw.cert == nil {
		return nil, fmt.Errorf("no certificate available")
	}

	return cw.cert, nil
}

// Stop stops the certificate watcher
func (cw *CertWatcher) Stop() {
	cw.certMutex.Lock()
	defer cw.certMutex.Unlock()

	// Only close channels if they haven't been closed yet
	select {
	case <-cw.stopChan:
		// Channel already closed, do nothing
		return
	default:
		// Channel not closed, close it
		close(cw.stopChan)
	}

	// Close the reload channel
	select {
	case <-cw.reloadChan:
		// Channel already closed, do nothing
	default:
		// Channel not closed, close it
		close(cw.reloadChan)
	}

	// Close the file watcher
	if cw.watcher != nil {
		if err := cw.watcher.Close(); err != nil {
			cw.log.Error("Failed to close watcher", zap.Error(err))
		}
	}

	cw.log.Info("Certificate watcher stopped")
}

// GetReloadChannel returns a channel that receives events when certificates are reloaded
func (cw *CertWatcher) GetReloadChannel() <-chan struct{} {
	return cw.reloadChan
}

// GetCertificateInfo returns information about the current certificate
func (cw *CertWatcher) GetCertificateInfo() (*CertificateInfo, error) {
	cw.certMutex.RLock()
	defer cw.certMutex.RUnlock()

	if cw.cert == nil || cw.cert.Leaf == nil {
		return nil, fmt.Errorf("no certificate available")
	}

	return &CertificateInfo{
		Subject:   cw.cert.Leaf.Subject.CommonName,
		NotBefore: cw.cert.Leaf.NotBefore,
		NotAfter:  cw.cert.Leaf.NotAfter,
		Issuer:    cw.cert.Leaf.Issuer.CommonName,
	}, nil
}

// CertificateInfo contains information about a certificate
type CertificateInfo struct {
	Subject   string
	NotBefore time.Time
	NotAfter  time.Time
	Issuer    string
}
