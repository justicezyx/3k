package schedule

const (
	PluginCpodSelector     = "CpodSelector"
	PluginNodeResourcesFit = "NodeResourcesFit"
	PluginResourceHeadroom = "NodeResourcesBalancedAllocation"
	PluginLoadBalance      = "NodeResourcesLeastAllocated"
	PluginCacheLocality    = "CacheLocality"
	PluginNodeCost         = "NodeCost"
)

func defaultFilterPlugins() []FilterPlugin {
	return []FilterPlugin{
		&CpodSelectorFilter{},
		&NodeResourcesFitFilter{},
	}
}

func defaultScorePlugins(weights Weights) []ScorePluginWeight {
	wmap := weightsToPluginWeights(weights)
	return []ScorePluginWeight{
		{Plugin: &ResourceHeadroomScorePlugin{}, Weight: wmap[PluginResourceHeadroom]},
		{Plugin: &LoadBalanceScorePlugin{}, Weight: wmap[PluginLoadBalance]},
		{Plugin: &CacheLocalityScorePlugin{}, Weight: wmap[PluginCacheLocality]},
		{Plugin: &NodeCostScorePlugin{}, Weight: wmap[PluginNodeCost]},
	}
}

// --- Filter plugins (kube Filter extension point) ---

type CpodSelectorFilter struct{}

func (p *CpodSelectorFilter) Name() string { return PluginCpodSelector }

func (p *CpodSelectorFilter) Filter(_ *CycleState, workload Workload, candidate Candidate) *Status {
	if workload.PinCluster != "" && candidate.Cluster.CpodID != workload.PinCluster {
		return NewStatus(ReasonUnschedulable + ": cpod selector mismatch")
	}
	return nil
}

type NodeResourcesFitFilter struct{}

func (p *NodeResourcesFitFilter) Name() string { return PluginNodeResourcesFit }

func (p *NodeResourcesFitFilter) Filter(_ *CycleState, workload Workload, candidate Candidate) *Status {
	n := candidate.Node
	if workload.GPUProduct != "" {
		if n.GPUProduct != workload.GPUProduct {
			return NewStatus(ReasonUnschedulable + ": insufficient gpu product")
		}
		if n.GPUAllocatable < workload.GPUCount {
			return NewStatus(ReasonUnschedulable + ": insufficient gpu")
		}
	}
	if workload.CPUCores > 0 && n.CPUAllocatable < workload.CPUCores {
		return NewStatus(ReasonUnschedulable + ": insufficient cpu")
	}
	if workload.MemBytes > 0 && n.MemAllocatable < workload.MemBytes {
		return NewStatus(ReasonUnschedulable + ": insufficient memory")
	}
	return nil
}

// --- Score plugins (kube Score + NormalizeScore extension points) ---

type ResourceHeadroomScorePlugin struct{}

func (p *ResourceHeadroomScorePlugin) Name() string { return PluginResourceHeadroom }

func (p *ResourceHeadroomScorePlugin) Score(_ *CycleState, workload Workload, candidate Candidate) (int64, *Status) {
	n := candidate.Node
	var ratio float64
	if n.GPUTotal > 0 && workload.GPUCount > 0 {
		ratio = float64(n.GPUAllocatable-workload.GPUCount) / float64(n.GPUTotal)
	} else if n.CPUAllocatable > 0 && workload.CPUCores > 0 {
		ratio = float64(n.CPUAllocatable-workload.CPUCores) / float64(n.CPUAllocatable)
	} else {
		ratio = 1
	}
	if ratio < 0 {
		ratio = 0
	}
	return int64(ratio * float64(MaxNodeScore)), nil
}

type LoadBalanceScorePlugin struct{}

func (p *LoadBalanceScorePlugin) Name() string { return PluginLoadBalance }

// Score favors clusters with more free GPU capacity (similar intent to NodeResourcesLeastAllocated).
func (p *LoadBalanceScorePlugin) Score(_ *CycleState, _ Workload, candidate Candidate) (int64, *Status) {
	total, alloc := clusterGPUStats(candidate.Cluster)
	if total <= 0 {
		return MaxNodeScore / 2, nil
	}
	ratio := float64(alloc) / float64(total)
	return int64(ratio * float64(MaxNodeScore)), nil
}

type CacheLocalityScorePlugin struct{}

func (p *CacheLocalityScorePlugin) Name() string { return PluginCacheLocality }

func (p *CacheLocalityScorePlugin) Score(_ *CycleState, workload Workload, candidate Candidate) (int64, *Status) {
	return int64(cacheHitRatio(candidate.Cluster, workload.CacheIDs) * float64(MaxNodeScore)), nil
}

type NodeCostScorePlugin struct{}

func (p *NodeCostScorePlugin) Name() string { return PluginNodeCost }

// Score returns inverted price so cheaper clusters score higher before NormalizeScore.
func (p *NodeCostScorePlugin) Score(_ *CycleState, _ Workload, candidate Candidate) (int64, *Status) {
	price := candidate.Cluster.GPUPricePerHour
	if price <= 0 {
		return 0, nil
	}
	// Use inverse micro-units to keep int64; normalization spreads across candidates.
	return int64(1_000_000 / price), nil
}

func clusterGPUStats(cluster ClusterSnapshot) (total, alloc int64) {
	for _, n := range cluster.Nodes {
		total += n.GPUTotal
		alloc += n.GPUAllocatable
	}
	return total, alloc
}

func cacheHitRatio(cluster ClusterSnapshot, required []string) float64 {
	if len(required) == 0 {
		return 1
	}
	if len(cluster.CacheIDs) == 0 {
		return 0
	}
	var hits int
	for _, id := range required {
		if _, ok := cluster.CacheIDs[id]; ok {
			hits++
		}
	}
	return float64(hits) / float64(len(required))
}
