
{{/*
Returns a deduplicated YAML list of excluded namespaces (for use in values blocks)
*/}}
{{- define "pacQuota.excludedNamespacesList" -}}
  {{- $default := list "kube-system" .Release.Namespace }}
  {{- $user := .Values.excludedNamespaces | default (list) }}
  {{- $all := concat $default $user }}
  {{- $deduped := uniq $all }}
  {{- join " " $deduped -}}
{{- end }}

{{/*
Returns a deduplicated, comma-separated string of excluded namespaces (for CLI args)
*/}}
{{- define "pacQuota.excludedNamespacesString" -}}
  {{- $default := list "kube-system" .Release.Namespace }}
  {{- $user := .Values.excludedNamespaces | default (list) }}
  {{- $all := concat $default $user }}
  {{- $deduped := uniq $all }}
  {{- join "," $deduped -}}
{{- end }}

{{- define "chart.name" -}}
{{- if .Chart }}
  {{- if .Chart.Name }}
    {{- .Chart.Name | trunc 63 | trimSuffix "-" }}
  {{- else if .Values.nameOverride }}
    {{ .Values.nameOverride | trunc 63 | trimSuffix "-" }}
  {{- else }}
    pac-quota-controller
  {{- end }}
{{- else }}
  pac-quota-controller
{{- end }}
{{- end }}


{{- define "chart.labels" -}}
{{- if .Chart.AppVersion -}}
app.kubernetes.io/version: {{ .Chart.AppVersion | quote }}
{{- end }}
{{- if .Chart.Version }}
helm.sh/chart: {{ .Chart.Version | quote }}
{{- end }}
app.kubernetes.io/name: {{ include "chart.name" . }}
app.kubernetes.io/instance: {{ .Release.Name }}
app.kubernetes.io/managed-by: {{ .Release.Service }}
{{- end }}


{{- define "chart.selectorLabels" -}}
app.kubernetes.io/name: {{ include "chart.name" . }}
app.kubernetes.io/instance: {{ .Release.Name }}
{{- end }}


{{- define "chart.hasMutatingWebhooks" -}}
{{- $hasMutating := false }}
{{- range . }}
  {{- if eq .type "mutating" }}
    $hasMutating = true }}{{- end }}
{{- end }}
{{ $hasMutating }}}}{{- end }}


{{- define "chart.hasValidatingWebhooks" -}}
{{- $hasValidating := false }}
{{- range . }}
  {{- if eq .type "validating" }}
    $hasValidating = true }}{{- end }}
{{- end }}
{{ $hasValidating }}}}{{- end }}


{{/*
CRQ usage ratio above .threshold, excluding ReportOnly CRQs. With prometheus.alerting.ownerNamespace.replacement
set, the alert's namespace label is label_replace of crq_name with ownerNamespace.regex.
*/}}
{{- define "chart.crqUsageAlertExpr" -}}
{{- $owner := .root.Values.prometheus.alerting.ownerNamespace -}}
{{ if $owner.replacement }}label_replace(
{{ end -}}
# max by drops the pod label, so a controller rollout doesn't reset `for`; hard=0 is skipped.
(
  max by (crq_name, resource) (pac_quota_controller_crq_used)
  / (max by (crq_name, resource) (pac_quota_controller_crq_hard) > 0)
) > {{ .threshold }}
and on(crq_name) max by (crq_name) (pac_quota_controller_crq_report_only) == 0
{{- if $owner.replacement }}
, "namespace", {{ $owner.replacement | quote }}, "crq_name", {{ $owner.regex | quote }})
{{- end }}
{{- end }}
