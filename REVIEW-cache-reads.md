# Senior Review — `refactor/controller-cache-reads`

Working document. Check items off as they land.

## Context

The branch migrates every read path in the controller and webhook packages
from a typed client-go `kubernetes.Interface` to controller-runtime's
`sigs.k8s.io/controller-runtime/pkg/client.Client`, so all reads now come from
the manager's shared informer cache instead of round-tripping to the
kube-apiserver. The diff also extracts `resolveCRQForNamespace` in the
webhook, moves a handful of free functions from the controller into their
owning packages (`storage`, `pod`, `services`), and drops the now-unused
`KubeClient` field and the duplicated clientset build in `pkg/manager`.

The refactor itself is correct and well-scoped. The items below are what a
senior reviewer would push back on: dead branches the refactor exposed,
a couple of correctness footguns, and cheap performance / readability wins.

### Note on prefetch

`prefetchNamespaceResources` was written before the move to informer-cached
reads, when each List was a real apiserver round-trip. With the cache, every
`r.List(...)` is a memory read, but controller-runtime returns a **deep-copied**
slice on each call. So removing prefetch outright would multiply deep-copy
work by the number of resources of the same kind tracked in the CRQ (e.g. 5×
for `requests.cpu` + `requests.memory` + `limits.cpu` + `limits.memory` +
`pods`).

The chosen approach is to **invert the loops** in `calculateAndAggregateUsage`
— outer loop becomes namespaces, inner loop reads pods/svcs/pvcs once on
demand, then iterates resources off the in-memory slices. This:

- Deletes `namespaceResourceSnapshot`, `prefetchNamespaceResources`,
  `shouldPrefetchNamespaceResources`.
- Deletes every `if hasSnapshot { ... } else { calc... }` dual branch in
  `resolveNamespaceResourceUsage`.
- Preserves the per-kind deep-copy amortisation prefetch was buying.
- Absorbs the dead `else` branch (§A.1), the missing empty-namespace guard
  (§A.2), and the storage-class-aggregation inefficiency (§B.1) into one
  restructure.

---

## A. Bugfixes (independent of the restructure)

- [x] **A.3** `objectcount.go:86–87` + `controller.go:599–605` — unsupported
      object-count resources silently returned `0`. Added
      `pac_quota_controller_unsupported_resource_total{resource}` counter and a
      Warn log in `calculateObjectCount`'s default branch. Test:
      `internal/controller/clusterresourcequota_controller_test.go` —
      `Describe("calculateObjectCount with unsupported resource")`.
- [x] **A.4** `controller.go:835–842` — extracted the goroutine body into
      `runViolationCleanupLoop(ctx, interval)` with a ctx-aware select so it
      exits on manager shutdown. Tests:
      `internal/controller/clusterresourcequota_controller_test.go` —
      `Describe("runViolationCleanupLoop")`.
