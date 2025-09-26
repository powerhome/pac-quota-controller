package usage

import (
	"testing"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
)

func TestUsage(t *testing.T) {
	RegisterFailHandler(Fail)
	RunSpecs(t, "Usage Package Suite")
}
