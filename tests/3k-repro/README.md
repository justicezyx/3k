# Reproducing the central-scheduler gaps

Tests for the findings in [`docs/scheduler/REVIEW-revised-design-gaps-2026-10-05.md`](../../docs/scheduler/REVIEW-revised-design-gaps-2026-10-05.md), against public `NascentCore/3k` at `17b19cf`. A test passes when it shows the behavior the finding describes.

Historical pin: `run.sh` always clones `https://github.com/NascentCore/3k` and checks out `17b19cf`. This fork’s `main` is ahead of that commit, so the suite does not run against the current checkout.

This directory does not change upstream 3k. `run.sh` clones that commit into a temp directory, copies the Go tests in, and runs `go test`. Nothing is pushed to that repository. `run.sh` does not start kind and does not run `kind/p12_cascade.sh`. `tests/3k-repro/go.mod` keeps these packages out of this repo’s `go test ./...`; the `*_test.go` files are compiled only after they are copied into the pin.

Category 6 (P32, P33, P34) is a list of enhancements, not issues. Those three are not tested. P5, P16, P20, and P22 were removed from the note and are not tested.

Status in the table is one of:

- **reproduced** — the test called shipped code, or read the loaded schema, and observed the behavior.
- **design model / by construction** — the test encodes a sentence from the note in this repo. The missing write is absent because the test does not perform it. It does not call 3k and is not a reproduction of shipped behavior.
- **source match** — the test compares checked-in yaml or Go source text. It does not execute that code.

## How to run

MariaDB or MySQL must be listening on `127.0.0.1:3306`, with a user that can create databases. The default DSN is `repro:repro@tcp(127.0.0.1:3306)/`. Override it with `REPRO_MYSQL_DSN`. This suite was run on MariaDB. The dump's `ROW_FORMAT=COMPACT` is rewritten to `DYNAMIC` before import because MariaDB otherwise rejects `sys_user` (ERROR 1118).

```bash
tests/3k-repro/run.sh
```

The script loads `deployment/sxcloud/database/init.sql` from the clone. Two columns the Go models select, and the dump does not have, are added in the harness only: `sys_cpod_node.cpod_name` and `sys_inference.model_meta`. Those `ALTER TABLE` statements run only after an `information_schema` check, so they do not use `ADD COLUMN IF NOT EXISTS`. App tables missing from the dump are created from the Go structs so `CpodJob` can query them.

For the import only, the harness sets `GLOBAL innodb_strict_mode=OFF` and `GLOBAL innodb_default_row_format=dynamic`. It restores both values when the import returns, and `TestMain` restores them again and drops the database `repro_schema`. Each test database is dropped in `t.Cleanup`. A run from before that restore had already left `innodb_strict_mode` OFF, so the value captured at the start of the latest `run.sh` was OFF and the restore wrote OFF again. MariaDB 10.11 defaults that variable to ON; it was set back to ON on this machine after the suite. `innodb_default_row_format` stayed `dynamic`, which is the MariaDB 10.11 default. `repro_schema` was not left behind.

Each test connection sets the server's isolation variable to `REPEATABLE-READ` and fails if the session is anything else. MariaDB 10.11 exposes that as `tx_isolation`; MySQL 8 exposes `transaction_isolation`. The harness uses whichever name the server has. go-zero runs the scheduler statements autocommit, so that isolation is not a snapshot wrapped around `CpodJob`.

The controller package at this pin does not compile its old tests (`DeployWebUI` is gone) and `go test` vets a `logrus.Infof` call with no format verb. `run.sh` moves those old test files aside inside the temp clone and passes `-vet=off` for that one package. The clone is the only tree it edits.

## Results

`run.sh` is the suite. The kind cascade is a separate script. Design-model rows are not shipped-code reproductions.

P1 logs `P1 double-claim count N/30`. The assertion is `N >= 20`. A count of 0 used to pass. 27/30 and 28/30 have both been observed, so a single older count is not the gate. The latest `run.sh` on this VM logged **28/30**. A sequential second island does not emit the job. On a double claim the table still has one row and one `cpod_id`.

