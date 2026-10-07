# Global scheduler design simulation

Minimal offline simulator to compare **shipped** WNS (`schedule.Score` on `(CPod, Node)` with portal `gpu_number` as single-node demand) against **proposed CPod-level gang** policies.

## Policies

| Name | Filter | Headroom / fitness |
|------|--------|-------------------|
| `baseline_shipped` | Single node fits `TotalGPUs()` | Per-node `resource_headroom` (current code) |
| `cpod_gang_bottleneck` | Gang pack exists | Max-min node slack after greedy gang pack |
| `cpod_best_single_node_headroom` | Gang pack exists | Best single-node headroom using portal GPU total (legacy) |
| `cpod_gang_cluster_slack` | Gang pack exists | Cluster GPU slack after gang |

## Workloads

Fixtures mirror repo examples: GPT-3 1.3B 1×8 / 2×8, pytorch multinode 2×4, Llama-7B inference, 1-GPU finetune.

## Run

```bash
go test ./internal/scheduler/designsim/ -v -count=1
go test ./internal/scheduler/designsim/ -run WeightSweep -v
```

Tests log decisions, critiques, and a weight preset recommendation (not a CI gate).
