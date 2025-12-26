package services

import (
	"testing"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	pkglogger "github.com/powerhome/pac-quota-controller/pkg/logger"
)

func TestService(t *testing.T) {
	RegisterFailHandler(Fail)
	RunSpecs(t, "Service Package Suite")
}

var _ = BeforeSuite(func() {
	pkglogger.InitTest()
})
