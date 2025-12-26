package namespace

import (
	"testing"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	pkglogger "github.com/powerhome/pac-quota-controller/pkg/logger"
)

func TestNamespace(t *testing.T) {
	RegisterFailHandler(Fail)
	RunSpecs(t, "Namespace Package Suite")
}

var _ = BeforeSuite(func() {
	pkglogger.InitTest()
})
