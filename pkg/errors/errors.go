package errors

import (
	"fmt"
	"sync"

	"go.uber.org/zap"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

// This is all placeholder ruberish
// Drink some coffee now, nothing to see here (yet).

// ErrorType represents the type of error
type ErrorType string

const (
	// ValidationError represents validation failures
	ValidationError ErrorType = "validation_error"
	// AdmissionReviewDecodeError represent an error when decoding an AdmissionReview request
	AdmissionReviewDecodeError = "admission_review_decode_error"
	// InternalError represents internal server errors
	InternalError ErrorType = "internal_error"
	// ConfigurationError represents configuration-related errors
	ConfigurationError ErrorType = "configuration_error"
)

// CustomError represents a structured error
type CustomError struct {
	Type    ErrorType
	Message string
	Err     error
	Fields  []zap.Field
}

// Error implements the error interface
func (e *CustomError) Error() string {
	if e.Err != nil {
		return fmt.Sprintf("%s: %s: %v", e.Type, e.Message, e.Err)
	}
	return fmt.Sprintf("%s: %s", e.Type, e.Message)
}

// Unwrap returns the underlying error
func (e *CustomError) Unwrap() error {
	return e.Err
}

// NewValidationError creates a new validation error
func NewValidationError(message string, err error, fields ...zap.Field) *CustomError {
	return &CustomError{
		Type:    ValidationError,
		Message: message,
		Err:     err,
		Fields:  fields,
	}
}

// NewInternalError creates a new internal error
func NewInternalError(message string, err error, fields ...zap.Field) *CustomError {
	return &CustomError{
		Type:    InternalError,
		Message: message,
		Err:     err,
		Fields:  fields,
	}
}

// NewConfigurationError creates a new configuration error
func NewConfigurationError(message string, err error, fields ...zap.Field) *CustomError {
	return &CustomError{
		Type:    ConfigurationError,
		Message: message,
		Err:     err,
		Fields:  fields,
	}
}

// AdmissionError represents an error in the admission process
type AdmissionError struct {
	metav1.Status
}

// NewAdmissionError creates a new admission error
func NewAdmissionError(message string) *AdmissionError {
	return &AdmissionError{
		Status: metav1.Status{
			Status:  "Failure",
			Message: message,
			Reason:  "Forbidden",
			Code:    403,
		},
	}
}

var (
	errorCounts = make(map[string]int)
	mutex       sync.Mutex
)

// IncrementErrorCount increments the count for a specific error type
func IncrementErrorCount(errorType string) {
	mutex.Lock()
	defer mutex.Unlock()
	errorCounts[errorType]++
}

// GetErrorCount retrieves the count for a specific error type
func GetErrorCount(errorType string) int {
	mutex.Lock()
	defer mutex.Unlock()

	return errorCounts[errorType]
}
