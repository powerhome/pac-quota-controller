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
		logger, _ := zap.NewDevelopment()
		ms := NewMetricsServer(logger)
		Expect(ms).NotTo(BeNil())
		Expect(ms.server).NotTo(BeNil())
		Expect(ms.server.Addr).To(Equal(":8081"))
	})
})
