# Scheduling deep dive: CPod vs node, and `NodeFreshWindow`

Companion notes for the global scheduler. Algorithm and integration overview: [GLOBAL_SCHEDULER_SCORING.md](./GLOBAL_SCHEDULER_SCORING.md).

---

## Terminology: CPod is not “the node”

| Term | Meaning in 3k |
|------|----------------|
| **CPod** | One Kubernetes cluster / tenant, identified by `cpod_id`. Scheduling compares **clusters** (cache, price, load-balance score). |
| **Node** | One **Kubernetes node** inside a CPod: a row in `sys_cpod_node` (`node_name`, GPU/CPU/mem allocatable, `cpod_id`, `updated_at`). |
| **Candidate** | A feasible **(CPod, node)** pair—the unit that gets filtered, scored, and (if this CPod wins) chosen for placement. |

Global scoring picks the best **(CPod, node)**, not “a CPod” alone. Cluster-level signals (cache locality, cost, load balance) attach to the CPod; capacity and headroom attach to the specific node.

---

## What is `Scheduling.NodeFreshWindow`?

YAML under `Scheduling` in `scheduler-api*.yaml`:

```yaml
Scheduling:
  Enabled: true
  NodeFreshWindow: 30m
  Weights:
    # ...
```

- **Type:** Go `time.Duration` string (e.g. `30m`, `45m`).
- **Default:** `30m` if unset (`internal/scheduler/config/config.go`).
- **Parsing:** `schedule.ParseFreshWindow` — empty or invalid values fall back to **30 minutes** (`internal/scheduler/schedule/config.go`).

At runtime:

1. `freshWindow := ParseFreshWindow(Config.Scheduling.NodeFreshWindow, 30*time.Minute)`
2. `cutoff := FreshNodeCutoff(freshWindow)` → `time.Now().Add(-freshWindow)`
3. Node queries use **`updated_at > cutoff`** on `sys_cpod_node`.

`CpodJob` loads **all** fresh nodes across CPods to build global snapshots; the same cutoff applies to that query (`internal/scheduler/logic/cpod_job_logic.go`).

---

## Why it exists: per-node heartbeat freshness

CPod agents report cluster state (e.g. via `CpodStatus` / heartbeats). That flow updates **`sys_cpod_node`** rows: allocatable resources and **`updated_at`**.

**Problem without a window:** Portal’s DB can still show allocatable capacity on a node that is **offline, removed, or no longer reported** by the CPod. Assigning there would target capacity that does not exist until the next truthful heartbeat overwrites or removes the row.

**What the window does:** Treat a node as **schedulable only if its row was updated recently**. If `updated_at` is older than `NodeFreshWindow`, that **node row is excluded** from the candidate set.

Important nuances:

- Freshness is **per node row**, not one timestamp for the whole CPod. A CPod can stay online while **individual nodes** go stale and drop out of scoring; other fresh nodes in the same CPod remain candidates.
- This is **not** a job timeout, poll interval, or CPod liveness TTL for the tenant—it only filters **which node records** participate in placement for this request.
- Stale exclusion reduces bad placements; it does not replace correct heartbeat-driven updates (see “dual ledger” notes in [GLOBAL_SCHEDULER_SCORING.md](./GLOBAL_SCHEDULER_SCORING.md)).

---

## Mental model

```text
For each sys_cpod_node row:
  if updated_at <= now - NodeFreshWindow:
    ignore for scheduling (stale / likely offline or unreported)
  else:
    eligible as part of (cpod_id, node) candidates
```

---

## Code pointers

| Piece | Location |
|-------|----------|
| Config field | `internal/scheduler/config/config.go` → `SchedulingConfig.NodeFreshWindow` |
| Parse + fallback | `internal/scheduler/schedule/config.go` → `ParseFreshWindow` |
| Cutoff time | `internal/scheduler/schedule/snapshot.go` → `FreshNodeCutoff` |
| Query filter | `internal/scheduler/logic/cpod_job_logic.go` → `updated_at > FreshNodeCutoff(...)` |
| Tests | `internal/scheduler/schedule/config_test.go`, `snapshot_test.go` |

---

## Related docs

- [GLOBAL_SCHEDULER_SCORING.md](./GLOBAL_SCHEDULER_SCORING.md) — WNS pipeline, pull model, weights
- [SYSTEM_ARCHITECTURE.md](./SYSTEM_ARCHITECTURE.md) — heartbeat vs Portal allocatable
