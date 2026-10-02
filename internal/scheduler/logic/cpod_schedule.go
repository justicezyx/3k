package logic

import (
	"database/sql"
	"encoding/json"
	"sxwl/3k/internal/scheduler/model"
	"sxwl/3k/internal/scheduler/schedule"
	"sxwl/3k/internal/scheduler/types"
	"time"

	"github.com/Masterminds/squirrel"
)

const emptyCpodExpr = "(cpod_id = '' OR cpod_id IS NULL)"

func (l *CpodJobLogic) tryScheduleOnCpod(
	clusters []schedule.ClusterSnapshot,
	live map[int64]*model.SysCpodNode,
	weights schedule.Weights,
	cpodID string,
	wl schedule.Workload,
) (*model.SysCpodNode, schedule.Placement, bool) {
	// TODO(YXZ-15): Classify infeasible vs skipped_not_winner via schedule.Score(); log global winner + breakdown.
	// https://linear.app/yxzhao/issue/YXZ-15/scheduler-prometheus-placement-metrics-and-unified-wns-observability
	p := schedule.ScoreForCluster(wl, clusters, weights, cpodID)
	if !p.OK {
		return nil, p, false
	}
	node := schedule.CommitPlacement(clusters, live, p, wl)
	if node == nil {
		return nil, p, false
	}
	return node, p, true
}

func (l *CpodJobLogic) scheduleWithClaim(
	clusters []schedule.ClusterSnapshot,
	live map[int64]*model.SysCpodNode,
	weights schedule.Weights,
	cpodID string,
	wl schedule.Workload,
	claim func() (bool, error),
) (*model.SysCpodNode, schedule.Placement, error) {
	// TODO(YXZ-15): recordSchedulingObs(workload, id, cpodID, p, node, result) for logs + metrics/scheduling.go.
	// https://linear.app/yxzhao/issue/YXZ-15/scheduler-prometheus-placement-metrics-and-unified-wns-observability
	node, p, ok := l.tryScheduleOnCpod(clusters, live, weights, cpodID, wl)
	if !ok {
		return nil, p, nil
	}
	claimed, err := claim()
	if err != nil {
		schedule.RevertPlacement(clusters, live, p, wl)
		return nil, p, err
	}
	if !claimed {
		schedule.RevertPlacement(clusters, live, p, wl)
		return nil, p, nil
	}
	return node, p, nil
}

func (l *CpodJobLogic) claimUserJob(jobID int64, cpodID string) (bool, error) {
	result, err := l.svcCtx.UserJobModel.UpdateColsByCond(l.ctx, l.svcCtx.UserJobModel.UpdateBuilder().Where(squirrel.And{
		squirrel.Eq{
			"job_id":         jobID,
			"obtain_status":  model.StatusObtainNeedSend,
			"deleted":        0,
		},
		squirrel.Expr(emptyCpodExpr),
	}).SetMap(map[string]interface{}{
		"cpod_id":     cpodID,
		"update_time": sql.NullTime{Time: time.Now(), Valid: true},
	}))
	if err != nil {
		return false, err
	}
	return rowsAffectedOne(result)
}

func (l *CpodJobLogic) claimInference(id int64, cpodID string) (bool, error) {
	result, err := l.svcCtx.InferenceModel.UpdateColsByCond(l.ctx, l.svcCtx.InferenceModel.UpdateBuilder().Where(squirrel.And{
		squirrel.Eq{"id": id, "status": model.StatusNotAssigned},
		squirrel.Expr(emptyCpodExpr),
	}).SetMap(map[string]interface{}{
		"cpod_id": cpodID,
		"status":  model.StatusAssigned,
	}))
	if err != nil {
		return false, err
	}
	return rowsAffectedOne(result)
}

func (l *CpodJobLogic) claimJupyterlab(id int64, cpodID string) (bool, error) {
	result, err := l.svcCtx.JupyterlabModel.UpdateColsByCond(l.ctx, l.svcCtx.JupyterlabModel.UpdateBuilder().Where(squirrel.And{
		squirrel.Eq{"id": id, "status": model.StatusNotAssigned},
		squirrel.Expr(emptyCpodExpr),
	}).SetMap(map[string]interface{}{
		"cpod_id": cpodID,
		"status":  model.StatusAssigned,
	}))
	if err != nil {
		return false, err
	}
	return rowsAffectedOne(result)
}

func (l *CpodJobLogic) claimAppJob(id int64, cpodID string) (bool, error) {
	result, err := l.svcCtx.AppJobModel.UpdateColsByCond(l.ctx, l.svcCtx.AppJobModel.UpdateBuilder().Where(squirrel.And{
		squirrel.Eq{"id": id, "status": model.StatusNotAssigned},
		squirrel.Expr(emptyCpodExpr),
	}).SetMap(map[string]interface{}{
		"cpod_id": cpodID,
		"status":  model.StatusAssigned,
	}))
	if err != nil {
		return false, err
	}
	return rowsAffectedOne(result)
}

func rowsAffectedOne(result sql.Result) (bool, error) {
	if result == nil {
		return false, nil
	}
	n, err := result.RowsAffected()
	if err != nil {
		return false, err
	}
	return n == 1, nil
}

func workloadFromUserJob(job *model.SysUserJob) schedule.Workload {
	return schedule.Workload{
		GPUProduct: job.GpuType.String,
		GPUCount:   job.GpuNumber.Int64,
		CPUCores:   1,
		CacheIDs:   schedule.TrainingCacheIDs(job.PretrainedModelName.String, job.DatasetName.String),
	}
}

func workloadFromInference(service *model.SysInference) schedule.Workload {
	return schedule.Workload{
		GPUProduct: service.GpuType.String,
		GPUCount:   service.GpuNumber.Int64,
		CPUCores:   schedule.InferenceCPUCores,
		MemBytes:   schedule.InferenceMemBytes(),
		CacheIDs:   schedule.InferenceCacheIDs(service.ModelName.String, adapterNameFromInference(service)),
	}
}

func adapterNameFromInference(service *model.SysInference) string {
	if !service.Metadata.Valid {
		return ""
	}
	var meta types.InferenceService
	_ = json.Unmarshal([]byte(service.Metadata.String), &meta)
	return meta.AdapterName
}

func workloadFromJupyterlab(j *model.SysJupyterlab, resource types.JupyterResource) schedule.Workload {
	wl := schedule.Workload{
		CPUCores: j.CpuCount,
		MemBytes: j.MemCount,
	}
	if j.GpuProd != "" {
		wl.GPUProduct = j.GpuProd
		wl.GPUCount = j.GpuCount
	}
	for _, m := range resource.Models {
		wl.CacheIDs = append(wl.CacheIDs, schedule.TrainingCacheIDs(m.ModelName, "")...)
	}
	for _, d := range resource.Datasets {
		wl.CacheIDs = append(wl.CacheIDs, schedule.TrainingCacheIDs("", d.DatasetName)...)
	}
	for _, a := range resource.Adapters {
		wl.CacheIDs = append(wl.CacheIDs, schedule.InferenceCacheIDs("", a.AdapterName)...)
	}
	wl.CacheIDs = schedule.UniqueCacheIDs(wl.CacheIDs)
	return wl
}

// TODO(YXZ-14): Derive CPU/mem/GPU and CacheIDs from SysApp.Crd and/or AppJob Meta.
// https://linear.app/yxzhao/issue/YXZ-14/appjob-resource-model-for-global-scheduler-placement
func workloadFromAppJob() schedule.Workload {
	return schedule.Workload{CPUCores: 1}
}
