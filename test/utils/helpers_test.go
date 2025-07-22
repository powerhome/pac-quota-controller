package utils

import (
	"testing"

	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/resource"

	quotav1alpha1 "github.com/powerhome/pac-quota-controller/api/v1alpha1"
)

func TestCompareResourceUsage(t *testing.T) {
	tests := []struct {
		name        string
		actual      quotav1alpha1.ResourceList
		expected    map[string]string
		shouldMatch bool
		errorMsg    string
	}{
		{
			name: "exact match",
			actual: quotav1alpha1.ResourceList{
				corev1.ResourceCPU:    resource.MustParse("100m"),
				corev1.ResourceMemory: resource.MustParse("256Mi"),
			},
			expected: map[string]string{
				"cpu":    "100m",
				"memory": "256Mi",
			},
			shouldMatch: true,
		},
		{
			name: "expected zero for non-mentioned resource",
			actual: quotav1alpha1.ResourceList{
				corev1.ResourceCPU:    resource.MustParse("100m"),
				corev1.ResourceMemory: resource.MustParse("0"),
			},
			expected: map[string]string{
				"cpu": "100m",
			},
			shouldMatch: true,
		},
		{
			name: "non-mentioned resource is not zero",
			actual: quotav1alpha1.ResourceList{
				corev1.ResourceCPU:    resource.MustParse("100m"),
				corev1.ResourceMemory: resource.MustParse("256Mi"),
			},
			expected: map[string]string{
				"cpu": "100m",
			},
			shouldMatch: false,
			errorMsg:    "resource memory: expected 0 (not mentioned), got 256Mi",
		},
		{
			name: "value mismatch",
			actual: quotav1alpha1.ResourceList{
				corev1.ResourceCPU: resource.MustParse("200m"),
			},
			expected: map[string]string{
				"cpu": "100m",
			},
			shouldMatch: false,
			errorMsg:    "resource cpu: expected 100m, got 200m",
		},
		{
			name: "expected resource not found",
			actual: quotav1alpha1.ResourceList{
				corev1.ResourceMemory: resource.MustParse("256Mi"),
			},
			expected: map[string]string{
				"cpu": "100m",
			},
			shouldMatch: false,
			errorMsg:    "expected resource cpu not found in actual usage",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			match, err := CompareResourceUsage(tt.actual, tt.expected)
			if match != tt.shouldMatch {
				t.Errorf("CompareResourceUsage() match = %v, want %v", match, tt.shouldMatch)
			}
			if !tt.shouldMatch {
				if err == nil {
					t.Errorf("CompareResourceUsage() expected error but got nil")
				} else if err.Error() != tt.errorMsg {
					t.Errorf("CompareResourceUsage() error message = %v, want %v", err.Error(), tt.errorMsg)
				}
			} else if err != nil {
				t.Errorf("CompareResourceUsage() unexpected error: %v", err)
			}
		})
	}
}

func TestExpectCRQUsageToMatch(t *testing.T) {
	tests := []struct {
		name        string
		actual      quotav1alpha1.ResourceList
		expected    map[string]string
		shouldError bool
		errorMsg    string
	}{
		{
			name: "exact match - no error",
			actual: quotav1alpha1.ResourceList{
				corev1.ResourceCPU:    resource.MustParse("100m"),
				corev1.ResourceMemory: resource.MustParse("256Mi"),
			},
			expected: map[string]string{
				"cpu":    "100m",
				"memory": "256Mi",
			},
			shouldError: false,
		},
		{
			name: "value mismatch - should error",
			actual: quotav1alpha1.ResourceList{
				corev1.ResourceCPU: resource.MustParse("200m"),
			},
			expected: map[string]string{
				"cpu": "100m",
			},
			shouldError: true,
			errorMsg:    "CRQ usage assertion failed: resource cpu: expected 100m, got 200m",
		},
		{
			name: "expected resource not found - should error",
			actual: quotav1alpha1.ResourceList{
				corev1.ResourceMemory: resource.MustParse("256Mi"),
			},
			expected: map[string]string{
				"cpu": "100m",
			},
			shouldError: true,
			errorMsg:    "CRQ usage assertion failed: expected resource cpu not found in actual usage",
		},
		{
			name: "non-mentioned resource is not zero - should error",
			actual: quotav1alpha1.ResourceList{
				corev1.ResourceCPU:    resource.MustParse("100m"),
				corev1.ResourceMemory: resource.MustParse("256Mi"),
			},
			expected: map[string]string{
				"cpu": "100m",
			},
			shouldError: true,
			errorMsg:    "CRQ usage assertion failed: resource memory: expected 0 (not mentioned), got 256Mi",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := ExpectCRQUsageToMatch(tt.actual, tt.expected)
			if tt.shouldError {
				if err == nil {
					t.Errorf("ExpectCRQUsageToMatch() expected error but got nil")
				} else if err.Error() != tt.errorMsg {
					t.Errorf("ExpectCRQUsageToMatch() error message = %v, want %v", err.Error(), tt.errorMsg)
				}
			} else if err != nil {
				t.Errorf("ExpectCRQUsageToMatch() unexpected error: %v", err)
			}
		})
	}
}
