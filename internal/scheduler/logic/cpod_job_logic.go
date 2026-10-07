package logic

import (
	"context"
	"encoding/json"
	"fmt"
	"strconv"
	"sxwl/3k/internal/scheduler/model"
	"sxwl/3k/pkg/consts"
	"sxwl/3k/pkg/storage"
	"time"

	"github.com/jinzhu/copier"

	"github.com/Masterminds/squirrel"

	"sxwl/3k/internal/scheduler/schedule"
	"sxwl/3k/internal/scheduler/svc"
	"sxwl/3k/internal/scheduler/types"

	"github.com/zeromicro/go-zero/core/logx"
)

type CpodJobLogic struct {
	logx.Logger
	ctx    context.Context
	svcCtx *svc.ServiceContext
}

func NewCpodJobLogic(ctx context.Context, svcCtx *svc.ServiceContext) *CpodJobLogic {
	return &CpodJobLogic{
		Logger: logx.WithContext(ctx),
		ctx:    ctx,
		svcCtx: svcCtx,
	}
}

func (l *CpodJobLogic) CpodJob(req *types.CpodJobReq) (resp *types.CpodJobResp, err error) {
	CpodNodeModel := l.svcCtx.CpodNodeModel
	UserJobModel := l.svcCtx.UserJobModel
	InferenceModel := l.svcCtx.InferenceModel
	JupyterlabModel := l.svcCtx.JupyterlabModel
	AppModel := l.svcCtx.AppModel
	AppJobModel := l.svcCtx.AppJobModel

	// check banned
	_, ok := l.svcCtx.Config.BannedCpod[req.CpodId]
	if ok {
		return nil, fmt.Errorf("cpod is illegal")
	}

	resp = &types.CpodJobResp{}
	resp.JobList = make([]map[string]interface{}, 0)
	resp.InferenceServiceList = make([]types.InferenceService, 0)
	resp.JupyterlabList = make([]types.JupyterLab, 0)
	resp.AppJobList = make([]types.AppJobInfo, 0)

	freshWindow := schedule.ParseFreshWindow(l.svcCtx.Config.Scheduling.NodeFreshWindow, 30*time.Minute)
	// TODO(YXZ-15): Pass workload kind/id into scheduleWithClaim; centralize WNS logs/metrics (YXZ-15).
	// https://linear.app/yxzhao/issue/YXZ-15/scheduler-prometheus-placement-metrics-and-unified-wns-observability
	schedWeights := schedule.WeightsFromConfig(
		l.svcCtx.Config.Scheduling.Weights.ResourceHeadroom,
		l.svcCtx.Config.Scheduling.Weights.LoadBalance,
		l.svcCtx.Config.Scheduling.Weights.CacheLocality,
		l.svcCtx.Config.Scheduling.Weights.Cost,
	)

	clusterSnapshots, nodeByID, err := l.loadGlobalSchedulingView(CpodNodeModel, freshWindow)
	if err != nil {
		return nil, err
	}

	// finetune and cpodjob
	jobs, err := UserJobModel.Find(l.ctx, UserJobModel.AllFieldsBuilder().Where(squirrel.Eq{
		"obtain_status": model.StatusObtainNeedSend,
		"deleted":       0,
	}))
	if err != nil {
		l.Logger.Errorf("user_job find obtain_status=%d deleted=0 err=%s", model.StatusObtainNeedSend, err)
		return nil, err
	}

	activeJobs := make([]*model.SysUserJob, 0)         // 已经在运行中的任务
	assignedJobs := make([]*model.SysUserJob, 0)       // 本次被分配的任务
	assignedNodes := make(map[*model.SysCpodNode]bool) // 本次被分配任务的node

	for _, job := range jobs {
		if !job.CpodId.Valid || job.CpodId.String == "" {
			wl := workloadFromUserJob(job)
			node, placement, err := l.scheduleWithClaim(clusterSnapshots, nodeByID, schedWeights, req.CpodId, wl, func() (bool, error) {
				return l.claimUserJob(job.JobId, req.CpodId)
			})
			if err != nil {
				l.Logger.Errorf("user_job claim job_id=%d err=%s", job.JobId, err)
				return nil, err
			}
			if node != nil {
				assignedJobs = append(assignedJobs, job)
				assignedNodes[node] = true
				// TODO(YXZ-15): Remove once recordSchedulingObs logs breakdown for all workloads (YXZ-15).
				l.Logger.Infof("user_job schedule score=%d cpod_id=%s node=%s job_id=%d breakdown=%+v",
					placement.TotalScore, req.CpodId, node.NodeName, job.JobId, placement.Breakdown)
			}
			continue
		}
		if job.CpodId.Valid && job.CpodId.String == req.CpodId {
			// TODO(YXZ-16): Jobs with cpod_id set skip global cluster Score; consider node-level WNS with PinCluster (YXZ-16 option B).
			// https://linear.app/yxzhao/issue/YXZ-16/global-scheduler-wire-workloadpincluster-cpod-affinity
			activeJobs = append(activeJobs, job)
		}
	}

	for _, job := range assignedJobs {
		l.Logger.Infof("user_job assigned job_id=%d job_name=%s cpod_id=%s", job.JobId, job.JobName.String, req.CpodId)
		cpodJobResp := map[string]any{}
		err = json.Unmarshal([]byte(job.JsonAll.String), &cpodJobResp)
		if err != nil {
			l.Logger.Errorf("unmarshal json=%s err=%s", job.JsonAll.String, err)
			continue
		}
		resp.JobList = append(resp.JobList, cpodJobResp)
	}

	for _, job := range activeJobs {
		// set to resp
		cpodJobResp := map[string]any{}
		err = json.Unmarshal([]byte(job.JsonAll.String), &cpodJobResp)
		if err != nil {
			l.Logger.Errorf("unmarshal json=%s err=%s", job.JsonAll.String, err)
			continue
		}
		resp.JobList = append(resp.JobList, cpodJobResp)
	}

	// inference services
	services, err := InferenceModel.FindAll(l.ctx, InferenceModel.AllFieldsBuilder().Where(
		squirrel.Or{
			squirrel.Eq{"status": model.StatusNotAssigned},
			squirrel.And{squirrel.Eq{"cpod_id": req.CpodId}, squirrel.NotEq{"status": model.StatusStopped}},
		},
	), "")
	if err != nil {
		l.Errorf("InferenceModel.FindAll err: %s", err)
		return nil, err
	}

	for _, service := range services {
		serviceResp := types.InferenceService{}
		if service.Metadata.Valid {
			_ = json.Unmarshal([]byte(service.Metadata.String), &serviceResp)
		} else {
			continue // 老任务没有metadata，直接忽略掉
		}
		if serviceResp.ModelCategory == "" {
			serviceResp.ModelCategory = consts.ModelCategoryEmbedding
		}
		statusDesc, ok := model.StatusToStr[service.Status]
		if ok {
			serviceResp.Status = statusDesc
		}
		serviceResp.CpodId = service.CpodId

		switch service.Status {
		case model.StatusNotAssigned:
			assigned := false
			wl := workloadFromInference(service)
			sid := service.Id
			// TODO(YXZ-15): WNS breakdown/metrics via scheduleWithClaim (YXZ-15).
			node, _, err := l.scheduleWithClaim(clusterSnapshots, nodeByID, schedWeights, req.CpodId, wl, func() (bool, error) {
				return l.claimInference(sid, req.CpodId)
			})
			if err != nil {
				l.Errorf("inference claim inferId=%d err=%s", service.Id, err)
				return nil, err
			}
			if node != nil {
				assignedNodes[node] = true
				assigned = true
			}
			if assigned {
				serviceResp.CpodId = req.CpodId
				l.Infof("inference assigned inferId=%d cpod_id=%s", service.Id, req.CpodId)
				resp.InferenceServiceList = append(resp.InferenceServiceList, serviceResp)
			}
		default:
			resp.InferenceServiceList = append(resp.InferenceServiceList, serviceResp)
		}
	}

	// jupyterlab
	jupyterlabList, err := JupyterlabModel.Find(l.ctx, JupyterlabModel.AllFieldsBuilder().Where(
		squirrel.Or{
			squirrel.Eq{"status": model.StatusNotAssigned},
			squirrel.And{squirrel.Eq{"cpod_id": req.CpodId}, squirrel.NotEq{"status": model.StatusStopped}},
		},
	))
	if err != nil {
		l.Errorf("JupyterlabModel.FindAll err: %s", err)
		return nil, err
	}

	for _, jupyterlab := range jupyterlabList {
		if model.FinalStatus(jupyterlab.Status) {
			continue
		}
		jupyterlabResp := types.JupyterLab{}
		_ = copier.Copy(&jupyterlabResp, jupyterlab)
		jupyterlabResp.CPUCount = strconv.FormatInt(jupyterlab.CpuCount, 10)
		jupyterlabResp.Memory = storage.BytesToHumanReadable(jupyterlab.MemCount)
		jupyterlabResp.GPUCount = int(jupyterlab.GpuCount)
		jupyterlabResp.GPUProduct = jupyterlab.GpuProd
		jupyterlabResp.DataVolumeSize = storage.BytesToHumanReadable(jupyterlab.DataVolumeSize)
		err = json.Unmarshal([]byte(jupyterlab.Resource), &jupyterlabResp.Resource)
		if err != nil {
			l.Errorf("json unmarshal jupyterlab: %d err: %s", jupyterlab.Id, err)
			// return nil, ErrSystem
		}
		jupyterlabResp.UserID = jupyterlab.NewUserId
		jupyterlabResp.Replicas = int(jupyterlab.Replicas)

		switch jupyterlab.Status {
		case model.StatusNotAssigned:
			assigned := false
			wl := workloadFromJupyterlab(jupyterlab, jupyterlabResp.Resource)
			jid := jupyterlab.Id
			// TODO(YXZ-15): WNS breakdown/metrics via scheduleWithClaim (YXZ-15).
			node, _, err := l.scheduleWithClaim(clusterSnapshots, nodeByID, schedWeights, req.CpodId, wl, func() (bool, error) {
				return l.claimJupyterlab(jid, req.CpodId)
			})
			if err != nil {
				l.Errorf("jupyterlab claim id=%d err=%s", jupyterlab.Id, err)
				return nil, err
			}
			if node != nil {
				assignedNodes[node] = true
				assigned = true
			}
			if assigned {
				l.Infof("jupyterlab assigned jupyterId=%d cpod_id=%s", jupyterlab.Id, req.CpodId)
				resp.JupyterlabList = append(resp.JupyterlabList, jupyterlabResp)
			}
		default:
			resp.JupyterlabList = append(resp.JupyterlabList, jupyterlabResp)
		}
	}

	// app job
	appJobList, err := AppJobModel.Find(l.ctx, AppJobModel.AllFieldsBuilder().Where(
		squirrel.Or{
			squirrel.Eq{"status": model.StatusNotAssigned},
			squirrel.And{squirrel.Eq{"cpod_id": req.CpodId}, squirrel.NotEq{"status": model.StatusStopped}},
		},
	))
	if err != nil {
		l.Errorf("AppJobModel.FindAll err: %s", err)
		return nil, err
	}

	appMap := make(map[string]*model.SysApp)
	for _, appJob := range appJobList {
		if model.FinalStatus(appJob.Status) {
			continue
		}

		app, ok := appMap[appJob.AppId]
		if !ok {
			app, err = AppModel.FindOneByQuery(l.ctx, AppModel.AllFieldsBuilder().Where(
				squirrel.Eq{"app_id": appJob.AppId},
			))
			if err != nil {
				l.Errorf("AppModel.FindOneByQuery err: %s", err)
				return nil, err
			}
			appMap[app.AppId] = app
		}

		appJobResp := types.AppJobInfo{}
		_ = copier.Copy(&appJobResp, appJob)
		appJobResp.Crd = app.Crd

		switch appJob.Status {
		case model.StatusNotAssigned:
			assigned := false
			// TODO(YXZ-14): Pass appJob + app into workload builder; scoring uses placeholder 1 CPU today.
			// https://linear.app/yxzhao/issue/YXZ-14/appjob-resource-model-for-global-scheduler-placement
			wl := workloadFromAppJob()
			aid := appJob.Id
			// TODO(YXZ-15): WNS breakdown/metrics via scheduleWithClaim (YXZ-15).
			node, _, err := l.scheduleWithClaim(clusterSnapshots, nodeByID, schedWeights, req.CpodId, wl, func() (bool, error) {
				return l.claimAppJob(aid, req.CpodId)
			})
			if err != nil {
				l.Errorf("appJob claim id=%d err=%s", appJob.Id, err)
				return nil, err
			}
			if node != nil {
				assignedNodes[node] = true
				assigned = true
			}
			if assigned {
				l.Infof("appJob assigned job_name=%d cpod_id=%s", appJob.Id, req.CpodId)
				resp.AppJobList = append(resp.AppJobList, appJobResp)
			}
		default:
			resp.AppJobList = append(resp.AppJobList, appJobResp)
		}
	}

	// update GPU usage
	for node := range assignedNodes {
		_, err = CpodNodeModel.UpdateColsByCond(l.ctx, CpodNodeModel.UpdateBuilder().Where(squirrel.Eq{
			"id": node.Id,
		}).SetMap(map[string]interface{}{
			"gpu_allocatable": node.GpuAllocatable,
			"cpu_allocatable": node.CpuAllocatable,
			"mem_allocatable": node.MemAllocatable,
		}))
		if err != nil {
			l.Logger.Errorf("CpodNodeModel assigned id=%d gpu_allocatable=%d cpu_allocatable=%d mem_allocatable=%d err=%s",
				node.Id, node.GpuAllocatable, node.CpuAllocatable, node.MemAllocatable, err)
			return nil, err
		}
		l.Logger.Infof("CpodNodeModel assigned id=%d gpu_allocatable=%d cpu_allocatable=%d mem_allocatable=%d",
			node.Id, node.GpuAllocatable, node.CpuAllocatable, node.MemAllocatable)
	}

	return
}

