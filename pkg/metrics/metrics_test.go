package metrics

import (
	"testing"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	"go.uber.org/zap"

	"github.com/powerhome/pac-quota-controller/pkg/config"
)

func TestMetrics(t *testing.T) {
	RegisterFailHandler(Fail)
	RunSpecs(t, "Metrics Package Suite")
}

var _ = Describe("MetricsServer", func() {
	It("should setup metrics server struct and underlying http.Server", func() {
		cfg := &config.Config{
			MetricsPort:   8080,
			SecureMetrics: false,
			// No certificate paths provided for this test.
		}
		logger, _ := zap.NewDevelopment()
		ms, err := NewMetricsServer(cfg, logger)
		Expect(err).NotTo(HaveOccurred())
		Expect(ms).NotTo(BeNil())
		Expect(ms.server).NotTo(BeNil())
		Expect(ms.server.Addr).To(Equal(":8080"))
	})
})
