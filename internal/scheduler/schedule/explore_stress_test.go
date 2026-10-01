package schedule

import (
	"fmt"
	"math"
	"math/rand"
	"testing"
)

// Exploratory stress tests: surface degenerate / suboptimal behaviors of the fixed scoring policy.
// These tests log findings and assert only invariants (determinism, feasibility), not optimality.

func TestExploreStress_randomSequentialPlacements(t *testing.T) {
	if testing.Short() {
		t.Skip("skip exploratory stress in -short")
	}
	const (
		trials      = 200
		clusterN    = 5
		nodesPer    = 3
		jobsPerTrial = 12
	)
	rng := rand.New(rand.NewSource(42))
	weights := DefaultWeights()

	var (
		infeasibleJobs     int
		tieCount           int
		lexTieBreaks       int
		loadSkewSum        float64
		greedySuboptimal   int
		normCollapsedDims  int
		totalNormDims      int
		pollWouldStarve    int
	)

	for trial := 0; trial < trials; trial++ {
		clusters := randomClusters(rng, clusterN, nodesPer)
		for j := 0; j < jobsPerTrial; j++ {
			w := Workload{
				GPUProduct: "G",
				GPUCount:   1 + int64(rng.Intn(2)),
				CPUCores:   1,
				CacheIDs:   randomCacheNeed(rng),
			}
			p := Score(w, clusters, weights)
			if !p.OK {
				infeasibleJobs++
				continue
			}
			if countTiedTotals(w, clusters, weights, p.TotalScore) > 1 {
				tieCount++
				if p.Candidate.Cluster.CpodID == lexMinFeasibleCpod(w, clusters) {
					lexTieBreaks++
				}
			}
			for _, b := range p.Breakdown {
				totalNormDims++
				if b.Normalized == MaxNodeScore && b.Score > 0 {
					// may be collapse: many candidates share same raw → all norm 100
					if dimensionCollapsed(w, clusters, weights, b.Name) {
						normCollapsedDims++
					}
				}
			}
			loadSkewSum += clusterLoadSkew(clusters)
			if betterHeadroomExists(w, clusters, p) {
				greedySuboptimal++
			}
			if !pollWinnerMatchesGlobal(w, clusters, weights, rng) {
				pollWouldStarve++
			}
			ApplyPlacement(clusters, p, w)
		}
	}

	t.Logf("trials=%d jobs=%d infeasible=%d (%.1f%%)",
		trials, trials*jobsPerTrial, infeasibleJobs, pct(infeasibleJobs, trials*jobsPerTrial))
	t.Logf("ties on total score: %d (%.1f%% of feasible)", tieCount, pct(tieCount, trials*jobsPerTrial-infeasibleJobs))
	t.Logf("ties resolved by lexicographic cpod_id (when tied): %d", lexTieBreaks)
	t.Logf("normalization collapse (flat raw across feasible): %d / %d dimensions (%.1f%%)",
		normCollapsedDims, totalNormDims, pct(normCollapsedDims, totalNormDims))
	t.Logf("single-step headroom-dominated better node existed: %d (%.1f%% of feasible)",
		greedySuboptimal, pct(greedySuboptimal, trials*jobsPerTrial-infeasibleJobs))
	t.Logf("avg cluster load skew (stdev of util): %.3f", loadSkewSum/float64(trials*jobsPerTrial-infeasibleJobs+1))
	t.Logf("random polling CPod != global winner: %d / %d simulations (%.1f%%) — pull-model starvation risk",
		pollWouldStarve, trials*jobsPerTrial, pct(pollWouldStarve, trials*jobsPerTrial))

	if tieCount > 0 && lexTieBreaks == tieCount {
		t.Log("DEGENERACY: every score tie is broken by cpod_id lex order — low IDs systematically favored")
	}
	if pct(normCollapsedDims, totalNormDims) > 40 {
		t.Log("DEGENERACY: normalization often assigns MaxNodeScore to all candidates — dimension stops discriminating")
	}
	if pct(pollWouldStarve, trials*jobsPerTrial) > 70 {
		t.Log("DEGENERACY: most jobs would not bind on a random polling CPod; losers wait for winner poll")
	}
}

