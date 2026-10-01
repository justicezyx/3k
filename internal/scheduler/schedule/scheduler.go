package schedule

import "sort"

// MaxNodeScore is the per-dimension score ceiling after min-max normalization.
const MaxNodeScore int64 = 100

const (
	factorResourceHeadroom = "resource_headroom"
	factorLoadBalance      = "load_balance"
	factorCacheLocality    = "cache_locality"
	factorCost             = "cost"
)

// ScoreBreakdown is one scoring dimension after normalization (for logs / audit).
type ScoreBreakdown struct {
	Name       string
	Score      int64
	Normalized int64
	Weight     int64
	Weighted   int64
}

// Score selects the best (cluster, node) for workload.
func Score(workload Workload, clusters []ClusterSnapshot, weights Weights) Placement {
	return schedule(workload, clusters, weights)
}

// ScoreForCluster returns the global best placement only if it belongs to requestCpodID.
func ScoreForCluster(workload Workload, clusters []ClusterSnapshot, weights Weights, requestCpodID string) Placement {
	p := Score(workload, clusters, weights)
	if !p.OK || p.Candidate.Cluster.CpodID != requestCpodID {
		return Placement{OK: false, TotalScore: p.TotalScore}
	}
	return p
}

func schedule(workload Workload, clusters []ClusterSnapshot, weights Weights) Placement {
	candidates := enumerateCandidates(clusters)
	if len(candidates) == 0 {
		return Placement{OK: false}
	}

	feasible := make([]Candidate, 0, len(candidates))
	for _, c := range candidates {
		if feasibleCandidate(workload, c) {
			feasible = append(feasible, c)
		}
	}
	if len(feasible) == 0 {
		return Placement{OK: false}
	}

	dims := scoreDimensions(weights)
	type candidateScore struct {
		idx        int
		total      int64
		breakdowns []ScoreBreakdown
	}
	scored := make([]candidateScore, len(feasible))
	for i := range feasible {
		scored[i] = candidateScore{idx: i, breakdowns: make([]ScoreBreakdown, 0, len(dims))}
	}

	for _, dim := range dims {
		if dim.weight <= 0 {
			continue
		}
		raw := make([]int64, len(feasible))
		for i, c := range feasible {
			raw[i] = dim.score(workload, c)
		}
		normalized := normalizeScores(raw)
		for i := range feasible {
			weighted := normalized[i] * dim.weight
			scored[i].total += weighted
			scored[i].breakdowns = append(scored[i].breakdowns, ScoreBreakdown{
				Name:       dim.name,
				Score:      raw[i],
				Normalized: normalized[i],
				Weight:     dim.weight,
				Weighted:   weighted,
			})
		}
	}

	sort.Slice(scored, func(i, j int) bool {
		if scored[i].total != scored[j].total {
			return scored[i].total > scored[j].total
		}
		ci, cj := feasible[scored[i].idx], feasible[scored[j].idx]
		if ci.Cluster.CpodID != cj.Cluster.CpodID {
			return ci.Cluster.CpodID < cj.Cluster.CpodID
		}
		return ci.Node.NodeName < cj.Node.NodeName
	})

	best := scored[0]
	return Placement{
		OK:         true,
		Candidate:  feasible[best.idx],
		TotalScore: best.total,
		Breakdown:  best.breakdowns,
	}
}

type scoreDimension struct {
	name   string
	weight int64
	score  func(Workload, Candidate) int64
}

func scoreDimensions(w Weights) []scoreDimension {
	return []scoreDimension{
		{name: factorResourceHeadroom, weight: scaleWeight(w.ResourceHeadroom), score: scoreResourceHeadroom},
		{name: factorLoadBalance, weight: scaleWeight(w.LoadBalance), score: scoreLoadBalance},
		{name: factorCacheLocality, weight: scaleWeight(w.CacheLocality), score: scoreCacheLocality},
		{name: factorCost, weight: scaleWeight(w.Cost), score: scoreCost},
	}
}

func scaleWeight(v float64) int64 {
	if v <= 0 {
		return 0
	}
	return int64(v * 100)
}

func feasibleCandidate(workload Workload, candidate Candidate) bool {
	if workload.PinCluster != "" && candidate.Cluster.CpodID != workload.PinCluster {
		return false
	}
	n := candidate.Node
	if workload.GPUProduct != "" {
		if n.GPUProduct != workload.GPUProduct || n.GPUAllocatable < workload.GPUCount {
			return false
		}
	}
	if workload.CPUCores > 0 && n.CPUAllocatable < workload.CPUCores {
		return false
	}
	if workload.MemBytes > 0 && n.MemAllocatable < workload.MemBytes {
		return false
	}
	return true
}

func scoreResourceHeadroom(workload Workload, candidate Candidate) int64 {
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
	return int64(ratio * float64(MaxNodeScore))
}

func scoreLoadBalance(_ Workload, candidate Candidate) int64 {
	total, alloc := clusterGPUStats(candidate.Cluster)
	if total <= 0 {
		return MaxNodeScore / 2
	}
	return int64(float64(alloc) / float64(total) * float64(MaxNodeScore))
}

func scoreCacheLocality(workload Workload, candidate Candidate) int64 {
	return int64(cacheHitRatio(candidate.Cluster, workload.CacheIDs) * float64(MaxNodeScore))
}

func scoreCost(_ Workload, candidate Candidate) int64 {
	price := candidate.Cluster.GPUPricePerHour
	if price <= 0 {
		return 0
	}
	return int64(1_000_000 / price)
}

func enumerateCandidates(clusters []ClusterSnapshot) []Candidate {
	var out []Candidate
	for _, cluster := range clusters {
		for _, node := range cluster.Nodes {
			out = append(out, Candidate{Cluster: cluster, Node: node})
		}
	}
	return out
}

func normalizeScores(scores []int64) []int64 {
	if len(scores) == 0 {
		return nil
	}
	min, max := scores[0], scores[0]
	for _, s := range scores[1:] {
		if s < min {
			min = s
		}
		if s > max {
			max = s
		}
	}
	out := make([]int64, len(scores))
	for i, s := range scores {
		if max == min {
			if s > 0 {
				out[i] = MaxNodeScore
			} else {
				out[i] = 0
			}
			continue
		}
		out[i] = MaxNodeScore * (s - min) / (max - min)
	}
	return out
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
