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

var _ = Describe("SetupStandaloneMetricsServer", func() {
	It("should setup standalone metrics server", func() {
		cfg := &config.Config{
			MetricsAddr: ":8080",
		}
		logger := zap.NewNop()
		server, err := SetupStandaloneMetricsServer(cfg, logger)
		Expect(err).NotTo(HaveOccurred())
		Expect(server).NotTo(BeNil())
		Expect(server.Addr).To(Equal(":8080"))
	})
})