func TestExploreStress_greedyVsBruteForceSmall(t *testing.T) {
	if testing.Short() {
		t.Skip("skip exploratory stress in -short")
	}
	// Two single-node clusters, 4 GPU each; place two 2-GPU jobs.
	// Optimal spread: one job per cluster (minimize max utilization).
	// Greedy scoring may stack both on the cluster with better cache/load/cost signals.
	clusters := []ClusterSnapshot{
		{
			CpodID: "cached_busy",
			Nodes:  []NodeSnapshot{{ID: 1, GPUProduct: "G", GPUAllocatable: 4, GPUTotal: 4, CPUAllocatable: 4}},
			CacheIDs: map[string]struct{}{"m": {}},
		},
		{
			CpodID: "empty",
			Nodes:  []NodeSnapshot{{ID: 2, GPUProduct: "G", GPUAllocatable: 4, GPUTotal: 4, CPUAllocatable: 4}},
			CacheIDs: map[string]struct{}{},
		},
	}
	jobs := []Workload{
		{GPUProduct: "G", GPUCount: 2, CPUCores: 1, CacheIDs: []string{"m"}},
		{GPUProduct: "G", GPUCount: 2, CPUCores: 1, CacheIDs: []string{"m"}},
	}
	weights := DefaultWeights()

	greedyMaxUtil := simulateGreedy(clusters, jobs, weights)
	optimalMaxUtil := bruteForceMinMaxUtil(clusters, jobs)
	t.Logf("greedy max cluster GPU util=%.2f optimal max util=%.2f gap=%.2f",
		greedyMaxUtil, optimalMaxUtil, greedyMaxUtil-optimalMaxUtil)

	if greedyMaxUtil > optimalMaxUtil+1e-9 {
		t.Log("DEGENERACY: sequential Score+Apply is suboptimal for bin-packing — cache/load/cost can stack jobs on one CPod")
	}
}

func TestExploreStress_loadBalanceFavorsAlreadyIdle(t *testing.T) {
	// load_balance scores higher when cluster has MORE free GPU — "rich get richer"
	hot := ClusterSnapshot{
		CpodID: "hot",
		Nodes:  []NodeSnapshot{{ID: 1, GPUProduct: "G", GPUAllocatable: 1, GPUTotal: 8, CPUAllocatable: 4}},
	}
	cold := ClusterSnapshot{
		CpodID: "cold",
		Nodes:  []NodeSnapshot{{ID: 2, GPUProduct: "G", GPUAllocatable: 7, GPUTotal: 8, CPUAllocatable: 4}},
	}
	w := Workload{GPUProduct: "G", GPUCount: 1, CPUCores: 1}
	p := Score(w, []ClusterSnapshot{hot, cold}, Weights{LoadBalance: 1})
	if p.Candidate.Cluster.CpodID != "cold" {
		t.Fatalf("expected cold cluster, got %s", p.Candidate.Cluster.CpodID)
	}
	t.Log("DEGENERACY: load_balance alone always prefers the emptier cluster — cannot rebalance toward hot cluster")
}

func TestExploreStress_cacheVsHeadroomConflict(t *testing.T) {
	clusters := []ClusterSnapshot{
		{
			CpodID: "cached_tight",
			Nodes:  []NodeSnapshot{{ID: 1, GPUProduct: "G", GPUAllocatable: 2, GPUTotal: 8, CPUAllocatable: 4}},
			CacheIDs: map[string]struct{}{"m": {}},
		},
		{
			CpodID: "fresh_wide",
			Nodes:  []NodeSnapshot{{ID: 2, GPUProduct: "G", GPUAllocatable: 8, GPUTotal: 8, CPUAllocatable: 4}},
		},
	}
	w := Workload{GPUProduct: "G", GPUCount: 1, CPUCores: 1, CacheIDs: []string{"m"}}
	pDefault := Score(w, clusters, DefaultWeights())
	pCacheOnly := Score(w, clusters, Weights{CacheLocality: 1})
	pHeadOnly := Score(w, clusters, Weights{ResourceHeadroom: 1})
	t.Logf("default winner=%s cache-only=%s headroom-only=%s",
		pDefault.Candidate.Cluster.CpodID,
		pCacheOnly.Candidate.Cluster.CpodID,
		pHeadOnly.Candidate.Cluster.CpodID)
	if pCacheOnly.Candidate.Cluster.CpodID == pHeadOnly.Candidate.Cluster.CpodID {
		t.Fatal("expected different winners under single-dimension weights")
	}
	t.Log("DEGENERACY: default weights can split cache vs headroom — tuning shifts winners discontinuously")
}

