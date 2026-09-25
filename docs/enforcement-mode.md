# Enforcement Mode in pac-quota-controller

## Overview

`ClusterResourceQuota` supports an `enforcementMode` field that controls what the
admission webhook does with a request that would push the quota's aggregate usage over
its `hard` limit:

| Mode | Behavior |
|---|---|
| `Blocking` (default) | Denies the request. This is today's behavior. |
| `ReportOnly` | Admits the request. The violation is still recorded via a `QuotaExceeded` event, `status.total.used` exceeding `status.total.hard`, and Prometheus metrics. |

A CRQ with `enforcementMode` unset behaves exactly as before this field existed.

## How it Works

- **Webhook:** the admission check (`validateCRQStatusUsage`) is unchanged except for one
  branch — when usage would exceed the limit and the CRQ is `ReportOnly`, it returns
  success instead of denying.
- **Controller:** the reconciler's usage aggregation, status update, and event recording
  are mode-agnostic. They already treat "observed usage above hard" as a fact to report,
  regardless of why it happened — so a `ReportOnly` CRQ running over quota is reported
  through the exact same path a `Blocking` CRQ uses for a genuine race condition today.
- **Metrics:** `pac_quota_controller_webhook_quota_violation_admitted_total` counts each
  admission let through because of `ReportOnly`. `pac_quota_controller_crq_report_only`
  exposes the current mode (`1`/`0`) per CRQ so the `QuotaBreached` and
  `CrqResourcePressure` alerts can exclude quotas that are expected to run over limit.
- **Changing mode:** `enforcementMode` is a plain spec field — edit it and the next
  admission request picks it up immediately. No controller restart or CRQ recreation
  needed.

## Usage in ClusterResourceQuota

```yaml
apiVersion: quota.powerapp.cloud/v1alpha1
kind: ClusterResourceQuota
metadata:
  name: team-web-quota
spec:
  namespaceSelector:
    matchLabels:
      team: web
  enforcementMode: ReportOnly
  hard:
    pods: "20"
    requests.cpu: "10"
```

## Testing

```sh
# E2E tests
make test-e2e  # includes test/e2e/enforcement_mode_test.go
```
