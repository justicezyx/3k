package designsim

import (
	"fmt"
	"math"

	"sxwl/3k/internal/scheduler/schedule"
)

// OnlineMetrics summarizes a sequential scheduling run.
type OnlineMetrics struct {
	Placed            int
	Infeasible        int
	CacheHits         int // full cache locality (all CacheIDs present on chosen CPod)
	MaxClusterUtil    float64
	UtilVariance      float64
	Decisions         []PlacementResult
}

// RunOnline schedules jobs in order on a mutable cluster snapshot.
func RunOnline(policy PolicyName, initial []schedule.ClusterSnapshot, jobs []GangSpec, weights schedule.Weights) OnlineMetrics {
	clusters := CloneClusters(initial)
	var m OnlineMetrics
	for _, g := range jobs {
		p := Place(policy, g, clusters, weights)
		m.Decisions = append(m.Decisions, p)
		if !p.OK {
			m.Infeasible++
			continue
		}
		m.Placed++
		if cacheHit(g, p.CpodID, initial) {
			m.CacheHits++
		}
		CommitPlacement(policy, g, clusters, p)
	}
	m.MaxClusterUtil, m.UtilVariance = fleetUtilStats(clusters)
	return m
}

func cacheHit(g GangSpec, cpodID string, initial []schedule.ClusterSnapshot) bool {
	if len(g.CacheIDs) == 0 {
		return true
	}
	for _, c := range initial {
		if c.CpodID != cpodID {
			continue
		}
		hits := 0
		for _, id := range g.CacheIDs {
			if _, ok := c.CacheIDs[id]; ok {
				hits++
			}
		}
		return hits == len(g.CacheIDs)
	}
	return false
}

func fleetUtilStats(clusters []schedule.ClusterSnapshot) (maxUtil, varUtil float64) {
	var utils []float64
	for _, c := range clusters {
		var total, alloc int64
		for _, n := range c.Nodes {
			total += n.GPUTotal
			alloc += n.GPUAllocatable
		}
		if total == 0 {
			continue
		}
		u := 1 - float64(alloc)/float64(total)
		utils = append(utils, u)
		if u > maxUtil {
			maxUtil = u
		}
	}
	if len(utils) == 0 {
		return 0, 0
	}
	mean := 0.0
	for _, u := range utils {
		mean += u
	}
	mean /= float64(len(utils))
	var sumSq float64
	for _, u := range utils {
		d := u - mean
		sumSq += d * d
	}
	return maxUtil, sumSq / float64(len(utils))
}

// CompareOrderings runs the same multiset of jobs in two orders.
func CompareOrderings(policy PolicyName, initial []schedule.ClusterSnapshot, orderA, orderB []GangSpec, weights schedule.Weights) (OnlineMetrics, OnlineMetrics) {
	return RunOnline(policy, initial, orderA, weights), RunOnline(policy, initial, orderB, weights)
}

func FormatPlacement(p PlacementResult) string {
	if !p.OK {
		return "INFEASIBLE"
	}
	if p.NodeName != "" {
		return fmt.Sprintf("%s/%s score=%d", p.CpodID, p.NodeName, p.Total)
	}
	return fmt.Sprintf("%s (gang) score=%d", p.CpodID, p.Total)
}

// WeightSweepResult is one weight vector evaluation across a scenario batch.
type WeightSweepResult struct {
	Weights schedule.Weights
	Score   float64 // higher is better composite
}

// CompositeObjective for weight tuning: cache hit rate - penalty for max util skew.
func CompositeObjective(m OnlineMetrics, jobCount int) float64 {
	if jobCount == 0 {
		return 0
	}
	cacheRate := float64(m.CacheHits) / (float64(m.Placed) + 1e-9)
	return cacheRate*100 - m.MaxClusterUtil*50 - math.Sqrt(m.UtilVariance)*20 - float64(m.Infeasible)*100
}