// --- simulation helpers ---

func randomClusters(rng *rand.Rand, cpods, nodesPer int) []ClusterSnapshot {
	out := make([]ClusterSnapshot, cpods)
	for i := 0; i < cpods; i++ {
		id := fmt.Sprintf("cpod-%02d", i)
		cs := ClusterSnapshot{
			CpodID:          id,
			CacheIDs:        map[string]struct{}{},
			GPUPricePerHour: float64(1 + rng.Intn(20)),
		}
		if rng.Float64() < 0.4 {
			cs.CacheIDs["m"] = struct{}{}
		}
		for j := 0; j < nodesPer; j++ {
			total := int64(4 + rng.Intn(5))
			alloc := int64(rng.Intn(int(total) + 1))
			cs.Nodes = append(cs.Nodes, NodeSnapshot{
				ID: int64(i*100 + j), CpodID: id, NodeName: fmt.Sprintf("n%d", j),
				GPUProduct: "G", GPUTotal: total, GPUAllocatable: alloc, CPUAllocatable: 16,
			})
		}
		out[i] = cs
	}
	return out
}

func randomCacheNeed(rng *rand.Rand) []string {
	if rng.Float64() < 0.5 {
		return nil
	}
	return []string{"m"}
}

func pct(num, den int) float64 {
	if den == 0 {
		return 0
	}
	return 100 * float64(num) / float64(den)
}

func countTiedTotals(w Workload, clusters []ClusterSnapshot, weights Weights, total int64) int {
	n := 0
	for _, c := range enumerateCandidates(clusters) {
		if !feasibleCandidate(w, c) {
			continue
		}
		if placementTotal(w, c, clusters, weights) == total {
			n++
		}
	}
	return n
}

func placementTotal(w Workload, target Candidate, clusters []ClusterSnapshot, weights Weights) int64 {
	feasible := make([]Candidate, 0)
	for _, c := range enumerateCandidates(clusters) {
		if feasibleCandidate(w, c) {
			feasible = append(feasible, c)
		}
	}
	dims := scoreDimensions(weights)
	var total int64
	for _, dim := range dims {
		if dim.weight <= 0 {
			continue
		}
		raw := make([]int64, len(feasible))
		for i, c := range feasible {
			raw[i] = dim.score(w, c)
		}
		norm := normalizeScores(raw)
		for i, c := range feasible {
			if c.Node.ID == target.Node.ID && c.Cluster.CpodID == target.Cluster.CpodID {
				total += norm[i] * dim.weight
				break
			}
		}
	}
	return total
}

func lexMinFeasibleCpod(w Workload, clusters []ClusterSnapshot) string {
	var min string
	for _, c := range enumerateCandidates(clusters) {
		if !feasibleCandidate(w, c) {
			continue
		}
		id := c.Cluster.CpodID
		if min == "" || id < min {
			min = id
		}
	}
	return min
}

func dimensionCollapsed(w Workload, clusters []ClusterSnapshot, weights Weights, dimName string) bool {
	feasible := make([]Candidate, 0)
	for _, c := range enumerateCandidates(clusters) {
		if feasibleCandidate(w, c) {
			feasible = append(feasible, c)
		}
	}
	if len(feasible) < 2 {
		return false
	}
	dims := scoreDimensions(weights)
	var scoreFn func(Workload, Candidate) int64
	for _, d := range dims {
		if d.name == dimName {
			scoreFn = d.score
			break
		}
	}
	if scoreFn == nil {
		return false
	}
	first := scoreFn(w, feasible[0])
	for _, c := range feasible[1:] {
		if scoreFn(w, c) != first {
			return false
		}
	}
	return first > 0
}

