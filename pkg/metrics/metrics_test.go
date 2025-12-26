package metrics

import (
	"testing"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	"go.uber.org/zap"
)

func TestMetrics(t *testing.T) {
	RegisterFailHandler(Fail)
	RunSpecs(t, "Metrics Package Suite")
}

var _ = Describe("MetricsServer", func() {
	It("should setup metrics server struct and underlying http.Server", func() {
		logger := zap.NewNop()
		ms, err := NewMetricsServer(logger)
		Expect(err).NotTo(HaveOccurred())
		Expect(ms).NotTo(BeNil())
		Expect(ms.server).NotTo(BeNil())
		Expect(ms.server.Addr).To(Equal(":8443"))
	})
})
