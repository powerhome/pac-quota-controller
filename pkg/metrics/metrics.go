package metrics

import (
	"sync"

	"github.com/prometheus/client_golang/prometheus"
	crmetrics "sigs.k8s.io/controller-runtime/pkg/metrics"
)

const (
	labelCRQName   = "crq_name"
	labelOperation = "operation"
	labelNamespace = "namespace"
	labelWebhook   = "webhook"
	labelResource  = "resource"
)

var (
	CRQUsage = prometheus.NewGaugeVec(
		prometheus.GaugeOpts{
			Name: "pac_quota_controller_crq_usage",
			Help: "Current usage of a resource for a ClusterResourceQuota in a namespace.",
		},
		[]string{labelCRQName, labelNamespace, labelResource},
	)
	CRQTotalUsage = prometheus.NewGaugeVec(
		prometheus.GaugeOpts{
			Name: "pac_quota_controller_crq_total_usage",
			Help: "Aggregated usage of a resource across all namespaces for a ClusterResourceQuota.",
		},
		// The per-namespace breakdown lives on CRQUsage; this metric is a single
		// total per (crq, resource). Earlier shapes included a comma-joined
		// `namespaces` label, which churned a new series on every namespace
		// add/remove and was an unbounded-cardinality bomb at scale.
		[]string{labelCRQName, labelResource},
	)
	// CRQHard and CRQUsed expose the absolute quantities behind CRQTotalUsage's
	// ratio. Same (crq_name, resource) cardinality as CRQTotalUsage.
	CRQHard = prometheus.NewGaugeVec(
		prometheus.GaugeOpts{
			Name: "pac_quota_controller_crq_hard",
			Help: "Enforced hard limit of a resource for a ClusterResourceQuota.",
		},
		[]string{labelCRQName, labelResource},
	)
	CRQUsed = prometheus.NewGaugeVec(
		prometheus.GaugeOpts{
			Name: "pac_quota_controller_crq_used",
			Help: "Aggregated absolute usage of a resource across all namespaces for a ClusterResourceQuota.",
		},
		[]string{labelCRQName, labelResource},
	)
	// CRQReportOnly is 1 when a CRQ's enforcement mode is ReportOnly, 0 otherwise. Exists
	// so the QuotaBreached alert can exclude quotas that are expected to run over limit.
	CRQReportOnly = prometheus.NewGaugeVec(
		prometheus.GaugeOpts{
			Name: "pac_quota_controller_crq_report_only",
			Help: "1 if the ClusterResourceQuota's enforcement mode is ReportOnly, 0 otherwise.",
		},
		[]string{labelCRQName},
	)
	WebhookValidationCount = prometheus.NewCounterVec(
		prometheus.CounterOpts{
			Name: "pac_quota_controller_webhook_validation_total",
			Help: "Total number of webhook validation requests.",
		},
		[]string{labelWebhook, labelOperation},
	)
	WebhookValidationDuration = prometheus.NewHistogramVec(
		prometheus.HistogramOpts{
			Name: "pac_quota_controller_webhook_validation_duration_seconds",
			Help: "Duration of webhook validation requests.",
			// Admission webhooks operate sub-millisecond on cache hits; the default
			// Prometheus buckets bottom out at 5ms and lose all signal in the hot path.
			Buckets: []float64{
				0.0001, 0.0005, 0.001, 0.002, 0.005,
				0.01, 0.025, 0.05, 0.1, 0.25, 0.5, 1,
			},
		},
		[]string{labelWebhook, labelOperation},
	)
	WebhookAdmissionDecision = prometheus.NewCounterVec(
		prometheus.CounterOpts{
			Name: "pac_quota_controller_webhook_admission_decision_total",
			Help: "Total number of webhook admission decisions (allowed/denied).",
		},
		[]string{labelWebhook, labelOperation, "decision"},
	)
	// WebhookAdmissionDenied breaks down denials by reason so operators can
	// distinguish working-as-intended quota_exceeded from broken-config
	// signals (bad_request, gvk_mismatch, missing_namespace).
	WebhookAdmissionDenied = prometheus.NewCounterVec(
		prometheus.CounterOpts{
			Name: "pac_quota_controller_webhook_admission_denied_total",
			Help: "Webhook admissions denied, broken down by reason.",
		},
		[]string{labelWebhook, "reason"},
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
		[]string{labelCRQName, labelResource},
	)
	// WebhookQuotaViolationAdmitted counts admissions allowed despite exceeding quota
	// because the CRQ's enforcement mode is ReportOnly.
	WebhookQuotaViolationAdmitted = prometheus.NewCounterVec(
		prometheus.CounterOpts{
			Name: "pac_quota_controller_webhook_quota_violation_admitted_total",
			Help: "Admissions allowed despite exceeding quota because the CRQ enforcement mode is ReportOnly.",
		},
		[]string{labelCRQName, labelResource},
	)

	// New metrics for controller reconciliation
	QuotaReconcileTotal = prometheus.NewCounterVec(
		prometheus.CounterOpts{
			Name: "pac_quota_controller_reconcile_total",
			Help: "Total number of ClusterResourceQuota reconciliations.",
		},
		[]string{labelCRQName, "status"},
	)
	QuotaReconcileErrors = prometheus.NewCounterVec(
		prometheus.CounterOpts{
			Name: "pac_quota_controller_reconcile_errors_total",
			Help: "Total number of reconciliation errors per ClusterResourceQuota.",
		},
		[]string{labelCRQName},
	)
	QuotaAggregationDuration = prometheus.NewHistogramVec(
		prometheus.HistogramOpts{
			Name: "pac_quota_controller_aggregation_duration_seconds",
			Help: "Time taken to aggregate resource usage across namespaces.",
		},
		[]string{labelCRQName},
	)
	QuotaAggregationStepDuration = prometheus.NewHistogramVec(
		prometheus.HistogramOpts{
			Name: "pac_quota_controller_aggregation_step_duration_seconds",
			Help: "Time taken by each resource aggregation step.",
		},
		[]string{labelCRQName, "step"},
	)
	// QuotaUnsupportedResource counts attempts to aggregate a resource the
	// controller has no calculator for (typo in CRQ spec or an unsupported
	// resource kind). Each hit reports usage=0 and admits requests; the
	// counter is the operator-visible signal that quota is silently passing.
	QuotaUnsupportedResource = prometheus.NewCounterVec(
		prometheus.CounterOpts{
			Name: "pac_quota_controller_unsupported_resource_total",
			Help: "Number of reconcile attempts that encountered a CRQ resource with no calculator.",
		},
		[]string{labelResource},
	)
	// EventsCleanedTotal counts events deleted by the cleanup loop.
	// Going to zero is the signal that cleanup itself has regressed (RBAC, query bug, etc.).
	EventsCleanedTotal = prometheus.NewCounter(
		prometheus.CounterOpts{
			Name: "pac_quota_controller_events_cleaned_total",
			Help: "PAC quota events deleted by the cleanup loop.",
		},
	)

	// Use controller-runtime's global registry
	registerOnce sync.Once
)