- [x] **A.5** `webhook_handler.go:250–253` — `resolveCRQForNamespace` now
      emits a Warn log on every nil-client hit (the startup-time Warn at
      `server.go:152` covers boot visibility; this surfaces "still
      misconfigured" at runtime). Test:
      `pkg/webhook/v1alpha1/webhook_handler_test.go` — "emits a Warn log on
      every nil-client hit".

(Originally §2.1 and §2.2 — dead `else` branch and empty-namespace guard —
are dropped here; they disappear under §B.1.)

---

## B. Structural refactor

- [ ] **B.1** `calculateAndAggregateUsage` (`controller.go:301–449`) — invert
      loops. New shape:

      ```go
      kinds := classifyKinds(crq.Spec.Hard)        // pods? svcs? pvcs?
      for _, nsName := range namespaces {
          if nsName == "" { continue }
          pods, svcs, pvcs := r.fetchNamespaceLists(ctx, nsName, kinds)
          pvcsByClass := bucketPVCsByStorageClass(pvcs) // §B.2
          for resourceName := range crq.Spec.Hard {
              used := computeUsage(resourceName, pods, svcs, pvcs, pvcsByClass)
              // accumulate into totalUsage + usageByNamespace
          }
      }
      ```

      Removes: `namespaceResourceSnapshot`, `prefetchNamespaceResources`,
      `shouldPrefetchNamespaceResources`, the `if hasSnapshot` dual branches,
      the three dead `else` aggregation branches, and the empty-namespace
      guard inconsistency.

- [ ] **B.2** Storage-class aggregation is currently
      `O(classes × namespaces × PVCs)`. Bucket PVCs by storage class once per
      namespace (single pass over `pvcs`), then look up by class in O(1).
      Cuts the storage hot path to `O(N·P + C·N)`. Falls out of §B.1.

- [ ] **B.3** `storage.go:65–80` — `CalculateUsage` lists PVCs twice in two
      switch cases. After §B.1 the controller doesn't call this for the
      reconcile path. Keep it for the webhook / external callers but
      collapse to a single List + projection switch.

- [ ] **B.4** `SetupWithManager` (`controller.go:767–884`, 117 LOC, mixed
      concerns) — split into:
      - `r.ensureCalculators(mgr)` — DI wiring.
      - `r.startEventCleanup(ctx, mgr)` — config load + background goroutines
        (with §A.4 fix wired in here).
      - `r.installWatches(mgr)` — the `Watches(...)` chain.

---

## C. Dead-code / cleanup

- [ ] **C.1** `controller.go:609–657` — `calculateComputeResources`,
      `calculateStorageResources`, `calculateServiceResources` each wrap a
      single calculator call with the same log block. After §B.1 most call
      sites disappear; collapse the remainder.
- [ ] **C.2** `usage.go:18–22` — `BaseResourceCalculator` /
      `NewBaseResourceCalculator` look orphaned post-refactor.
      `grep -R BaseResourceCalculator pkg/ internal/ cmd/` to confirm, then
      delete.
- [ ] **C.3** `webhook_handler.go:158–171` — `validateAgainstCRQ` is now a
      2-line wrapper. `grep` usage; delete if no in-tree callers.
- [ ] **C.4** `pvc_webhook.go:128–133` — the `// Potentially dead code`
      comment is editorial. Keep the `Sign() <= 0` guard but replace the
      comment with a one-liner stating why we keep it (defensive against
      test-only injection of shrink requests).
- [ ] **C.5** `webhook_handler.go` — add a `logValidationPassed(logger, kind,
      name, namespace)` helper; replace the three near-identical Debug logs
      in `pod_webhook.go:127`, `pvc_webhook.go:139`, `service_webhook.go`.

---

## D. Manager wiring

- [ ] **D.1** `pkg/manager/manager.go` no longer builds any calculator. The
      lazy `if r.X == nil { r.X = ... }` blocks in `SetupWithManager`
      (controller.go:773–797) cover the production wiring. Either:
      (a) drop the nil checks in `SetupWithManager` and require
      `manager.go` to wire all four calculators explicitly, or
      (b) drop `manager.go`'s calculator-related code entirely and let
      `SetupWithManager` be the single source of truth.
      Pick (b) — it's already 99% there.
- [ ] **D.2** After §D.1, the `Error`-level "Calculator is nil" logs in
      `controller.go:625, 645, 363, 396` are unreachable. Remove them along
      with the nil checks.

---

## E. Out of scope for this PR (follow-ups)

- `quota.go:46–101` — `GetCRQByNamespace` lists all CRQs and runs a
  selector match per admission. Index CRQs by selector signature or maintain
  a `sync.Map[namespaceName]crqName` warmed by a CRQ watch. File as
  follow-up issue; do not attempt here.

---

## F. Verification

- `go vet ./... && go test ./internal/controller/... ./pkg/webhook/... ./pkg/kubernetes/...`
  — existing tests cover both the snapshot and calculator fallback paths;
  after §B.1 those collapse to one path and existing tests should still pass.
- `make test-e2e` (if present) for a real reconcile end-to-end.
- Manually `grep -R BaseResourceCalculator pkg/ internal/ cmd/` before
  deleting (§C.2).
- Spot-check Prometheus output with a CRQ tracking a typo'd resource
  (e.g. `congigmaps`) — must show non-zero
  `crq_unsupported_resource_total` and a Warn log (§A.3).

---

## Function-complexity audit (reference)

### `internal/controller/clusterresourcequota_controller.go`

| Function | LOC | CC | Nesting | Verdict |
|---|---|---|---|---|
| `Reconcile` (168–298) | 130 | ~6 | 3 | Long but linear; acceptable. |
| `calculateAndAggregateUsage` (301–449) | 148 | ~10 | 3–4 | **Refactor — §B.1.** |
| `resolveNamespaceResourceUsage` (451–494) | 43 | 6 | 2 | Disappears under §B.1. |
| `prefetchNamespaceResources` (528–561) | 33 | 2 | 1 | Disappears under §B.1. |
| `shouldPrefetchNamespaceResources` (496–520) | 24 | 4 | 2 | Disappears under §B.1. |
| `aggregationStepForResource` (563–581) | 18 | 4 | 1 | Trivial. |
| `calculateObjectCount` (584–606) | 22 | 3 | 1 | Trivial. |
| `calculate{Compute,Storage,Service}Resources` (609–657) | ~16 each | 2 | 1 | Collapsible — §C.1. |
| `containerTerminated` (105–128) | 23 | 4 | 2 | OK. |
| `findQuotasForObject` (684–738) | 54 | 4 | 2 | OK. |
| `SetupWithManager` (767–884) | 117 | ~6 | 2 | **Split — §B.4.** |
| `updateStatus` (660–677) | 17 | 2 | 1 | Clean. |
| `isComputeResource` (742–764) | 22 | 3 | 1 | Clean. |
| `resourceUpdatePredicate.Update` (55–86) | 31 | 5 | 3 | OK. |

### `pkg/webhook/v1alpha1/*.go`

| Function | LOC | CC | Verdict |
|---|---|---|---|
| `runWebhook` (handler:53–129) | 76 | 5 | Well-factored. |
| `validateAgainstCRQ` (handler:158–171) | 13 | 1 | Delete — §C.3. |
| `validateCRQStatusUsage` (handler:176–238) | 62 | 4 | OK; verbose logging. |
| `resolveCRQForNamespace` (handler:242–282) | 40 | 4 | Clean. |
| `PVC.validateOperation` (pvc:79–144) | 65 | 4 | OK. |
| `Pod.validateOperation` (pod:80–133) | 53 | 4 | Clean. |
| `Service.validateOperation` (service:74–108) | ~35 | 3 | Clean. |