P18 is not a race. `JobCreate` rejects `balance < 0`, inserts the job, and does not update `user_balance`. Two sequential creates on 0.00 both succeed and leave the balance at 0.00. A missing balance row and a balance of -1.00 are rejected.

| ID | Category | Test | Proves | Run | Status |
|---|---|---|---|---|---|
| P1 | 1. Code vs design | `TestP1_DoubleClaim_BothPullersGetJob` | Two concurrent `CpodJob` callers can both return one unassigned train job. The claim is `UPDATE ... WHERE job_id` and `sql.Result` is discarded. The row stores one `cpod_id`. A later sequential pull does not emit it. | `go test ./internal/scheduler/logic/ -run TestP1` | reproduced (gate >= 20/30; latest run 28/30) |
| P2 | 1. Code vs design | `TestP2_HeartbeatOverwritesDebitedAllocatable`, `TestP2_ObserverCountsOnlyBoundPodGPUs` | Status update writes `gpu_allocatable` from observed free and replaces a debit of 0 with 8. The node table has no `reserved_gpu`. The observer subtracts GPU requests only when `NodeName` is set. | logic + `cpodoperator/internal/synchronizer` | reproduced |
| P7 | 1. Code vs design | `TestP7_ZeroGPUPerReplica`, `TestP7_PyTorchAndMPIRequestZeroGPU` | `GpuNumber >= 8` stores `GPURequiredPerReplica = 0` and `WorkerReplicas = GpuNumber/8`. PyTorch and MPI then request `nvidia.com/gpu: 0`. A 7-GPU job keeps 7. Finetune is skipped. | synchronizer + controller | reproduced |
| P8 | 1. Code vs design | `TestP8_PresetCpodIDSkipsGPUCount` | A train row already stamped with the caller's `cpod_id`, and an inference row already `StatusAssigned`, are returned when the node has 1 free GPU and the job wants 16. The node debit stays 1. | logic | reproduced |
| P10 | 1. Code vs design | `TestP10_EmptyGPUTypeMatchesAnyNode` | Empty `gpu_type` matches an H100 node with 0 free GPUs and 1 free CPU. Allocatable becomes -8. | logic | reproduced |
| P12 | 2. Code incomplete | `TestP12_CheckpointPVCOwnerRefCascade`, `kind/p12_cascade.sh` | Checkpoint PVC is created with `Controller` and `BlockOwnerDeletion`. `releaseSavedModel` strips the owner ref on the model-save PVC only. On a kind cluster the real operator creates that PVC, and garbage collection deletes it when the CPodJob is deleted and when the FineTune parent is deleted. | fake client + kind (kind is not part of `run.sh`) | reproduced |
| P26 | 2. Code incomplete | `TestP26_HeartbeatPayloadFieldsMissing` | Heartbeat JSON from `getResourceInfo` has no queue depth, RDMA domain, drain/health, or inventory epoch. Node `status` is the label `status`. | fake client | reproduced |
| P11 | 3. Missing use case | `TestP11_StopAndOverdraftDoNotRelease` | `JobStop` and `JobsDel` set deleted or stopped and leave `cpod_id` and `gpu_allocatable` unchanged. The shipped code has the same gap the note leaves out of the loop. | logic, MySQL | reproduced |
| P14 | 3. Missing use case | `TestP14_FilterMissHasNoReason` | An A100 job against an H100 node stays need-send. `types.Job` has `ObtainStatus` and no reason field. | logic, MySQL | reproduced |
| P23 | 3. Missing use case | `TestP23_IslandOwnerPaymentHasNoAPI` | Every loaded table and every `Path` in `routes.go` is scanned. `user_balance`, `user_billing`, `user_recharge`, and `/pay/balance` are present. A table or path whose name contains payout, payee, disburse, settlement, or settle fails the test. | logic, schema + routes source | reproduced |
| P3 | 4. Design quality | `TestP3_ReservedGPUHasNoJobKeyAndNoDecrease` | The encoded debit matches `inv_version` and `gpu_allocatable` and does not attach a job id. Success, failure, stop, overdraft, and heartbeat expiry do not decrease `reserved_gpu`. | design model | design model / by construction |
| P13 | 4. Design quality | `TestP13_DrainMarkerIsUnnamed` | The encoded re-admit clears `cpod_id` only. No obtain-status write, product check, or reservation release is performed, because this file does not perform one. | design model | design model / by construction |
| P15 | 4. Design quality | `TestP15_ClaimWhereIDMissesTrainKey` | `UPDATE sys_user_job ... WHERE id=?` errors (no `id` column; key is `job_id`). The same statement matches `sys_inference.id`. | logic, MySQL | reproduced |
| P4 | 4. Design quality | `TestP4_StaleNodeDropsCandidateButStickyJobStillEmitted`, `TestP4_SilenceDoesNotDrain` | MySQL: a node silent for 31 minutes is not a candidate, a job already on that `cpod_id` is still returned, and the node table has no drain column. The silence test only encodes the note: a 30-minute predicate does not drain, because the model never drains. | logic, MySQL + design model | reproduced (MySQL); design model / by construction (silence) |
| P19 | 4. Design quality | `TestP19_ManagersReturnBeforeAnyWrite`, `TestP19_DefaultConfigSkipsBillAndBalanceStop`, `TestP19_ControlLoopHasNoBillWrite` | With both cron flags off, `BillingManager.Update` and `BalanceManager.Update` return before any model call. Turning one flag on reaches that manager's models. The yaml test matches `CronBilling` / `CronBalance` text and the early-return source. The control-loop test encodes the note's step list and does not call the managers. | pay stubs + yaml source match + design model | reproduced (managers); source match (yaml); design model / by construction (control loop) |
| P9 | 4. Design quality | `TestP9_FourMatchersAndTwoSkipCapacity` | Train with `cpu_allocatable` 0 stays unassigned. Train with `cpu_allocatable` 1 and a free GPU is assigned. Inference requires 4 CPU and 50GiB. Jupyter with an empty GPU product assigns with no node. App jobs assign with no capacity check. | logic, MySQL | reproduced |
| P21 | 4. Design quality | `TestP21_QuotaKeyIsUserNotOrg` | `sys_quota` has no org column. User A is rejected at quota 1. User B in the same `company_id` has no row and is allowed. | logic, MySQL | reproduced |
| P27 | 4. Design quality | `TestP27_CheckpointContractHasNoFlushOrResume` | The encoded contract is an off-island copy or a non-cascaded PVC. Re-admit clears `cpod_id`. Flush and resume stay empty because the model leaves them empty. | design model | design model / by construction |
| P28 | 4. Design quality | `TestP28_P4MeansAdmitAndCreateTimeBind` | One reading of P4 admits and leaves `cpod_id` empty. Another writes `cpod_id` at create and does not read balance. The two functions are defined that way in the test. | design model | design model / by construction |
| P29 | 4. Design quality | `TestP29_ReplicaInferHasNoReplicaColumn` | `sys_inference` has `cpod_id` and `metadata` and no replica or instance column. `InferenceDeploy` writes `min_instances` and `max_instances` into `metadata` and stores one `cpod_id`. | logic, MySQL | reproduced |
| P24 | 4. Design quality | `TestP24_CacheLocalityHasNoNamedInput` | The encoded score uses headroom, load, and cost. Two islands that differ only by cache objects tie, because the function ignores that field. | design model | design model / by construction |
| P30 | 4. Design quality | `TestP30_TwoRDMALabels` | `needAllocateRDMADevice` counts `feature.node.kubernetes.io/rdma.available`. `e2e/a_ib_test.go` counts `rdma.capable`. Two capable-only nodes do not allocate RDMA. The note names both labels and does not pick one. | fake client + source | reproduced |
| P31 | 4. Design quality | `TestP31_NotBannedIsConfigMapNotATable` | `Config.BannedCpod` rejects the puller before the node query. `sys_cpod_node` has no ban column. | logic, MySQL | reproduced |
| P6 | 5. Design wrong | `TestP6_SixteenGPUJobNeverFitsTwoEightGPUNodes` | A 16-GPU job against two 8-GPU nodes stays need-send. Each comparison is one row's `GpuAllocatable >= GpuNumber`. Shipped code implements the one-node rule the note keeps. | logic, MySQL | reproduced |
| P17 | 5. Design wrong | `TestP17_QuotaCountsOnlyRunning` | Pending train, inference, and jupyter GPUs are not counted. A running train, a running inference, and a running jupyter each fill the quota while the quota row exists. A missing quota row allows the job. A running app job is not counted. | logic, MySQL | reproduced |
| P18 | 5. Design wrong | `TestP18_BalanceReadBeforeInsert` | Two sequential creates on balance 0.00 both insert. Balance stays 0.00. A missing row fails. A negative balance is rejected and stays negative. Create does not debit. | logic, MySQL | reproduced (deterministic; not a race) |
| P32 | 6. Enhancement | — | Unique key on `sys_quota (new_user_id, resource)`. Not an issue. | — | skipped |
| P33 | 6. Enhancement | — | Heartbeat as the only writer of `gpu_allocatable`. Not an issue. | — | skipped |
| P34 | 6. Enhancement | — | One reservation row with `expires_at`. Not an issue. | — | skipped |

