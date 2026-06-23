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

var _ = Describe("EqualStringMap", func() {
	It("returns true for two nil maps", func() {
		Expect(EqualStringMap(nil, nil)).To(BeTrue())
	})

	It("treats nil and empty maps as equal", func() {
		Expect(EqualStringMap(nil, map[string]string{})).To(BeTrue())
	})

	It("returns true for equal maps", func() {
		a := map[string]string{"x": "1", "y": "2"}
		b := map[string]string{"y": "2", "x": "1"}
		Expect(EqualStringMap(a, b)).To(BeTrue())
	})

	It("returns false when lengths differ", func() {
		Expect(EqualStringMap(map[string]string{"x": "1"}, map[string]string{"x": "1", "y": "2"})).To(BeFalse())
	})

	It("returns false when a value differs", func() {
		Expect(EqualStringMap(map[string]string{"x": "1"}, map[string]string{"x": "2"})).To(BeFalse())
	})

	It("returns false when a key is missing", func() {
		Expect(EqualStringMap(map[string]string{"x": "1"}, map[string]string{"y": "1"})).To(BeFalse())
	})
})
