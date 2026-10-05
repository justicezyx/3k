# Portal 用户 GPU 并发配额（Quota）

本文档描述算想云 **Global Scheduler**（`cmd/scheduler`）中的 **用户级 GPU 并发上限**：按 `(用户, GPU 类型)` 限制该用户在同一 GPU 型号上**同时 Running 的 GPU 卡数**。这是任务创建时的**可选准入检查**，与账户余额、Kubernetes `ResourceQuota` 是不同机制。

## 一、与其它「限额」的区别

| 机制 | 存储 / 位置 | 含义 | 典型失败原因 |
|------|-------------|------|--------------|
| **Quota（本文）** | MySQL `sys_quota` | 某用户某 GPU 类型上 Running 任务占用的 GPU **数量**上限 | `CheckQuota`：`used + need > quota` |
| **余额（Billing）** | MySQL `user_balance` 等 | 预付费 / 余额是否足够持续扣费 | `余额不足`（如 `job_create_logic` 在 Quota 之前检查） |
| **K8s ResourceQuota** | 集群 namespace | 命名空间内 CPU/GPU/PVC 等资源对象上限 | APIServer 拒绝创建 Pod 等（见 [NAMESPACEISOLATION.md](./NAMESPACEISOLATION.md)） |

产品侧 Quota 常为**锦上添花**：默认不为用户写入 `sys_quota` 行时，**不做并发上限检查**（见下文默认策略）。

计费细节见 [BILLING.md](./BILLING.md)（待完善）。

## 二、数据模型

表 **`sys_quota`**（模型 `internal/scheduler/model.SysQuota`）：

| 字段 | 说明 |
|------|------|
| `new_user_id` | Portal 用户 ID（字符串） |
| `resource` | GPU 类型字符串，与任务表中的 `gpu_type` / `gpu_prod` / `gpu_model` 对齐 |
| `quota` | 该用户在该 GPU 类型上的**最大并发 GPU 卡数**（整数） |

逻辑主键为 `(new_user_id, resource)`；管理接口按行 `id` 做更新与删除。

## 三、检查逻辑 `CheckQuota`

实现：`internal/scheduler/job/quota.go` — `CheckQuota(ctx, svc, userID, resource, need)`。

```text
1. 按 (new_user_id, resource) 查 sys_quota
   - 无记录 → 直接通过（视为该 GPU 类型无配额限制）
   - 有记录 → quota = 行内 quota 字段

2. 统计当前「已占用」GPU 数 used（仅 status = Running）：
   - user_job：work_status = Running，gpu_type = resource，deleted = 0 → 累加 gpu_number
   - inference：status = Running，gpu_type = resource → 累加 gpu_number
   - jupyterlab：status = Running，gpu_prod = resource → 累加 gpu_count

3. 若 used + need > quota → 拒绝，返回剩余 left = quota - used
   否则通过，left = quota - used
```

说明：

- **只计 Running**：Pending、Stopped、失败等状态不占配额；创建瞬间的检查基于当前 Running 汇总，**不会在创建时预占**配额行（无 reservation 表）。
- **按 GPU 类型分桶**：训练、推理、Jupyter 使用各自表中的 GPU 类型字段，必须与 `sys_quota.resource` 字符串一致。
- **FineTune** 在提交微调任务时按关联 `user_job` 的 `GpuType` / `GpuNumber` 调用同一 `CheckQuota`。
- **YAML 应用**等其它创建路径当前**未**调用 `CheckQuota`（仅训练创建、FineTune、推理部署、JupyterLab 创建）。

## 四、准入顺序（任务创建）

以训练任务 `JobCreate` 为例（`internal/scheduler/logic/job_create_logic.go`）：

1. **余额**：`user_balance.balance < 0` → 拒绝（`余额不足`）。
2. **Quota**：`CheckQuota` → 拒绝时日志/错误中带 `left` 与 `need`。
3. 通过后写入任务并进入调度队列（全局调度见 [GLOBAL_SCHEDULER_SCORING.md](./GLOBAL_SCHEDULER_SCORING.md)）。

推理部署、JupyterLab 创建、FineTune 同样在各自 Logic 中调用 `CheckQuota`（字段名分别为 `GpuModel`/`GpuCount`、`GPUProduct`/`GPUCount`、训练 Job 的 GPU 字段）。

Quota **不替代**调度阶段的 CPod/节点资源是否足够；它只限制**单用户单 GPU 型号的并发占用**。

## 五、管理 API

Scheduler HTTP API（`cmd/scheduler/scheduler.api`），Gateway 对外路径 **`/api/resource/quota`** → Scheduler **`/quota`**（见 `cmd/gateway/etc/gateway-api*.yaml`）。

| 方法 | Scheduler 路径 | 说明 |
|------|----------------|------|
| POST | `/quota` | 新增 `(user_id, resource, quota)` |
| GET | `/quota` | 列表；可选 `user_id` 查询参数过滤目标用户 |
| PUT | `/quota` | 按 `id` 更新 `quota` 数值 |
| DELETE | `/quota` | 按 `id` 删除 |

所有 Quota 写操作与列表在 Logic 层通过 **`UserModel.IsAdmin`** 校验：仅管理员可操作（非管理员返回 `ErrNotAdmin`）。请求需带 Header **`Sx-User-ID`**（操作者）。

控制台 / UI 封装见 `ui/src/services/index.ts`（`/api/resource/quota`）。

## 六、代码索引

| 用途 | 路径 |
|------|------|
| 配额检查 | `internal/scheduler/job/quota.go` |
| 训练创建 | `internal/scheduler/logic/job_create_logic.go` |
| FineTune | `internal/scheduler/logic/finetune_logic.go` |
| 推理部署 | `internal/scheduler/logic/inference_deploy_logic.go` |
| JupyterLab | `internal/scheduler/logic/jupyterlab_create_logic.go` |
| CRUD Handlers | `internal/scheduler/handler/quota_*_handler.go` |
| CRUD Logic | `internal/scheduler/logic/quota_*_logic.go` |
| 表模型 | `internal/scheduler/model/sys_quota_model_gen.go` |

## 七、已知局限与演进方向（非承诺）

- 无配额行 = 无限制，与「全平台默认 cap」不兼容；若产品需要默认上限，需在用户注册或租户开通时写入默认 `sys_quota` 或改代码默认策略。
- 占用仅看 Portal DB 中 Running 状态，与集群内实际 Pod 相位可能存在短暂不一致（心跳 / 状态同步延迟）。
- 未覆盖 YAML 应用、按量并发以外的维度（如总 GPU 不分型号、CPod 级限额等）。
