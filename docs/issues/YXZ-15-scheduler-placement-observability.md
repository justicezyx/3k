# YXZ-15: Scheduler placement metrics and unified WNS observability

**Tracker:** [Linear YXZ-15](https://linear.app/yxzhao/issue/YXZ-15/scheduler-prometheus-placement-metrics-and-unified-wns-observability)

GitHub Issues are disabled on `justicezyx/3k`; this file mirrors the issue for repo search and code TODO references.

## Background

The global multi-cluster scheduler (weighted normalized scoring, WNS) lives in `internal/scheduler/schedule` and integrates with CPod pull via `CpodJob` when `Scheduling.Enabled` is true (`docs/GLOBAL_SCHEDULER_SCORING.md`).

Pipeline: load fresh nodes across CPods → `feasibleCandidate` → per-dimension score → `normalizeScores` → weighted sum → `ScoreForCluster` (bind only if polling CPod is the global winner) → optimistic DB claim with `RevertPlacement` on race loss.

P0–P3 (core scoring, all workload types, claims, tests) are implemented. Doc §九 describes observability that is only partially delivered.

## Problem

1. **Metrics gap (doc §九)** — No Prometheus counters/histograms (`scheduler_placement_total{workload,result}`, `scheduler_placement_score{workload}`). The scheduler REST service has no custom metrics.
2. **Logging gap** — `Placement.Breakdown` is logged only for training `user_job` in `cpod_job_logic.go`. Inference, JupyterLab, and AppJob call `scheduleWithClaim` but discard placement details.
3. **Skip-reason ambiguity** — `ScoreForCluster` returns `OK: false` for both infeasible and global winner ≠ polling CPod, dropping breakdown and winner CPod id.
4. **Operational impact** — With `Scheduling.Enabled` default false, teams enabling WNS lack dashboards for placement failures, claim races, or score distributions.

## Proposed solutions

### A. Centralize observability in `scheduleWithClaim` (recommended)

- Add `recordSchedulingObs(workloadKind, workloadID, pollingCpod, p, node, result)` when WNS is active.
- In `tryScheduleOnCpod`, call full `schedule.Score()` first to classify `infeasible` vs `skipped_not_winner` (log global winner + breakdown).
- One structured log per attempt for all workload types; remove duplicate user_job-only log.

### B. Prometheus via go-zero

- Add `Prometheus` block to `cmd/scheduler/etc/scheduler-api*.yaml`.
- Implement `internal/scheduler/metrics/scheduling.go` with `go-zero/core/metric` counter + histogram; low-cardinality labels only.

### C. Alternative: `prometheus/client_golang` like cpodoperator.

## Acceptance criteria

- [ ] Every WNS attempt via `scheduleWithClaim` emits structured log with `workload`, `result`, `breakdown` on assign.
- [ ] `skipped_not_winner`, `infeasible`, `claim_lost` distinguishable in logs and counters.
- [ ] Prometheus metrics exposed on configured `/metrics` port.
- [ ] GLOBAL_SCHEDULER_SCORING.md §九 updated.
- [ ] Unit test for outcome classification.

## Code TODOs

Search: `TODO(YXZ-15)` in `internal/scheduler/` and `cmd/scheduler/`.
