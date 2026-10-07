# YXZ-16: Global scheduler — wire Workload.PinCluster (CPod affinity)

**Tracker:** [Linear YXZ-16](https://linear.app/yxzhao/issue/YXZ-16/global-scheduler-wire-workloadpincluster-cpod-affinity)

GitHub Issues are disabled on `justicezyx/3k`; this file mirrors the issue for repo search and code TODO references.

## Background

The global multi-cluster scheduler (`internal/scheduler/schedule`) scores feasible `(CPod, node)` pairs using a `Workload` struct. Hard CPod affinity is `Workload.PinCluster`: when non-empty, `feasibleCandidate()` in `scheduler.go` rejects candidates on other clusters.

Design: `docs/GLOBAL_SCHEDULER_SCORING.md` §3.1 (CPod filter) and §二 (preserve user `cpod_id` affinity).

Production workloads are built in `internal/scheduler/logic/cpod_schedule.go` and scored from `CpodJob`.

**Evidence:** `PinCluster` appears only in `types.go`, `feasibleCandidate`, and `TestFilter_pinCluster` — no production builder sets the field.

## Problem

1. **Dead filter** — `PinCluster` is always empty in production; WNS considers all fresh CPods.
2. **Split models** — User CPod choice is often DB `cpod_id` at create (`job_create_logic`, `inference_deploy_logic`), which bypasses global cluster scoring; nothing maps into `PinCluster` for rows that still call `Score()`.
3. **Doc drift** — Docs describe CPod affinity via the scheduler filter; behavior is mostly DB routing in `cpod_job_logic.go`.
4. **Future** — Soft pin (score within one CPod while `cpod_id` empty until claim) needs this hook or an API field.

## Proposed solutions

- **A (recommended):** Helper to set `PinCluster` from DB/API fields in all `workloadFrom*`; document rules per workload type.
- **B:** For jobs with `cpod_id` set but still `NeedSend`, run node-level WNS on that CPod via `PinCluster` / dedicated path.
- **C:** New `preferred_cpod_id` API vs pre-bind `cpod_id`.
- **D:** Remove `PinCluster` and document DB-only affinity.

## Acceptance criteria

- [ ] Documented rules: `cpod_id` vs `PinCluster` per workload type
- [ ] Production builders wired (or field removed per Option D)
- [ ] Tests for builder mapping / pinned scoring
- [ ] Update `GLOBAL_SCHEDULER_SCORING.md`

## Code TODOs

Search: `TODO(YXZ-16)` in `internal/scheduler/`.