func clusterLoadSkew(clusters []ClusterSnapshot) float64 {
	utils := make([]float64, 0, len(clusters))
	for _, c := range clusters {
		total, alloc := clusterGPUStats(c)
		if total == 0 {
			continue
		}
		utils = append(utils, 1-float64(alloc)/float64(total))
	}
	if len(utils) < 2 {
		return 0
	}
	mean := 0.0
	for _, u := range utils {
		mean += u
	}
	mean /= float64(len(utils))
	var varSum float64
	for _, u := range utils {
		d := u - mean
		varSum += d * d
	}
	return math.Sqrt(varSum / float64(len(utils)))
}

func betterHeadroomExists(w Workload, clusters []ClusterSnapshot, p Placement) bool {
	winnerHead := headroomAfter(w, p.Candidate.Node)
	for _, c := range enumerateCandidates(clusters) {
		if !feasibleCandidate(w, c) {
			continue
		}
		if c.Node.ID == p.Candidate.Node.ID {
			continue
		}
		if headroomAfter(w, c.Node) > winnerHead+1e-9 {
			return true
		}
	}
	return false
}

func headroomAfter(w Workload, n NodeSnapshot) float64 {
	if n.GPUTotal <= 0 || w.GPUCount <= 0 {
		return 0
	}
	return float64(n.GPUAllocatable-w.GPUCount) / float64(n.GPUTotal)
}

func pollWinnerMatchesGlobal(w Workload, clusters []ClusterSnapshot, weights Weights, rng *rand.Rand) bool {
	global := Score(w, clusters, weights)
	if !global.OK {
		return true
	}
	pollID := clusters[rng.Intn(len(clusters))].CpodID
	poll := ScoreForCluster(w, clusters, weights, pollID)
	return poll.OK
}

func cloneClusters(in []ClusterSnapshot) []ClusterSnapshot {
	out := make([]ClusterSnapshot, len(in))
	for i, c := range in {
		out[i] = c
		out[i].Nodes = append([]NodeSnapshot(nil), c.Nodes...)
		if c.CacheIDs != nil {
			out[i].CacheIDs = make(map[string]struct{}, len(c.CacheIDs))
			for k, v := range c.CacheIDs {
				out[i].CacheIDs[k] = v
			}
		}
	}
	return out
}

func maxClusterGPUUtil(clusters []ClusterSnapshot) float64 {
	maxU := 0.0
	for _, c := range clusters {
		total, alloc := clusterGPUStats(c)
		if total == 0 {
			continue
		}
		u := 1 - float64(alloc)/float64(total)
		if u > maxU {
			maxU = u
		}
	}
	return maxU
}

func simulateGreedy(base []ClusterSnapshot, jobs []Workload, weights Weights) float64 {
	clusters := cloneClusters(base)
	for _, w := range jobs {
		p := Score(w, clusters, weights)
		if !p.OK {
			continue
		}
		ApplyPlacement(clusters, p, w)
	}
	return maxClusterGPUUtil(clusters)
}

func bruteForceMinMaxUtil(base []ClusterSnapshot, jobs []Workload) float64 {
	if len(jobs) > 10 {
		return math.MaxFloat64
	}
	best := math.MaxFloat64
	var dfs func(clusters []ClusterSnapshot, idx int)
	dfs = func(clusters []ClusterSnapshot, idx int) {
		if idx == len(jobs) {
			u := maxClusterGPUUtil(clusters)
			if u < best {
				best = u
			}
			return
		}
		w := jobs[idx]
		for _, c := range enumerateCandidates(clusters) {
			if !feasibleCandidate(w, c) {
				continue
			}
			next := cloneClusters(clusters)
			p := Placement{OK: true, Candidate: c}
			ApplyPlacement(next, p, w)
			dfs(next, idx+1)
		}
	}
	dfs(cloneClusters(base), 0)
	return best
}
