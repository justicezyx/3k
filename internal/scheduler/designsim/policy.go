package designsim

import (
	"fmt"
	"sort"

	"sxwl/3k/internal/scheduler/schedule"
)

// PolicyName identifies a scoring / placement design variant.
type PolicyName string

const (
	// PolicyBaselineShipped is current WNS: (CPod×Node), PortalGPUCount on one node, schedule.Score.
	PolicyBaselineShipped PolicyName = "baseline_shipped"
	// PolicyCPodGangBottleneck: filter gang-feasible CPods; score at CPod granularity with gang bottleneck headroom.
	PolicyCPodGangBottleneck PolicyName = "cpod_gang_bottleneck"
	// PolicyCPodBestNodeHeadroom: gang filter; headroom = best single-node headroom using PortalGPUCount (legacy shape).
	PolicyCPodBestNodeHeadroom PolicyName = "cpod_best_single_node_headroom"
	// PolicyCPodGangClusterSlack: gang filter; headroom = cluster-wide GPU slack after placing total GPUs.
	PolicyCPodGangClusterSlack PolicyName = "cpod_gang_cluster_slack"
)

// PlacementResult is one scheduling decision.
type PlacementResult struct {
	OK        bool
	Policy    PolicyName
	CpodID    string
	NodeName  string // baseline only; proposed CPod-only policies leave empty
	GangPack  *GangPlacement
	Total     int64
	Breakdown []schedule.ScoreBreakdown
}

type scoreDim struct {
	name   string
	weight int64
	raw    func(g GangSpec, c schedule.ClusterSnapshot) int64
}

func scaleWeight(v float64) int64 {
	if v <= 0 {
		return 0
	}
	return int64(v * 100)
}

func scoreDimensions(w schedule.Weights, headroom func(GangSpec, schedule.ClusterSnapshot) int64) []scoreDim {
	return []scoreDim{
		{name: "resource_headroom", weight: scaleWeight(w.ResourceHeadroom), raw: headroom},
		{name: "load_balance", weight: scaleWeight(w.LoadBalance), raw: func(_ GangSpec, c schedule.ClusterSnapshot) int64 {
			return schedule.ClusterLoadBalanceRaw(c)
		}},
		{name: "cache_locality", weight: scaleWeight(w.CacheLocality), raw: func(g GangSpec, c schedule.ClusterSnapshot) int64 {
			return schedule.ClusterCacheLocalityRaw(g.ToScheduleWorkload(), c)
		}},
		{name: "cost", weight: scaleWeight(w.Cost), raw: func(_ GangSpec, c schedule.ClusterSnapshot) int64 {
			return schedule.ClusterCostRaw(c)
		}},
	}
}

func cpodWeightedScore(g GangSpec, clusters []schedule.ClusterSnapshot, weights schedule.Weights, headroom func(GangSpec, schedule.ClusterSnapshot) int64) (best schedule.ClusterSnapshot, total int64, breakdown []schedule.ScoreBreakdown, ok bool) {
	var feasible []schedule.ClusterSnapshot
	for _, c := range clusters {
		if ClusterFitsGang(g, c) {
			feasible = append(feasible, c)
		}
	}
	if len(feasible) == 0 {
		return schedule.ClusterSnapshot{}, 0, nil, false
	}
	dims := scoreDimensions(weights, headroom)
	type scored struct {
		idx   int
		total int64
		br    []schedule.ScoreBreakdown
	}
	results := make([]scored, len(feasible))
	for i := range feasible {
		results[i] = scored{idx: i, br: make([]schedule.ScoreBreakdown, 0, len(dims))}
	}
	for _, dim := range dims {
		if dim.weight <= 0 {
			continue
		}
		raw := make([]int64, len(feasible))
		for i, c := range feasible {
			raw[i] = dim.raw(g, c)
		}
		norm := schedule.NormalizeScores(raw)
		for i := range feasible {
			weighted := norm[i] * dim.weight
			results[i].total += weighted
			results[i].br = append(results[i].br, schedule.ScoreBreakdown{
				Name:       dim.name,
				Score:      raw[i],
				Normalized: norm[i],
				Weight:     dim.weight,
				Weighted:   weighted,
			})
		}
	}
	sort.Slice(results, func(i, j int) bool {
		if results[i].total != results[j].total {
			return results[i].total > results[j].total
		}
		return feasible[results[i].idx].CpodID < feasible[results[j].idx].CpodID
	})
	b := results[0]
	return feasible[b.idx], b.total, b.br, true
}

