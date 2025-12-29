package usage

import (
	"testing"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	pkglogger "github.com/powerhome/pac-quota-controller/pkg/logger"
)

func TestUsage(t *testing.T) {
	RegisterFailHandler(Fail)
	RunSpecs(t, "Usage Package Suite")
}

var _ = BeforeSuite(func() {
	pkglogger.InitTest()
})
