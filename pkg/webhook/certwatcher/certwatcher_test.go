package certwatcher

import (
	"context"
	"crypto/ecdsa"
	"crypto/elliptic"
	"crypto/rand"
	"crypto/x509"
	"crypto/x509/pkix"
	"encoding/pem"
	"math/big"
	"os"
	"path/filepath"
	"testing"
	"time"

	"go.uber.org/zap"
	"gopkg.in/fsnotify.v1"
)

// writeCertPair generates a self-signed cert/key pair into dir and returns their paths.
func writeCertPair(t *testing.T, dir string) (certPath, keyPath string) {
	t.Helper()
	priv, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	if err != nil {
		t.Fatalf("generate key: %v", err)
	}
	tmpl := x509.Certificate{
		SerialNumber:          big.NewInt(1),
		Subject:               pkix.Name{CommonName: "certwatcher-test"},
		NotBefore:             time.Now().Add(-time.Hour),
		NotAfter:              time.Now().Add(time.Hour),
		KeyUsage:              x509.KeyUsageDigitalSignature,
		BasicConstraintsValid: true,
	}
	der, err := x509.CreateCertificate(rand.Reader, &tmpl, &tmpl, &priv.PublicKey, priv)
	if err != nil {
		t.Fatalf("create cert: %v", err)
	}
	keyDER, err := x509.MarshalECPrivateKey(priv)
	if err != nil {
		t.Fatalf("marshal key: %v", err)
	}

	certPath = filepath.Join(dir, "tls.crt")
	keyPath = filepath.Join(dir, "tls.key")
	writePEM(t, certPath, "CERTIFICATE", der)
	writePEM(t, keyPath, "EC PRIVATE KEY", keyDER)
	return certPath, keyPath
}

func writePEM(t *testing.T, path, blockType string, der []byte) {
	t.Helper()
	if err := os.WriteFile(path, pem.EncodeToMemory(&pem.Block{Type: blockType, Bytes: der}), 0o600); err != nil {
		t.Fatalf("write %s: %v", path, err)
	}
}

func newWatcher(t *testing.T, certPath, keyPath string) *CertWatcher {
	t.Helper()
	cw, err := NewCertWatcher(certPath, keyPath, zap.NewNop())
	if err != nil {
		t.Fatalf("NewCertWatcher: %v", err)
	}
	t.Cleanup(cw.Stop)
	return cw
}

func TestNewCertWatcher(t *testing.T) {
	cw, err := NewCertWatcher("/no/cert", "/no/key", zap.NewNop())
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	defer cw.Stop()
	if cw.watcher == nil || cw.reloadChan == nil || cw.stopChan == nil {
		t.Fatal("watcher not fully initialized")
	}
}

func TestLoadCertificate(t *testing.T) {
	t.Run("success", func(t *testing.T) {
		dir := t.TempDir()
		certPath, keyPath := writeCertPair(t, dir)
		cw := newWatcher(t, certPath, keyPath)

		if err := cw.loadCertificate(); err != nil {
			t.Fatalf("loadCertificate: %v", err)
		}
		got, err := cw.GetCertificate(nil)
		if err != nil || got == nil {
			t.Fatalf("GetCertificate after load: cert=%v err=%v", got, err)
		}
	})

	t.Run("missing cert file", func(t *testing.T) {
		dir := t.TempDir()
		_, keyPath := writeCertPair(t, dir)
		cw := newWatcher(t, filepath.Join(dir, "absent.crt"), keyPath)
		if err := cw.loadCertificate(); err == nil {
			t.Fatal("expected error for missing cert file")
		}
	})

	t.Run("missing key file", func(t *testing.T) {
		dir := t.TempDir()
		certPath, _ := writeCertPair(t, dir)
		cw := newWatcher(t, certPath, filepath.Join(dir, "absent.key"))
		if err := cw.loadCertificate(); err == nil {
			t.Fatal("expected error for missing key file")
		}
	})

	t.Run("malformed cert PEM", func(t *testing.T) {
		dir := t.TempDir()
		_, keyPath := writeCertPair(t, dir)
		certPath := filepath.Join(dir, "bad.crt")
		if err := os.WriteFile(certPath, []byte("not a pem"), 0o600); err != nil {
			t.Fatal(err)
		}
		cw := newWatcher(t, certPath, keyPath)
		if err := cw.loadCertificate(); err == nil {
			t.Fatal("expected decode error for malformed cert PEM")
		}
	})

	t.Run("malformed key PEM", func(t *testing.T) {
		dir := t.TempDir()
		certPath, _ := writeCertPair(t, dir)
		keyPath := filepath.Join(dir, "bad.key")
		if err := os.WriteFile(keyPath, []byte("not a pem"), 0o600); err != nil {
			t.Fatal(err)
		}
		cw := newWatcher(t, certPath, keyPath)
		if err := cw.loadCertificate(); err == nil {
			t.Fatal("expected decode error for malformed key PEM")
		}
	})
}