func clusterSlackHeadroomRaw(g GangSpec, c schedule.ClusterSnapshot) int64 {
	if !ClusterFitsGang(g, c) {
		return 0
	}
	var total, alloc int64
	for _, n := range c.Nodes {
		total += n.GPUTotal
		alloc += n.GPUAllocatable
	}
	need := g.TotalGPUs()
	if total <= 0 {
		return schedule.MaxNodeScore / 2
	}
	ratio := float64(alloc-need) / float64(total)
	if ratio < 0 {
		ratio = 0
	}
	return int64(ratio * float64(schedule.MaxNodeScore))
}

// Place picks a cluster (and optionally node) for one gang workload.
func Place(policy PolicyName, g GangSpec, clusters []schedule.ClusterSnapshot, weights schedule.Weights) PlacementResult {
	switch policy {
	case PolicyBaselineShipped:
		w := g.ToScheduleWorkload()
		p := schedule.Score(w, clusters, weights)
		if !p.OK {
			return PlacementResult{OK: false, Policy: policy}
		}
		return PlacementResult{
			OK:        true,
			Policy:    policy,
			CpodID:    p.Candidate.Cluster.CpodID,
			NodeName:  p.Candidate.Node.NodeName,
			Total:     p.TotalScore,
			Breakdown: p.Breakdown,
		}
	case PolicyCPodGangBottleneck:
		c, total, br, ok := cpodWeightedScore(g, clusters, weights, GangBottleneckHeadroomRaw)
		if !ok {
			return PlacementResult{OK: false, Policy: policy}
		}
		return PlacementResult{
			OK: true, Policy: policy, CpodID: c.CpodID, Total: total, Breakdown: br,
			GangPack: FeasibleGangPack(g, c),
		}
	case PolicyCPodBestNodeHeadroom:
		headroom := func(g GangSpec, c schedule.ClusterSnapshot) int64 {
			return BestSingleNodeHeadroomRaw(g.ToScheduleWorkload(), c)
		}
		c, total, br, ok := cpodWeightedScore(g, clusters, weights, headroom)
		if !ok {
			return PlacementResult{OK: false, Policy: policy}
		}
		return PlacementResult{
			OK: true, Policy: policy, CpodID: c.CpodID, Total: total, Breakdown: br,
			GangPack: FeasibleGangPack(g, c),
		}
	case PolicyCPodGangClusterSlack:
		c, total, br, ok := cpodWeightedScore(g, clusters, weights, clusterSlackHeadroomRaw)
		if !ok {
			return PlacementResult{OK: false, Policy: policy}
		}
		return PlacementResult{
			OK: true, Policy: policy, CpodID: c.CpodID, Total: total, Breakdown: br,
			GangPack: FeasibleGangPack(g, c),
		}
	default:
		panic(fmt.Sprintf("unknown policy %q", policy))
	}
}

// CommitPlacement mutates cluster snapshots after a decision (in-request accounting).
func CommitPlacement(policy PolicyName, g GangSpec, clusters []schedule.ClusterSnapshot, p PlacementResult) {
	if !p.OK {
		return
	}
	w := g.ToScheduleWorkload()
	switch policy {
	case PolicyBaselineShipped:
		// Baseline uses portal single-node debit (entire GPUCount on chosen node).
		for i := range clusters {
			if clusters[i].CpodID != p.CpodID {
				continue
			}
			for j := range clusters[i].Nodes {
				if clusters[i].Nodes[j].NodeName != p.NodeName {
					continue
				}
				clusters[i].Nodes[j].GPUAllocatable -= w.GPUCount
				clusters[i].Nodes[j].CPUAllocatable -= w.CPUCores
				clusters[i].Nodes[j].MemAllocatable -= w.MemBytes
				return
			}
		}
	default:
		pack := p.GangPack
		if pack == nil {
			for i := range clusters {
				if clusters[i].CpodID == p.CpodID {
					pack = FeasibleGangPack(g, clusters[i])
					break
				}
			}
		}
		if pack == nil {
			return
		}
		for i := range clusters {
			if clusters[i].CpodID != p.CpodID {
				continue
			}
			for _, used := range pack.Nodes {
				for j := range clusters[i].Nodes {
					if clusters[i].Nodes[j].ID != used.ID {
						continue
					}
					clusters[i].Nodes[j].GPUAllocatable -= g.GPUsPerWorker
					clusters[i].Nodes[j].CPUAllocatable -= g.CPUCores
					clusters[i].Nodes[j].MemAllocatable -= g.MemBytes
				}
			}
			return
		}
	}
}
