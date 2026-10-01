package schedule

import "sort"

// FeasibleCandidates returns all (cluster, node) pairs that satisfy hard constraints.
func FeasibleCandidates(workload Workload, clusters []ClusterSnapshot) []Candidate {
	var out []Candidate
	for _, cluster := range clusters {
		if workload.PinCluster != "" && cluster.CpodID != workload.PinCluster {
			continue
		}
		for _, node := range cluster.Nodes {
			if !nodeFeasible(workload, node) {
				continue
			}
			out = append(out, Candidate{Cluster: cluster, Node: node})
		}
	}
	return out
}

func nodeFeasible(w Workload, n NodeSnapshot) bool {
	if w.GPUProduct != "" {
		if n.GPUProduct != w.GPUProduct || n.GPUAllocatable < w.GPUCount {
			return false
		}
	}
	if w.CPUCores > 0 && n.CPUAllocatable < w.CPUCores {
		return false
	}
	if w.MemBytes > 0 && n.MemAllocatable < w.MemBytes {
		return false
	}
	return true
}

type rawFactors struct {
	resourceHeadroom float64
	loadBalance      float64
	cacheLocality    float64
	cost             float64
	hasCost          bool
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

func computeRawFactors(w Workload, c Candidate) rawFactors {
	node := c.Node
	cluster := c.Cluster

	var headroom float64
	if node.GPUTotal > 0 && w.GPUCount > 0 {
		after := float64(node.GPUAllocatable - w.GPUCount)
		headroom = after / float64(node.GPUTotal)
	} else if node.CPUAllocatable > 0 && w.CPUCores > 0 {
		after := float64(node.CPUAllocatable - w.CPUCores)
		headroom = after / float64(node.CPUAllocatable)
	} else {
		headroom = 1
	}

	total, alloc := clusterGPUStats(cluster)
	var loadBal float64
	if total > 0 {
		loadBal = float64(alloc) / float64(total)
	} else {
		loadBal = 0.5
	}

	locality := cacheHitRatio(cluster, w.CacheIDs)

	f := rawFactors{
		resourceHeadroom: headroom,
		loadBalance:      loadBal,
		cacheLocality:    locality,
	}
	if cluster.GPUPricePerHour > 0 {
		f.cost = cluster.GPUPricePerHour
		f.hasCost = true
	}
	return f
}

// Score selects the best candidate using weighted normalized scoring (WNS).
func Score(workload Workload, clusters []ClusterSnapshot, weights Weights) Placement {
	candidates := FeasibleCandidates(workload, clusters)
	if len(candidates) == 0 {
		return Placement{OK: false}
	}

	raws := make([]rawFactors, len(candidates))
	for i, c := range candidates {
		raws[i] = computeRawFactors(workload, c)
	}

	headroomVals := make([]float64, len(candidates))
	loadVals := make([]float64, len(candidates))
	cacheVals := make([]float64, len(candidates))
	costVals := make([]float64, 0)
	costIdx := make([]int, 0)
	for i, r := range raws {
		headroomVals[i] = r.resourceHeadroom
		loadVals[i] = r.loadBalance
		cacheVals[i] = r.cacheLocality
		if r.hasCost {
			costIdx = append(costIdx, i)
			costVals = append(costVals, r.cost)
		}
	}

	headroomNorm := normalizeHigherBetter(headroomVals)
	loadNorm := normalizeHigherBetter(loadVals)
	cacheNorm := normalizeHigherBetter(cacheVals)

	costNormAll := make([]float64, len(candidates))
	for i := range costNormAll {
		costNormAll[i] = 0.5
	}
	if len(costVals) > 0 {
		// Lower price is better: invert via max - x before normalizeHigherBetter.
		min, max := costVals[0], costVals[0]
		for _, v := range costVals[1:] {
			if v < min {
				min = v
			}
			if v > max {
				max = v
			}
		}
		inverted := make([]float64, len(costVals))
		for j, v := range costVals {
			inverted[j] = max - v + min // maps cheaper → larger
		}
		norm := normalizeHigherBetter(inverted)
		for j, idx := range costIdx {
			costNormAll[idx] = norm[j]
		}
	}

	type scored struct {
		idx   int
		total float64
	}
	scoredList := make([]scored, len(candidates))
	for i := range candidates {
		var sumW, sum float64
		add := func(name string, norm, w float64, raw float64, factors *[]FactorScore) {
			if w <= 0 {
				return
			}
			weighted := w * norm
			sumW += w
			sum += weighted
			*factors = append(*factors, FactorScore{
				Name: name, Raw: raw, Normalized: norm, Weight: w, Weighted: weighted,
			})
		}
		var factors []FactorScore
		add("resource_headroom", headroomNorm[i], weights.ResourceHeadroom, raws[i].resourceHeadroom, &factors)
		add("load_balance", loadNorm[i], weights.LoadBalance, raws[i].loadBalance, &factors)
		add("cache_locality", cacheNorm[i], weights.CacheLocality, raws[i].cacheLocality, &factors)
		if raws[i].hasCost {
			add("cost", costNormAll[i], weights.Cost, raws[i].cost, &factors)
		}
		total := 0.0
		if sumW > 0 {
			total = sum / sumW
		}
		scoredList[i] = scored{idx: i, total: total}
		_ = factors // factors attached on winner only below
	}

	sort.Slice(scoredList, func(i, j int) bool {
		if scoredList[i].total != scoredList[j].total {
			return scoredList[i].total > scoredList[j].total
		}
		ci, cj := candidates[scoredList[i].idx], candidates[scoredList[j].idx]
		if ci.Cluster.CpodID != cj.Cluster.CpodID {
			return ci.Cluster.CpodID < cj.Cluster.CpodID
		}
		return ci.Node.NodeName < cj.Node.NodeName
	})

	bestIdx := scoredList[0].idx
	best := candidates[bestIdx]

	// Rebuild factor breakdown for the winner (for logging / audit).
	var factors []FactorScore
	add := func(name string, norm, w float64, raw float64) {
		if w <= 0 {
			return
		}
		weighted := w * norm
		factors = append(factors, FactorScore{
			Name: name, Raw: raw, Normalized: norm, Weight: w, Weighted: weighted,
		})
	}
	add("resource_headroom", headroomNorm[bestIdx], weights.ResourceHeadroom, raws[bestIdx].resourceHeadroom)
	add("load_balance", loadNorm[bestIdx], weights.LoadBalance, raws[bestIdx].loadBalance)
	add("cache_locality", cacheNorm[bestIdx], weights.CacheLocality, raws[bestIdx].cacheLocality)
	if raws[bestIdx].hasCost {
		add("cost", costNormAll[bestIdx], weights.Cost, raws[bestIdx].cost)
	}

	var sumW, sum float64
	for _, f := range factors {
		sumW += f.Weight
		sum += f.Weighted
	}
	total := scoredList[0].total
	if sumW == 0 {
		total = 0
	}

	return Placement{
		OK:         true,
		Candidate:  best,
		TotalScore: total,
		Factors:    factors,
	}
}

// ScoreForCluster returns the global best placement only if it belongs to requestCpodID.
func ScoreForCluster(workload Workload, clusters []ClusterSnapshot, weights Weights, requestCpodID string) Placement {
	p := Score(workload, clusters, weights)
	if !p.OK || p.Candidate.Cluster.CpodID != requestCpodID {
		return Placement{OK: false, TotalScore: p.TotalScore, Factors: p.Factors}
	}
	return p
}