func TestGetCertificateNoCert(t *testing.T) {
	cw := newWatcher(t, "/no/cert", "/no/key")
	if _, err := cw.GetCertificate(nil); err == nil {
		t.Fatal("expected error when no certificate is loaded")
	}
}

func TestWaitForCertificateFiles(t *testing.T) {
	t.Run("loads when files are present", func(t *testing.T) {
		dir := t.TempDir()
		certPath, keyPath := writeCertPair(t, dir)
		cw := newWatcher(t, certPath, keyPath)
		if err := cw.waitForCertificateFiles(context.Background(), 3, time.Millisecond); err != nil {
			t.Fatalf("waitForCertificateFiles: %v", err)
		}
	})

	t.Run("times out when files never appear", func(t *testing.T) {
		cw := newWatcher(t, "/no/cert", "/no/key")
		if err := cw.waitForCertificateFiles(context.Background(), 2, time.Millisecond); err == nil {
			t.Fatal("expected timeout error")
		}
	})

	t.Run("returns on context cancellation", func(t *testing.T) {
		cw := newWatcher(t, "/no/cert", "/no/key")
		ctx, cancel := context.WithCancel(context.Background())
		cancel()
		if err := cw.waitForCertificateFiles(ctx, 5, time.Hour); err != context.Canceled {
			t.Fatalf("expected context.Canceled, got %v", err)
		}
	})
}

func TestHandleFileEvent(t *testing.T) {
	t.Run("ignores unrelated files", func(t *testing.T) {
		dir := t.TempDir()
		certPath, keyPath := writeCertPair(t, dir)
		cw := newWatcher(t, certPath, keyPath)
		cw.handleFileEvent(fsnotify.Event{Name: filepath.Join(dir, "other"), Op: fsnotify.Write})
		if _, err := cw.GetCertificate(nil); err == nil {
			t.Fatal("unrelated file should not have triggered a load")
		}
	})

	t.Run("ignores non-write operations", func(t *testing.T) {
		dir := t.TempDir()
		certPath, keyPath := writeCertPair(t, dir)
		cw := newWatcher(t, certPath, keyPath)
		cw.handleFileEvent(fsnotify.Event{Name: certPath, Op: fsnotify.Chmod})
		if _, err := cw.GetCertificate(nil); err == nil {
			t.Fatal("non-write op should not have triggered a load")
		}
	})

	t.Run("reloads and signals on write", func(t *testing.T) {
		dir := t.TempDir()
		certPath, keyPath := writeCertPair(t, dir)
		cw := newWatcher(t, certPath, keyPath)

		cw.handleFileEvent(fsnotify.Event{Name: certPath, Op: fsnotify.Write})

		select {
		case <-cw.GetReloadChannel():
		case <-time.After(2 * time.Second):
			t.Fatal("expected a reload signal after a write event")
		}
		if _, err := cw.GetCertificate(nil); err != nil {
			t.Fatalf("certificate should be loaded after reload: %v", err)
		}
	})
}

func TestGetCertificateInfo(t *testing.T) {
	t.Run("errors when no certificate", func(t *testing.T) {
		cw := newWatcher(t, "/no/cert", "/no/key")
		if _, err := cw.GetCertificateInfo(); err == nil {
			t.Fatal("expected error when no certificate is loaded")
		}
	})

	t.Run("returns info once loaded", func(t *testing.T) {
		dir := t.TempDir()
		certPath, keyPath := writeCertPair(t, dir)
		cw := newWatcher(t, certPath, keyPath)
		if err := cw.loadCertificate(); err != nil {
			t.Fatalf("loadCertificate: %v", err)
		}
		info, err := cw.GetCertificateInfo()
		if err != nil {
			t.Fatalf("GetCertificateInfo: %v", err)
		}
		if info.Subject != "certwatcher-test" {
			t.Fatalf("unexpected subject %q", info.Subject)
		}
	})
}

func TestStartAndStop(t *testing.T) {
	dir := t.TempDir()
	certPath, keyPath := writeCertPair(t, dir)
	cw, err := NewCertWatcher(certPath, keyPath, zap.NewNop())
	if err != nil {
		t.Fatalf("NewCertWatcher: %v", err)
	}

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	if err := cw.Start(ctx); err != nil {
		t.Fatalf("Start: %v", err)
	}
	if _, err := cw.GetCertificate(nil); err != nil {
		t.Fatalf("certificate should be loaded after Start: %v", err)
	}

	// Stop must be safe to call more than once.
	cw.Stop()
	cw.Stop()
}
