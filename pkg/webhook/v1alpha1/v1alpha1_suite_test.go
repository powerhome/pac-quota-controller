package v1alpha1

import (
	"testing"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	"github.com/powerhome/pac-quota-controller/pkg/logger"
)

func TestV1Alpha1(t *testing.T) {
	RegisterFailHandler(Fail)
	RunSpecs(t, "Webhook V1Alpha1 Package Suite")
}

var _ = BeforeSuite(func() {
	logger.InitTest()
})
