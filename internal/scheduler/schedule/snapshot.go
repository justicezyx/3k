package schedule

import (
	"time"

	"sxwl/3k/internal/scheduler/model"
)

// BuildClusterSnapshots groups DB node rows into per-CPod snapshots.
// bannedCpodIDs entries are omitted entirely.
func BuildClusterSnapshots(
	nodes []*model.SysCpodNode,
	caches []*model.SysCpodCache,
	bannedCpodIDs map[string]string,
	gpuPrice map[string]float64,
) []ClusterSnapshot {
	cacheByCpod := make(map[string]map[string]struct{})
	for _, c := range caches {
		if cacheByCpod[c.CpodId] == nil {
			cacheByCpod[c.CpodId] = make(map[string]struct{})
		}
		cacheByCpod[c.CpodId][c.DataId] = struct{}{}
	}

	clusterNodes := make(map[string][]NodeSnapshot)
	for _, n := range nodes {
		if _, banned := bannedCpodIDs[n.CpodId]; banned {
			continue
		}
		clusterNodes[n.CpodId] = append(clusterNodes[n.CpodId], NodeSnapshot{
			ID:             n.Id,
			CpodID:         n.CpodId,
			NodeName:       n.NodeName,
			GPUProduct:     n.GpuProd,
			GPUAllocatable: n.GpuAllocatable,
			GPUTotal:       n.GpuTotal,
			CPUAllocatable: n.CpuAllocatable,
			MemAllocatable: n.MemAllocatable,
			UpdatedAt:      n.UpdatedAt,
		})
	}

	out := make([]ClusterSnapshot, 0, len(clusterNodes))
	for cpodID, nodeList := range clusterNodes {
		cs := ClusterSnapshot{
			CpodID:   cpodID,
			Nodes:    nodeList,
			CacheIDs: cacheByCpod[cpodID],
		}
		if cs.CacheIDs == nil {
			cs.CacheIDs = make(map[string]struct{})
		}
		if len(nodeList) > 0 && gpuPrice != nil {
			cs.GPUPricePerHour = gpuPrice[nodeList[0].GPUProduct]
		}
		out = append(out, cs)
	}
	return out
}

// ApplyPlacement mutates snapshots after a successful assignment (optimistic portal ledger).
func ApplyPlacement(clusters []ClusterSnapshot, p Placement, w Workload) {
	if !p.OK {
		return
	}
	for i := range clusters {
		if clusters[i].CpodID != p.Candidate.Cluster.CpodID {
			continue
		}
		for j := range clusters[i].Nodes {
			if clusters[i].Nodes[j].ID != p.Candidate.Node.ID {
				continue
			}
			clusters[i].Nodes[j].GPUAllocatable -= w.GPUCount
			clusters[i].Nodes[j].CPUAllocatable -= w.CPUCores
			clusters[i].Nodes[j].MemAllocatable -= w.MemBytes
			return
		}
	}
}

// FreshNodeCutoff returns the oldest updated_at still considered alive.
func FreshNodeCutoff(window time.Duration) time.Time {
	return time.Now().Add(-window)
}
