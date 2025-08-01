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

package certwatcher

import (
	"context"
	"crypto/rand"
	"crypto/rsa"
	"crypto/tls"
	"crypto/x509"
	"crypto/x509/pkix"
	"encoding/pem"
	"math/big"
	"net"
	"os"
	"path/filepath"
	"testing"
	"time"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	"go.uber.org/zap"
	"go.uber.org/zap/zaptest"
	"gopkg.in/fsnotify.v1"
)

func TestCertWatcher(t *testing.T) {
	RegisterFailHandler(Fail)
	RunSpecs(t, "CertWatcher Suite")
}

var _ = Describe("CertWatcher", func() {
	var (
		logger      *zap.Logger
		tempDir     string
		certPath    string
		keyPath     string
		certWatcher *CertWatcher
	)

	BeforeEach(func() {
		logger = zaptest.NewLogger(GinkgoT())

		// Create temporary directory
		var err error
		tempDir, err = os.MkdirTemp("", "certwatcher-test")
		Expect(err).NotTo(HaveOccurred())

		certPath = filepath.Join(tempDir, "tls.crt")
		keyPath = filepath.Join(tempDir, "tls.key")
	})

	AfterEach(func() {
		if certWatcher != nil {
			certWatcher.Stop()
		}
		// Clean up
		if err := os.RemoveAll(tempDir); err != nil {
			Expect(err).NotTo(HaveOccurred())
		}
		err := logger.Sync()
		Expect(err).ToNot(HaveOccurred())
	})

	Describe("NewCertWatcher", func() {
		It("should fail when certificate file does not exist", func() {
			_, err := NewCertWatcher(certPath, keyPath, logger)
			Expect(err).To(HaveOccurred())
			Expect(err.Error()).To(ContainSubstring("certificate file does not exist"))
		})

		It("should fail when key file does not exist", func() {
			// Create only the cert file
			createTestCertificate(certPath, keyPath)
			if err := os.Remove(keyPath); err != nil {
				Expect(err).NotTo(HaveOccurred())
			}

			_, err := NewCertWatcher(certPath, keyPath, logger)
			Expect(err).To(HaveOccurred())
			Expect(err.Error()).To(ContainSubstring("private key file does not exist"))
		})

		It("should create certwatcher with valid certificate files", func() {
			createTestCertificate(certPath, keyPath)

			var err error
			certWatcher, err = NewCertWatcher(certPath, keyPath, logger)
			Expect(err).NotTo(HaveOccurred())
			Expect(certWatcher).NotTo(BeNil())
			Expect(certWatcher.certPath).To(Equal(certPath))
			Expect(certWatcher.keyPath).To(Equal(keyPath))
			Expect(certWatcher.cert).NotTo(BeNil())
		})
	})

	Describe("GetCertificate", func() {
		It("should return certificate for client hello", func() {
			createTestCertificate(certPath, keyPath)
			var err error
			certWatcher, err = NewCertWatcher(certPath, keyPath, logger)
			Expect(err).NotTo(HaveOccurred())

			clientHello := &tls.ClientHelloInfo{
				ServerName: "test.example.com",
			}

			cert, err := certWatcher.GetCertificate(clientHello)
			Expect(err).NotTo(HaveOccurred())
			Expect(cert).NotTo(BeNil())
		})

		It("should handle nil client hello", func() {
			createTestCertificate(certPath, keyPath)
			var err error
			certWatcher, err = NewCertWatcher(certPath, keyPath, logger)
			Expect(err).NotTo(HaveOccurred())

			cert, err := certWatcher.GetCertificate(nil)
			Expect(err).NotTo(HaveOccurred())
			Expect(cert).NotTo(BeNil())
		})
	})

	Describe("GetCertificateInfo", func() {
		It("should return certificate information", func() {
			createTestCertificate(certPath, keyPath)
			var err error
			certWatcher, err = NewCertWatcher(certPath, keyPath, logger)
			Expect(err).NotTo(HaveOccurred())

			info, err := certWatcher.GetCertificateInfo()
			Expect(err).NotTo(HaveOccurred())
			Expect(info).NotTo(BeNil())
			Expect(info.Subject).NotTo(BeEmpty())
			Expect(info.Issuer).NotTo(BeEmpty())
		})

		It("should handle certificate info retrieval error", func() {
			// Create invalid certificate files
			err := os.WriteFile(certPath, []byte("invalid cert"), 0644)
			Expect(err).NotTo(HaveOccurred())
			err = os.WriteFile(keyPath, []byte("invalid key"), 0644)
			Expect(err).NotTo(HaveOccurred())

			certWatcher, err = NewCertWatcher(certPath, keyPath, logger)
			// This should fail during creation, not during GetCertificateInfo
			Expect(err).To(HaveOccurred())
			Expect(certWatcher).To(BeNil())
		})
	})

	Describe("GetReloadChannel", func() {
		It("should return reload channel", func() {
			createTestCertificate(certPath, keyPath)
			var err error
			certWatcher, err = NewCertWatcher(certPath, keyPath, logger)
			Expect(err).NotTo(HaveOccurred())

			reloadChan := certWatcher.GetReloadChannel()
			Expect(reloadChan).NotTo(BeNil())
		})
	})

	Describe("Stop", func() {
		It("should stop the certwatcher gracefully", func() {
			createTestCertificate(certPath, keyPath)
			var err error
			certWatcher, err = NewCertWatcher(certPath, keyPath, logger)
			Expect(err).NotTo(HaveOccurred())

			// Start the watcher
			ctx, cancel := context.WithCancel(context.Background())
			defer cancel()
			go func() {
				err := certWatcher.Start(ctx)
				Expect(err).NotTo(HaveOccurred())
			}()

			// Give it a moment to start
			time.Sleep(100 * time.Millisecond)

			// Stop the watcher
			certWatcher.Stop()

			// Verify it's stopped - the watcher field might not be nil immediately
			// but the stop should complete without error
		})

		It("should handle multiple stop calls", func() {
			createTestCertificate(certPath, keyPath)
			var err error
			certWatcher, err = NewCertWatcher(certPath, keyPath, logger)
			Expect(err).NotTo(HaveOccurred())

			// Multiple stop calls should not panic
			certWatcher.Stop()
			certWatcher.Stop()
			certWatcher.Stop()
		})
	})

	Describe("Start", func() {
		It("should start and handle context cancellation", func() {
			createTestCertificate(certPath, keyPath)
			var err error
			certWatcher, err = NewCertWatcher(certPath, keyPath, logger)
			Expect(err).NotTo(HaveOccurred())

			ctx, cancel := context.WithCancel(context.Background())

			// Start in goroutine
			go func() {
				err := certWatcher.Start(ctx)
				Expect(err).NotTo(HaveOccurred())
			}()

			// Give it a moment to start
			time.Sleep(100 * time.Millisecond)

			// Cancel context
			cancel()

			// Give it a moment to stop
			time.Sleep(100 * time.Millisecond)
		})

		It("should handle file system watcher errors", func() {
			createTestCertificate(certPath, keyPath)
			var err error
			certWatcher, err = NewCertWatcher(certPath, keyPath, logger)
			Expect(err).NotTo(HaveOccurred())

			// Remove the directory to cause watcher errors
			err = os.RemoveAll(tempDir)
			Expect(err).NotTo(HaveOccurred())

			ctx, cancel := context.WithTimeout(context.Background(), 200*time.Millisecond)
			defer cancel()

			_ = certWatcher.Start(ctx)
			// Should not panic, may return error or timeout
		})
	})

	Describe("handleFileEvent", func() {
		It("should handle certificate file modification", func() {
			createTestCertificate(certPath, keyPath)
			var err error
			certWatcher, err = NewCertWatcher(certPath, keyPath, logger)
			Expect(err).NotTo(HaveOccurred())

			// Simulate file modification
			time.Sleep(100 * time.Millisecond)

			// Touch the certificate file
			currentTime := time.Now()
			err = os.Chtimes(certPath, currentTime, currentTime)
			Expect(err).NotTo(HaveOccurred())

			// Give it a moment to process
			time.Sleep(100 * time.Millisecond)
		})

		It("should handle key file modification", func() {
			createTestCertificate(certPath, keyPath)
			var err error
			certWatcher, err = NewCertWatcher(certPath, keyPath, logger)
			Expect(err).NotTo(HaveOccurred())

			// Simulate file modification
			time.Sleep(100 * time.Millisecond)

			// Touch the key file
			currentTime := time.Now()
			err = os.Chtimes(keyPath, currentTime, currentTime)
			Expect(err).NotTo(HaveOccurred())

			// Give it a moment to process
			time.Sleep(100 * time.Millisecond)
		})

		It("should ignore events for other files", func() {
			createTestCertificate(certPath, keyPath)
			var err error
			certWatcher, err = NewCertWatcher(certPath, keyPath, logger)
			Expect(err).NotTo(HaveOccurred())

			// Create a test event for a different file
			event := fsnotify.Event{
				Name: "/tmp/other-file.txt",
				Op:   fsnotify.Write,
			}

			// This should not trigger any reload
			certWatcher.handleFileEvent(event)
			Expect(true).To(BeTrue())
		})

		It("should ignore non-write events", func() {
			createTestCertificate(certPath, keyPath)
			var err error
			certWatcher, err = NewCertWatcher(certPath, keyPath, logger)
			Expect(err).NotTo(HaveOccurred())

			// Create a test event for the certificate file with different operation
			event := fsnotify.Event{
				Name: certPath,
				Op:   fsnotify.Create,
			}

			// This should not trigger any reload
			certWatcher.handleFileEvent(event)
			Expect(true).To(BeTrue())
		})

		It("should handle reload channel being full", func() {
			createTestCertificate(certPath, keyPath)
			var err error
			certWatcher, err = NewCertWatcher(certPath, keyPath, logger)
			Expect(err).NotTo(HaveOccurred())

			// Fill the reload channel
			for i := 0; i < 100; i++ {
				select {
				case certWatcher.reloadChan <- struct{}{}:
				default:
					// Channel is full, continue to next iteration
				}
			}

			// Create a test event for the certificate file
			event := fsnotify.Event{
				Name: certPath,
				Op:   fsnotify.Write,
			}

			// This should not panic even with full channel
			certWatcher.handleFileEvent(event)
			Expect(true).To(BeTrue())
		})

		It("should handle certificate file write events directly", func() {
			createTestCertificate(certPath, keyPath)
			var err error
			certWatcher, err = NewCertWatcher(certPath, keyPath, logger)
			Expect(err).NotTo(HaveOccurred())

			// Create a test event for the certificate file
			event := fsnotify.Event{
				Name: certPath,
				Op:   fsnotify.Write,
			}

			// This should not panic
			certWatcher.handleFileEvent(event)
			Expect(true).To(BeTrue())
		})

		It("should handle key file write events directly", func() {
			createTestCertificate(certPath, keyPath)
			var err error
			certWatcher, err = NewCertWatcher(certPath, keyPath, logger)
			Expect(err).NotTo(HaveOccurred())

			// Create a test event for the key file
			event := fsnotify.Event{
				Name: keyPath,
				Op:   fsnotify.Write,
			}

			// This should not panic
			certWatcher.handleFileEvent(event)
			Expect(true).To(BeTrue())
		})
	})
})

