package utils

import (
	"testing"

	"github.com/stretchr/testify/assert"
)

func TestDeduplicateAndSort(t *testing.T) {
	tests := []struct {
		name     string
		input    []string
		expected []string
	}{
		{
			name:     "Empty slice",
			input:    []string{},
			expected: nil, // DeduplicateAndSort returns nil for empty input
		},
		{
			name:     "No duplicates",
			input:    []string{"c", "b", "a"},
			expected: []string{"a", "b", "c"},
		},
		{
			name:     "With duplicates",
			input:    []string{"b", "a", "b", "c", "a"},
			expected: []string{"a", "b", "c"},
		},
		{
			name:     "Already sorted, with duplicates",
			input:    []string{"a", "a", "b", "c", "c"},
			expected: []string{"a", "b", "c"},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			// Execute
			result := DeduplicateAndSort(tt.input)

			// Assert
			assert.Equal(t, tt.expected, result)
		})
	}
}