func (l *CpodJobLogic) loadGlobalSchedulingView(
	CpodNodeModel model.SysCpodNodeModel,
	freshWindow time.Duration,
) ([]schedule.ClusterSnapshot, map[int64]*model.SysCpodNode, error) {
	allNodes, err := CpodNodeModel.Find(l.ctx, CpodNodeModel.AllFieldsBuilder().Where(
		squirrel.Expr("updated_at > ?", schedule.FreshNodeCutoff(freshWindow)),
	))
	if err != nil {
		l.Logger.Errorf("CpodNodeModel Find all fresh nodes err=%s", err)
		return nil, nil, err
	}
	nodeByID := make(map[int64]*model.SysCpodNode, len(allNodes))
	for _, n := range allNodes {
		nodeByID[n.Id] = n
	}
	cacheList, err := l.svcCtx.CpodCacheModel.Find(l.ctx, l.svcCtx.CpodCacheModel.AllFieldsBuilder())
	if err != nil {
		l.Logger.Errorf("CpodCacheModel Find err=%s", err)
		return nil, nil, err
	}
	priceRows, err := CpodNodeModel.GpuTypeAndPrice(l.ctx)
	if err != nil {
		l.Logger.Errorf("CpodNodeModel GpuTypeAndPrice err=%s", err)
		return nil, nil, err
	}
	gpuPrice := make(map[string]float64)
	for _, row := range priceRows {
		gpuPrice[row.GPUProd] = row.Amount
	}
	clusterSnapshots := schedule.BuildClusterSnapshots(allNodes, cacheList, l.svcCtx.Config.BannedCpod, gpuPrice)
	return clusterSnapshots, nodeByID, nil
}
