package objectcount

import (
	"testing"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
)

func TestObjectCountCalculator(t *testing.T) {
	RegisterFailHandler(Fail)
	RunSpecs(t, "ObjectCountCalculator Suite")
}
