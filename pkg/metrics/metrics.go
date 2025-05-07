package metrics

import (
	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/client_golang/prometheus/promauto"
)

var (
	// ValidationRequestsTotal counts the total number of validation requests
	ValidationRequestsTotal = promauto.NewCounterVec(
		prometheus.CounterOpts{
			Name: "validation_requests_total",
			Help: "Total number of validation requests",
		},
		[]string{"resource_type", "result"},
	)

	// ValidationRequestDuration measures the duration of validation requests
	ValidationRequestDuration = promauto.NewHistogramVec(
		prometheus.HistogramOpts{
			Name:    "validation_request_duration_seconds",
			Help:    "Duration of validation requests in seconds",
			Buckets: prometheus.DefBuckets,
		},
		[]string{"resource_type"},
	)

	// ValidationErrorsTotal counts the total number of validation errors
	ValidationErrorsTotal = promauto.NewCounterVec(
		prometheus.CounterOpts{
			Name: "validation_errors_total",
			Help: "Total number of validation errors",
		},
		[]string{"resource_type", "error_type"},
	)

	// WebhookRequestDuration tracks the duration of webhook requests
	WebhookRequestDuration = promauto.NewHistogramVec(
		prometheus.HistogramOpts{
			Name:    "webhook_request_duration_seconds",
			Help:    "Duration of webhook requests in seconds",
			Buckets: prometheus.DefBuckets,
		},
		[]string{"resource_type"},
	)
)

// RecordValidationRequest records a validation request
func RecordValidationRequest(resourceType string, result string, duration float64) {
	ValidationRequestsTotal.WithLabelValues(resourceType, result).Inc()
	ValidationRequestDuration.WithLabelValues(resourceType).Observe(duration)
}

// RecordValidationError records a validation error
func RecordValidationError(resourceType string, errorType string) {
	ValidationErrorsTotal.WithLabelValues(resourceType, errorType).Inc()
}