## Kind cascade (P12)

`kind/p12_cascade.sh` is not part of `run.sh`. It needs docker, kind, and kubectl. It clones `NascentCore/3k` at `17b19cf` (or reuses `P12_3K_DIR` when that checkout is already the pin), creates kind cluster `p12-cascade`, installs the CPodJob and FineTune CRDs plus the MPIJob, PyTorchJob, and InferenceService CRDs the operator watches, and runs `cpodoperator/cmd/operator` against the cluster. No GPUs. The operator is not modified.

The script writes kubectl transcripts to a temp directory and copies them into `kind/evidence/` only after the checks pass. It parses `ownerReferences` (kind, name, `controller`, `blockOwnerDeletion`) and fails if the operator log does not contain `ckpt pvc not found, create it` for both claims. The review pass that added those checks did not re-run the cluster. The transcripts already in `kind/evidence/` are from the earlier successful run, and they still parse as those owner refs.

The script then does two deletes, both `--cascade=background`:

1. A CPodJob with `ckptPath` and `ckptVolumeSize` set and no model or dataset. `GetCKPTPVC` creates `{name}-ckpt` (`operator.log`: `ckpt pvc not found, create it`). The PVC's owner ref is `CPodJob`, `controller: true`, `blockOwnerDeletion: true`. Deleting the CPodJob leaves the namespace empty.
2. A FineTune for `ZhipuAI/chatglm3-6b`. The FineTune reconciler creates `{name}-cpodjob` with a controlling owner ref to the FineTune, and the CPodJob reconciler creates the checkpoint PVC. Deleting the FineTune removes the CPodJob and `p12ft-cpodjob-ckpt`. A copied public-model PVC with no owner ref to the FineTune stays.

```
$ kubectl delete cpodjob -n p12-job p12job --cascade=background --wait=true
cpodjob.cpod.cpod "p12job" deleted from p12-job namespace
$ kubectl get cpodjob,pvc -n p12-job
No resources found in p12-job namespace.
```

```
$ kubectl delete finetune -n p12-ft p12ft --cascade=background --wait=true
finetune.cpod.cpod "p12ft" deleted from p12-ft namespace
$ kubectl get cpodjob -n p12-ft p12ft-cpodjob
Error from server (NotFound): cpodjobs.cpod.cpod "p12ft-cpodjob" not found
$ kubectl get pvc -n p12-ft p12ft-cpodjob-ckpt
Error from server (NotFound): persistentvolumeclaims "p12ft-cpodjob-ckpt" not found
```

## Hard to test

No finding was left without a running test. The design-model rows above pass by construction. The kind cascade is the apiserver garbage-collection check that the fake client cannot do, and it is separate from `run.sh`.