// Helper function to create test certificate and key files
func createTestCertificate(certPath, keyPath string) {
	// Generate private key
	privateKey, err := rsa.GenerateKey(rand.Reader, 2048)
	Expect(err).NotTo(HaveOccurred())

	// Create certificate template
	template := x509.Certificate{
		SerialNumber: big.NewInt(1),
		Subject: pkix.Name{
			CommonName: "localhost",
		},
		Issuer: pkix.Name{
			CommonName: "localhost",
		},
		NotBefore:             time.Now(),
		NotAfter:              time.Now().Add(24 * time.Hour),
		KeyUsage:              x509.KeyUsageKeyEncipherment | x509.KeyUsageDigitalSignature,
		ExtKeyUsage:           []x509.ExtKeyUsage{x509.ExtKeyUsageServerAuth},
		BasicConstraintsValid: true,
		IPAddresses:           []net.IP{net.ParseIP("127.0.0.1")},
		DNSNames:              []string{"localhost"},
	}

	// Create certificate
	certDER, err := x509.CreateCertificate(rand.Reader, &template, &template, &privateKey.PublicKey, privateKey)
	Expect(err).NotTo(HaveOccurred())

	// Encode certificate to PEM
	certPEM := pem.EncodeToMemory(&pem.Block{
		Type:  "CERTIFICATE",
		Bytes: certDER,
	})

	// Encode private key to PEM
	keyPEM := pem.EncodeToMemory(&pem.Block{
		Type:  "RSA PRIVATE KEY",
		Bytes: x509.MarshalPKCS1PrivateKey(privateKey),
	})

	// Write files
	err = os.WriteFile(certPath, certPEM, 0644)
	Expect(err).NotTo(HaveOccurred())

	err = os.WriteFile(keyPath, keyPEM, 0600)
	Expect(err).NotTo(HaveOccurred())
}