func RegisterWebhookMetrics() {
	registerOnce.Do(func() {
		crmetrics.Registry.MustRegister(
			CRQUsage,
			CRQTotalUsage,
			CRQHard,
			CRQUsed,
			CRQReportOnly,
			WebhookValidationCount,
			WebhookValidationDuration,
			WebhookAdmissionDecision,
			WebhookAdmissionDenied,
			WebhookCRQLookup,
			WebhookStatusMissing,
			WebhookQuotaViolationAdmitted,
			QuotaReconcileTotal,
			QuotaReconcileErrors,
			QuotaAggregationDuration,
			QuotaAggregationStepDuration,
			QuotaUnsupportedResource,
			EventsCleanedTotal,
		)
	})
}

// DeleteCRQMetrics drops every series for a deleted CRQ across all per-CRQ gauges.
// Without this, a gauge's last value keeps being reported forever, since the
// Prometheus client library never expires a label combination on its own.
func DeleteCRQMetrics(crqName string) {
	labels := prometheus.Labels{labelCRQName: crqName}
	CRQUsage.DeletePartialMatch(labels)
	CRQTotalUsage.DeletePartialMatch(labels)
	CRQHard.DeletePartialMatch(labels)
	CRQUsed.DeletePartialMatch(labels)
	CRQReportOnly.DeletePartialMatch(labels)
}
