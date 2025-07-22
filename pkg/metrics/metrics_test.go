package metrics

import (
	"crypto/tls"
	"testing"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"

	"github.com/powerhome/pac-quota-controller/pkg/config"
)

func TestMetrics(t *testing.T) {
	RegisterFailHandler(Fail)
	RunSpecs(t, "Metrics Package Suite")
}

var _ = Describe("SetupMetricsServer", func() {
	It("should setup insecure metrics server", func() {
		cfg := &config.Config{
			MetricsAddr:     ":8080",
			SecureMetrics:   false,
			MetricsCertPath: "",
		}
		options, watcher, err := SetupMetricsServer(cfg, nil)
		Expect(err).NotTo(HaveOccurred())
		Expect(options.BindAddress).To(Equal(":8080"))
		Expect(options.SecureServing).To(BeFalse())
		Expect(watcher).To(BeNil())
	})

	It("should setup secure metrics server without certs", func() {
		cfg := &config.Config{
			MetricsAddr:     ":8443",
			SecureMetrics:   true,
			MetricsCertPath: "",
		}
		options, watcher, err := SetupMetricsServer(cfg, []func(*tls.Config){})
		Expect(err).NotTo(HaveOccurred())
		Expect(options.BindAddress).To(Equal(":8443"))
		Expect(options.SecureServing).To(BeTrue())
		Expect(watcher).To(BeNil())
		Expect(options.FilterProvider).NotTo(BeNil())
	})

	It("should return error for invalid cert path", func() {
		cfg := &config.Config{
			MetricsAddr:     ":8443",
			SecureMetrics:   true,
			MetricsCertPath: "/invalid/path",
			MetricsCertName: "cert.pem",
			MetricsCertKey:  "key.pem",
		}
		_, _, err := SetupMetricsServer(cfg, nil)
		Expect(err).To(HaveOccurred())
	})
})
