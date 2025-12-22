package usage

import (
	"fmt"

	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/resource"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
)

var _ = Describe("Usage", func() {
	Describe("BaseResourceCalculator", func() {
		It("should create new base resource calculator", func() {
			calculator := &BaseResourceCalculator{}
			Expect(calculator).NotTo(BeNil())
		})

		It("should create calculator with nil client", func() {
			calculator := NewBaseResourceCalculator(nil)
			Expect(calculator).NotTo(BeNil())
			Expect(calculator.Client).To(BeNil())
		})
	})

	Describe("UsageResult", func() {
		var result *UsageResult

		BeforeEach(func() {
			result = NewUsageResult("test-namespace")
		})

		It("should create new usage result", func() {
			Expect(result).NotTo(BeNil())
			Expect(result.Namespace).To(Equal("test-namespace"))
			Expect(result.Usage).NotTo(BeNil())
			Expect(result.Errors).NotTo(BeNil())
			Expect(result.ResourceCount).To(Equal(0))
		})

		It("should add usage correctly", func() {
			quantity := resource.MustParse("100m")
			result.AddUsage(corev1.ResourceCPU, quantity)

			Expect(result.Usage[corev1.ResourceCPU]).To(Equal(quantity))
			Expect(result.ResourceCount).To(Equal(1))
		})

		It("should add multiple usages", func() {
			cpuQuantity := resource.MustParse("100m")
			memoryQuantity := resource.MustParse("128Mi")

			result.AddUsage(corev1.ResourceCPU, cpuQuantity)
			result.AddUsage(corev1.ResourceMemory, memoryQuantity)

			Expect(result.Usage[corev1.ResourceCPU]).To(Equal(cpuQuantity))
			Expect(result.Usage[corev1.ResourceMemory]).To(Equal(memoryQuantity))
			Expect(result.ResourceCount).To(Equal(2))
		})

		It("should overwrite existing usage", func() {
			quantity1 := resource.MustParse("100m")
			quantity2 := resource.MustParse("200m")

			result.AddUsage(corev1.ResourceCPU, quantity1)
			result.AddUsage(corev1.ResourceCPU, quantity2)

			Expect(result.Usage[corev1.ResourceCPU]).To(Equal(quantity2))
			Expect(result.ResourceCount).To(Equal(2)) // Counts each addition
		})

		It("should add errors correctly", func() {
			err1 := resource.ErrFormatWrong
			err2 := fmt.Errorf("custom error")

			result.AddError(err1)
			result.AddError(err2)

			Expect(result.Errors).To(HaveLen(2))
			Expect(result.Errors).To(ContainElement(err1))
			Expect(result.Errors).To(ContainElement(err2))
		})

		It("should detect errors correctly", func() {
			Expect(result.HasErrors()).To(BeFalse())

			result.AddError(resource.ErrFormatWrong)
			Expect(result.HasErrors()).To(BeTrue())
		})

		It("should get total usage for existing resource", func() {
			quantity := resource.MustParse("100m")
			result.AddUsage(corev1.ResourceCPU, quantity)

			totalUsage := result.GetTotalUsage(corev1.ResourceCPU)
			Expect(totalUsage).To(Equal(quantity))
		})

		It("should return empty quantity for non-existent resource", func() {
			totalUsage := result.GetTotalUsage(corev1.ResourceCPU)
			Expect(totalUsage).To(Equal(resource.Quantity{}))
		})

		It("should handle zero quantity", func() {
			zeroQuantity := resource.MustParse("0")
			result.AddUsage(corev1.ResourceCPU, zeroQuantity)

			totalUsage := result.GetTotalUsage(corev1.ResourceCPU)
			Expect(totalUsage).To(Equal(zeroQuantity))
		})

		It("should handle large quantities", func() {
			largeQuantity := resource.MustParse("1Ti")
			result.AddUsage(corev1.ResourceMemory, largeQuantity)

			totalUsage := result.GetTotalUsage(corev1.ResourceMemory)
			Expect(totalUsage).To(Equal(largeQuantity))
		})

		It("should handle decimal quantities", func() {
			decimalQuantity := resource.MustParse("1.5")
			result.AddUsage(corev1.ResourceCPU, decimalQuantity)

			totalUsage := result.GetTotalUsage(corev1.ResourceCPU)
			Expect(totalUsage).To(Equal(decimalQuantity))
		})
	})

	Describe("ResourceUsage", func() {
		It("should create resource usage struct", func() {
			usage := &ResourceUsage{
				ResourceName: corev1.ResourceCPU,
				Quantity:     resource.MustParse("100m"),
				Error:        nil,
			}

			Expect(usage.ResourceName).To(Equal(corev1.ResourceCPU))
			Expect(usage.Quantity).To(Equal(resource.MustParse("100m")))
			Expect(usage.Error).ToNot(HaveOccurred())
		})

		It("should handle resource usage with error", func() {
			err := resource.ErrFormatWrong
			usage := &ResourceUsage{
				ResourceName: corev1.ResourceMemory,
				Quantity:     resource.Quantity{},
				Error:        err,
			}

			Expect(usage.ResourceName).To(Equal(corev1.ResourceMemory))
			Expect(usage.Quantity).To(Equal(resource.Quantity{}))
			Expect(usage.Error).To(Equal(err))
		})
	})

	Describe("Common Resource Names", func() {
		It("should have correct CPU resource names", func() {
			Expect(ResourceRequestsCPU).To(Equal(corev1.ResourceRequestsCPU))
			Expect(ResourceLimitsCPU).To(Equal(corev1.ResourceLimitsCPU))
			Expect(ResourceCPU).To(Equal(corev1.ResourceCPU))
		})

		It("should have correct memory resource names", func() {
			Expect(ResourceRequestsMemory).To(Equal(corev1.ResourceRequestsMemory))
			Expect(ResourceLimitsMemory).To(Equal(corev1.ResourceLimitsMemory))
			Expect(ResourceMemory).To(Equal(corev1.ResourceMemory))
		})

		It("should have correct storage resource names", func() {
			Expect(ResourceRequestsStorage).To(Equal(corev1.ResourceRequestsStorage))
			Expect(ResourceStorage).To(Equal(corev1.ResourceStorage))
		})

		It("should have correct ephemeral storage resource names", func() {
			Expect(ResourceRequestsEphemeralStorage).To(Equal(corev1.ResourceRequestsEphemeralStorage))
			Expect(ResourceLimitsEphemeralStorage).To(Equal(corev1.ResourceLimitsEphemeralStorage))
			Expect(ResourceEphemeralStorage).To(Equal(corev1.ResourceEphemeralStorage))
		})
	})

	Describe("Resource Quantity Utilities", func() {
		It("should create new quantity with decimal format", func() {
			quantity := NewQuantity(100, resource.DecimalSI)
			Expect(quantity.MilliValue()).To(Equal(int64(100000))) // 100 * 1000 for millis
		})

		It("should create new quantity with binary format", func() {
			quantity := NewQuantity(1024, resource.BinarySI)
			Expect(quantity.Value()).To(Equal(int64(1024)))
		})

		It("should create decimal quantity", func() {
			quantity := NewDecimalQuantity(100, resource.DecimalSI)
			Expect(quantity.MilliValue()).To(Equal(int64(100000))) // 100 * 1000 for millis
		})

		It("should create binary quantity", func() {
			quantity := NewBinaryQuantity(1024, resource.BinarySI)
			Expect(quantity.Value()).To(Equal(int64(1024)))
		})

		It("should handle zero values", func() {
			quantity := NewQuantity(0, resource.DecimalSI)
			Expect(quantity.MilliValue()).To(Equal(int64(0)))
		})

		It("should handle negative values", func() {
			quantity := NewQuantity(-100, resource.DecimalSI)
			Expect(quantity.MilliValue()).To(Equal(int64(-100000))) // -100 * 1000 for millis
		})

		It("should handle large values", func() {
			quantity := NewQuantity(1000000, resource.DecimalSI)
			Expect(quantity.MilliValue()).To(Equal(int64(1000000000))) // 1000000 * 1000 for millis
		})
	})

	Describe("UsageResult Edge Cases", func() {
		It("should handle empty namespace", func() {
			result := NewUsageResult("")
			Expect(result.Namespace).To(Equal(""))
			Expect(result.Usage).NotTo(BeNil())
			Expect(result.Errors).NotTo(BeNil())
		})

		It("should handle very long namespace name", func() {
			longNamespace := "very-long-namespace-name-that-exceeds-normal-length-limits-for-testing-purposes"
			result := NewUsageResult(longNamespace)
			Expect(result.Namespace).To(Equal(longNamespace))
		})

		It("should handle special characters in namespace", func() {
			specialNamespace := "namespace-with-special-chars-123_456-789"
			result := NewUsageResult(specialNamespace)
			Expect(result.Namespace).To(Equal(specialNamespace))
		})

		It("should handle multiple errors", func() {
			result := NewUsageResult("test-namespace")
			errors := []error{
				resource.ErrFormatWrong,
				fmt.Errorf("custom error 1"),
				fmt.Errorf("custom error 2"),
			}

			for _, err := range errors {
				result.AddError(err)
			}

			Expect(result.HasErrors()).To(BeTrue())
			Expect(result.Errors).To(HaveLen(3))
			Expect(result.Errors).To(ContainElement(resource.ErrFormatWrong))
			Expect(result.Errors).To(ContainElement(fmt.Errorf("custom error 1")))
			Expect(result.Errors).To(ContainElement(fmt.Errorf("custom error 2")))
		})

		It("should handle nil error", func() {
			result := NewUsageResult("test-namespace")
			result.AddError(nil)

			Expect(result.HasErrors()).To(BeTrue())
			Expect(result.Errors).To(HaveLen(1))
			Expect(result.Errors[0]).ToNot(HaveOccurred())
		})

		It("should handle all resource types", func() {
			result := NewUsageResult("test-namespace")
			resources := []corev1.ResourceName{
				corev1.ResourceCPU,
				corev1.ResourceMemory,
				corev1.ResourceStorage,
				corev1.ResourceEphemeralStorage,
				"hugepages-2Mi",
				"nvidia.com/gpu",
			}

			for i, resourceName := range resources {
				quantity := resource.MustParse(fmt.Sprintf("%dm", 100+i))
				result.AddUsage(resourceName, quantity)
			}

			Expect(result.ResourceCount).To(Equal(len(resources)))
			for i, resourceName := range resources {
				expectedQuantity := resource.MustParse(fmt.Sprintf("%dm", 100+i))
				Expect(result.GetTotalUsage(resourceName)).To(Equal(expectedQuantity))
			}
		})
	})

	Describe("Performance characteristics", func() {
		It("should handle large number of resources efficiently", func() {
			result := NewUsageResult("test-namespace")

			// Add many resources
			for i := 0; i < 1000; i++ {
				resourceName := corev1.ResourceName(fmt.Sprintf("resource-%d", i))
				quantity := resource.MustParse(fmt.Sprintf("%dm", i))
				result.AddUsage(resourceName, quantity)
			}

			Expect(result.ResourceCount).To(Equal(1000))
			Expect(result.Usage).To(HaveLen(1000))

			// Verify some specific values
			Expect(result.GetTotalUsage("resource-0")).To(Equal(resource.MustParse("0m")))
			Expect(result.GetTotalUsage("resource-999")).To(Equal(resource.MustParse("999m")))
		})
	})

	Describe("GetBaseResourceName", func() {
		type testCase struct {
			input    corev1.ResourceName
			expected corev1.ResourceName
		}

		DescribeTable("should correctly handle resource names",
			func(tc testCase) {
				result := GetBaseResourceName(tc.input)
				Expect(result).To(Equal(tc.expected))
			},
			// Basic requests prefix stripping
			Entry("strip requests prefix from cpu", testCase{
				input:    "requests.cpu",
				expected: "cpu",
			}),
			Entry("strip requests prefix from memory", testCase{
				input:    "requests.memory",
				expected: "memory",
			}),
			Entry("strip requests prefix from storage", testCase{
				input:    "requests.storage",
				expected: "storage",
			}),
			Entry("strip requests prefix from ephemeral storage", testCase{
				input:    "requests.ephemeral-storage",
				expected: "ephemeral-storage",
			}),

			// Basic limits prefix stripping
			Entry("strip limits prefix from cpu", testCase{
				input:    "limits.cpu",
				expected: "cpu",
			}),
			Entry("strip limits prefix from memory", testCase{
				input:    "limits.memory",
				expected: "memory",
			}),
			Entry("strip limits prefix from ephemeral storage", testCase{
				input:    "limits.ephemeral-storage",
				expected: "ephemeral-storage",
			}),

			// Resources without prefixes
			Entry("return cpu as-is when no prefix", testCase{
				input:    "cpu",
				expected: "cpu",
			}),
			Entry("return memory as-is when no prefix", testCase{
				input:    "memory",
				expected: "memory",
			}),

			// Edge cases
			Entry("handle empty string", testCase{
				input:    "",
				expected: "",
			}),
			Entry("handle requests prefix only", testCase{
				input:    "requests.",
				expected: "",
			}),
			Entry("handle limits prefix only", testCase{
				input:    "limits.",
				expected: "",
			}),

			// Extended resources
			Entry("strip requests prefix from extended resource", testCase{
				input:    "requests.nvidia.com/gpu",
				expected: "nvidia.com/gpu",
			}),
			Entry("strip limits prefix from extended resource", testCase{
				input:    "limits.nvidia.com/gpu",
				expected: "nvidia.com/gpu",
			}),
			Entry("return extended resource as-is when no prefix", testCase{
				input:    "nvidia.com/gpu",
				expected: "nvidia.com/gpu",
			}),
			Entry("handle extended resource with multiple dots (requests)", testCase{
				input:    "requests.example.com/custom.resource",
				expected: "example.com/custom.resource",
			}),
			Entry("handle extended resource with multiple dots (limits)", testCase{
				input:    "limits.example.com/custom.resource",
				expected: "example.com/custom.resource",
			}),

			// Hugepages
			Entry("handle hugepages with requests prefix", testCase{
				input:    "requests.hugepages-2Mi",
				expected: "hugepages-2Mi",
			}),
			Entry("handle hugepages with limits prefix", testCase{
				input:    "limits.hugepages-1Gi",
				expected: "hugepages-1Gi",
			}),

			// Special cases
			Entry("handle resource with special characters", testCase{
				input:    "requests.custom-resource_v1",
				expected: "custom-resource_v1",
			}),
			Entry("not strip prefix-like patterns in the middle", testCase{
				input:    "cpu.requests.memory",
				expected: "cpu.requests.memory",
			}),
			Entry("handle resource starting with 'requests' but not prefix", testCase{
				input:    "requestscpu",
				expected: "requestscpu",
			}),
			Entry("handle resource starting with 'limits' but not prefix", testCase{
				input:    "limitsmemory",
				expected: "limitsmemory",
			}),

			// Standard Kubernetes resource names
			Entry("handle standard requests.cpu", testCase{
				input:    corev1.ResourceRequestsCPU,
				expected: corev1.ResourceCPU,
			}),
			Entry("handle standard limits.cpu", testCase{
				input:    corev1.ResourceLimitsCPU,
				expected: corev1.ResourceCPU,
			}),
			Entry("handle standard requests.memory", testCase{
				input:    corev1.ResourceRequestsMemory,
				expected: corev1.ResourceMemory,
			}),
			Entry("handle standard limits.memory", testCase{
				input:    corev1.ResourceLimitsMemory,
				expected: corev1.ResourceMemory,
			}),
			Entry("handle standard requests.storage", testCase{
				input:    corev1.ResourceRequestsStorage,
				expected: corev1.ResourceStorage,
			}),
			Entry("handle standard requests.ephemeral-storage", testCase{
				input:    corev1.ResourceRequestsEphemeralStorage,
				expected: corev1.ResourceEphemeralStorage,
			}),
			Entry("handle standard limits.ephemeral-storage", testCase{
				input:    corev1.ResourceLimitsEphemeralStorage,
				expected: corev1.ResourceEphemeralStorage,
			}),
			Entry("handle standard cpu", testCase{
				input:    corev1.ResourceCPU,
				expected: corev1.ResourceCPU,
			}),
			Entry("handle standard memory", testCase{
				input:    corev1.ResourceMemory,
				expected: corev1.ResourceMemory,
			}),
			Entry("handle standard storage", testCase{
				input:    corev1.ResourceStorage,
				expected: corev1.ResourceStorage,
			}),
			Entry("handle standard pods", testCase{
				input:    corev1.ResourcePods,
				expected: corev1.ResourcePods,
			}),
		)
	})
})
