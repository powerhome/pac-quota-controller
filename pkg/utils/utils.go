package utils

import (
	"sort"
)

// DeduplicateAndSort removes duplicates and sorts a string slice.
// Returns a new slice with unique strings sorted alphabetically.
func DeduplicateAndSort(input []string) []string {
	// Remove duplicates
	seen := make(map[string]struct{})
	var result []string

	for _, item := range input {
		if _, exists := seen[item]; !exists {
			seen[item] = struct{}{}
			result = append(result, item)
		}
	}

	// Sort for consistent results
	sort.Strings(result)
	return result
}

// equalStringMap compares two string maps for equality.
func EqualStringMap(a, b map[string]string) bool {
	if len(a) != len(b) {
		return false
	}
	for k, v := range a {
		if b[k] != v {
			return false
		}
	}
	return true
}
