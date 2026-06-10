package metrics

import (
	"sync"

	"github.com/prometheus/client_golang/prometheus"
	crmetrics "sigs.k8s.io/controller-runtime/pkg/metrics"
)

var (
	CRQUsage = prometheus.NewGaugeVec(
		prometheus.GaugeOpts{
			Name: "pac_quota_controller_crq_usage",
			Help: "Current usage of a resource for a ClusterResourceQuota in a namespace.",
		},
		[]string{"crq_name", "namespace", "resource"},
	)
	CRQTotalUsage = prometheus.NewGaugeVec(
		prometheus.GaugeOpts{
			Name: "pac_quota_controller_crq_total_usage",
			Help: "Aggregated usage of a resource across all namespaces for a ClusterResourceQuota.",
		},
		[]string{"crq_name", "resource", "namespace", "namespaces"},
	)
	WebhookValidationCount = prometheus.NewCounterVec(
		prometheus.CounterOpts{
			Name: "pac_quota_controller_webhook_validation_total",
			Help: "Total number of webhook validation requests.",
		},
		[]string{"webhook", "operation", "namespace"},
	)
	WebhookValidationDuration = prometheus.NewHistogramVec(
		prometheus.HistogramOpts{
			Name: "pac_quota_controller_webhook_validation_duration_seconds",
			Help: "Duration of webhook validation requests.",
		},
		[]string{"webhook", "operation", "namespace"},
	)
	WebhookAdmissionDecision = prometheus.NewCounterVec(
		prometheus.CounterOpts{
			Name: "pac_quota_controller_webhook_admission_decision_total",
			Help: "Total number of webhook admission decisions (allowed/denied).",
		},
		[]string{"webhook", "operation", "decision", "namespace"},
	)
	// WebhookCRQLookup counts CRQ resolution outcomes during admission.
	// Result values: found, not_found, namespace_error, crq_error, no_client.
	WebhookCRQLookup = prometheus.NewCounterVec(
		prometheus.CounterOpts{
			Name: "pac_quota_controller_webhook_crq_lookup_total",
			Help: "Outcome of CRQ resolution attempts during webhook admission.",
		},
		[]string{"result"},
	)
	// WebhookStatusMissing counts admissions allowed because the CRQ status had
	// no usage recorded yet for the requested resource (cold start / new key).
	WebhookStatusMissing = prometheus.NewCounterVec(
		prometheus.CounterOpts{
			Name: "pac_quota_controller_webhook_status_missing_total",
			Help: "Number of webhook admissions admitted because the CRQ status had no usage value for the resource.",
		},
		[]string{"crq_name", "resource"},
	)

	// New metrics for controller reconciliation
	QuotaReconcileTotal = prometheus.NewCounterVec(
		prometheus.CounterOpts{
			Name: "pac_quota_controller_reconcile_total",
			Help: "Total number of ClusterResourceQuota reconciliations.",
		},
		[]string{"crq_name", "status"},
	)
	QuotaReconcileErrors = prometheus.NewCounterVec(
		prometheus.CounterOpts{
			Name: "pac_quota_controller_reconcile_errors_total",
			Help: "Total number of reconciliation errors per ClusterResourceQuota.",
		},
		[]string{"crq_name"},
	)
	QuotaAggregationDuration = prometheus.NewHistogramVec(
		prometheus.HistogramOpts{
			Name: "pac_quota_controller_aggregation_duration_seconds",
			Help: "Time taken to aggregate resource usage across namespaces.",
		},
		[]string{"crq_name"},
	)
	QuotaAggregationStepDuration = prometheus.NewHistogramVec(
		prometheus.HistogramOpts{
			Name: "pac_quota_controller_aggregation_step_duration_seconds",
			Help: "Time taken by each resource aggregation step.",
		},
		[]string{"crq_name", "step"},
	)

	// Use controller-runtime's global registry
	registerOnce sync.Once
)

func RegisterWebhookMetrics() {
	registerOnce.Do(func() {
		crmetrics.Registry.MustRegister(
			CRQUsage,
			CRQTotalUsage,
			WebhookValidationCount,
			WebhookValidationDuration,
			WebhookAdmissionDecision,
			WebhookCRQLookup,
			WebhookStatusMissing,
			QuotaReconcileTotal,
			QuotaReconcileErrors,
			QuotaAggregationDuration,
			QuotaAggregationStepDuration,
		)
	})
}
