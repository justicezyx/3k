# YXZ-14: AppJob resource model for global scheduler placement

**Tracker:** [Linear YXZ-14](https://linear.app/yxzhao/issue/YXZ-14/appjob-resource-model-for-global-scheduler-placement)

GitHub Issues are disabled on `justicezyx/3k`; this file mirrors the issue for repo search and code TODO references.

## Background

The global multi-cluster scheduler (`internal/scheduler/schedule`) scores feasible `(CPod, node)` pairs using a `Workload` struct (GPU product/count, CPU, memory, cache IDs). Integration lives in `CpodJob` (`internal/scheduler/logic/cpod_job_logic.go`, `cpod_schedule.go`).

Training jobs, inference, and JupyterLab map DB fields (and Jupyter `Resource` JSON) into `Workload`. AppJobs are different: capacity is defined by the app template YAML (`SysApp.Crd`) and optional job `Meta`, not columns on `sys_app_job`.

See also `docs/GLOBAL_SCHEDULER_SCORING.md` §1.2.

## Problem

1. **`workloadFromAppJob()` is a stub** — always `CPUCores: 1`; no GPU, memory, or cache IDs.
2. **Legacy path** — `claimAppJob` with no node capacity check when scheduling is disabled.
3. **Enabled path** — global scoring runs on a placeholder footprint; `CommitPlacement` deducts 1 CPU only.
4. **Data model** — `SysAppJob` has no resource columns; requirements must come from CRD/Meta or new fields.

## Proposed solutions

- **A (recommended):** Parse CPU/mem/GPU from `SysApp.Crd` YAML when building `Workload`.
- **B:** Store explicit resources on create (`Meta` or DB columns).
- **C:** Policy until A/B — document best-effort or skip global score for AppJob.
- **D (complement):** CPod agent rejects CRDs that exceed reported capacity.

## Acceptance criteria

- [ ] Real `Workload` for AppJob from app/job data
- [ ] Consistent checks in legacy and enabled paths
- [ ] Tests + doc table row for AppJob mapping

## Code TODOs

Search: `TODO(YXZ-14)` in `internal/scheduler/logic/`.
