package utils

import (
	"testing"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
)

func TestUtils(t *testing.T) {
	RegisterFailHandler(Fail)
	RunSpecs(t, "Utils Package Suite")
}

var _ = Describe("DeduplicateAndSort", func() {
	It("should return nil for empty slice", func() {
		result := DeduplicateAndSort([]string{})
		Expect(result).To(BeNil())
	})

	It("should sort slice without duplicates", func() {
		input := []string{"c", "b", "a"}
		result := DeduplicateAndSort(input)
		expected := []string{"a", "b", "c"}
		Expect(result).To(Equal(expected))
	})

	It("should remove duplicates and sort", func() {
		input := []string{"b", "a", "b", "c", "a"}
		result := DeduplicateAndSort(input)
		expected := []string{"a", "b", "c"}
		Expect(result).To(Equal(expected))
	})

	It("should handle already sorted slice with duplicates", func() {
		input := []string{"a", "a", "b", "c", "c"}
		result := DeduplicateAndSort(input)
		expected := []string{"a", "b", "c"}
		Expect(result).To(Equal(expected))
	})
})
