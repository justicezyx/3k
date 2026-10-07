package designsim

import (
	"sort"

	"sxwl/3k/internal/scheduler/schedule"
)

// GangSpec is the scheduling demand for gang-style training (portal + CPodJob semantics).
type GangSpec struct {
	Name          string
	GPUProduct    string
	GPUsPerWorker int64
	WorkerCount   int64
	CPUCores      int64
	MemBytes      int64
	CacheIDs      []string
}

func (g GangSpec) TotalGPUs() int64 {
	if g.WorkerCount <= 0 {
		return g.GPUsPerWorker
	}
	return g.GPUsPerWorker * g.WorkerCount
}

// PortalGPUCount returns gpu_number as stored for global WNS today (single-node filter uses this as GPUCount).
func (g GangSpec) PortalGPUCount() int64 {
	return g.TotalGPUs()
}

func (g GangSpec) ToScheduleWorkload() schedule.Workload {
	return schedule.Workload{
		GPUProduct: g.GPUProduct,
		GPUCount:   g.PortalGPUCount(),
		CPUCores:   g.CPUCores,
		MemBytes:   g.MemBytes,
		CacheIDs:   append([]string(nil), g.CacheIDs...),
	}
}

// GangPlacement assigns one worker per selected node (typical multi-node PyTorchJob).
type GangPlacement struct {
	Nodes []schedule.NodeSnapshot
}

// FeasibleGangPack finds a greedy gang packing: WorkerCount nodes each with >= GPUsPerWorker allocatable.
func FeasibleGangPack(g GangSpec, cluster schedule.ClusterSnapshot) *GangPlacement {
	if g.GPUsPerWorker <= 0 {
		g.GPUsPerWorker = g.TotalGPUs()
		g.WorkerCount = 1
	}
	if g.WorkerCount <= 0 {
		g.WorkerCount = 1
	}
	nodes := append([]schedule.NodeSnapshot(nil), cluster.Nodes...)
	sort.Slice(nodes, func(i, j int) bool {
		if nodes[i].GPUProduct != nodes[j].GPUProduct {
			return nodes[i].GPUProduct < nodes[j].GPUProduct
		}
		return nodes[i].GPUAllocatable > nodes[j].GPUAllocatable
	})
	var picked []schedule.NodeSnapshot
	for _, n := range nodes {
		if g.GPUProduct != "" && n.GPUProduct != g.GPUProduct {
			continue
		}
		if n.GPUAllocatable < g.GPUsPerWorker {
			continue
		}
		if g.CPUCores > 0 && n.CPUAllocatable < g.CPUCores {
			continue
		}
		if g.MemBytes > 0 && n.MemAllocatable < g.MemBytes {
			continue
		}
		picked = append(picked, n)
		if int64(len(picked)) >= g.WorkerCount {
			return &GangPlacement{Nodes: picked[:g.WorkerCount]}
		}
	}
	return nil
}

func ClusterFitsGang(g GangSpec, cluster schedule.ClusterSnapshot) bool {
	return FeasibleGangPack(g, cluster) != nil
}

// GangBottleneckHeadroomRaw max-min post-placement GPU headroom over nodes used in the best greedy pack.
// Raw score in [0, schedule.MaxNodeScore].
func GangBottleneckHeadroomRaw(g GangSpec, cluster schedule.ClusterSnapshot) int64 {
	pack := FeasibleGangPack(g, cluster)
	if pack == nil {
		return 0
	}
	var bottleneck float64 = 1
	for _, n := range pack.Nodes {
		if n.GPUTotal <= 0 || g.GPUsPerWorker <= 0 {
			continue
		}
		ratio := float64(n.GPUAllocatable-g.GPUsPerWorker) / float64(n.GPUTotal)
		if ratio < bottleneck {
			bottleneck = ratio
		}
	}
	if bottleneck < 0 {
		bottleneck = 0
	}
	return int64(bottleneck * float64(schedule.MaxNodeScore))
}

// BestSingleNodeHeadroomRaw max over nodes that could fit PortalGPUCount on one node (current WNS headroom shape).
func BestSingleNodeHeadroomRaw(w schedule.Workload, cluster schedule.ClusterSnapshot) int64 {
	var best int64
	for _, n := range cluster.Nodes {
		c := schedule.Candidate{Cluster: cluster, Node: n}
		if !schedule.FeasibleCandidate(w, c) {
			continue
		}
		s := schedule.ScoreResourceHeadroom(w, c)
		if s > best {
			best = s
		}
	}
	return best
}
