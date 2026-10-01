package schedule

import (
	"testing"
)

func TestScore_prefersCacheAndHeadroom(t *testing.T) {
	clusters := []ClusterSnapshot{
		{
			CpodID: "cpod-a",
			Nodes: []NodeSnapshot{{
				ID: 1, CpodID: "cpod-a", NodeName: "n1", GPUProduct: "H100",
				GPUAllocatable: 4, GPUTotal: 8, CPUAllocatable: 32,
			}},
			CacheIDs: map[string]struct{}{},
		},
		{
			CpodID: "cpod-b",
			Nodes: []NodeSnapshot{{
				ID: 2, CpodID: "cpod-b", NodeName: "n1", GPUProduct: "H100",
				GPUAllocatable: 8, GPUTotal: 8, CPUAllocatable: 32,
			}},
			CacheIDs:        map[string]struct{}{"model-llama": {}},
			GPUPricePerHour: 10,
		},
	}
	w := Workload{
		GPUProduct: "H100", GPUCount: 1, CPUCores: 1,
		CacheIDs: []string{"model-llama"},
	}
	p := Score(w, clusters, DefaultWeights())
	if !p.OK {
		t.Fatal("expected feasible placement")
	}
	if p.Candidate.Cluster.CpodID != "cpod-b" {
		t.Fatalf("expected cpod-b, got %s score=%d", p.Candidate.Cluster.CpodID, p.TotalScore)
	}
}

func TestScoreForCluster_rejectsNonWinner(t *testing.T) {
	clusters := []ClusterSnapshot{
		{CpodID: "cpod-a", Nodes: []NodeSnapshot{{
			ID: 1, GPUProduct: "H100", GPUAllocatable: 8, GPUTotal: 8, CPUAllocatable: 8,
		}}},
		{CpodID: "cpod-b", Nodes: []NodeSnapshot{{
			ID: 2, GPUProduct: "H100", GPUAllocatable: 2, GPUTotal: 8, CPUAllocatable: 8,
		}}},
	}
	w := Workload{GPUProduct: "H100", GPUCount: 1, CPUCores: 1}
	p := ScoreForCluster(w, clusters, DefaultWeights(), "cpod-b")
	if p.OK {
		t.Fatal("cpod-b should lose on headroom vs cpod-a")
	}
}

func TestScore_pinCluster(t *testing.T) {
	clusters := []ClusterSnapshot{
		{CpodID: "cpod-a", Nodes: []NodeSnapshot{{ID: 1, GPUProduct: "H100", GPUAllocatable: 8, GPUTotal: 8, CPUAllocatable: 8}}},
		{CpodID: "cpod-b", Nodes: []NodeSnapshot{{ID: 2, GPUProduct: "H100", GPUAllocatable: 8, GPUTotal: 8, CPUAllocatable: 8}}},
	}
	w := Workload{GPUProduct: "H100", GPUCount: 1, CPUCores: 1, PinCluster: "cpod-b"}
	p := Score(w, clusters, DefaultWeights())
	if !p.OK || p.Candidate.Cluster.CpodID != "cpod-b" {
		t.Fatalf("expected pinned cpod-b, got %+v", p)
	}
}

func TestScore_noFeasibleGPU(t *testing.T) {
	clusters := []ClusterSnapshot{{
		CpodID: "c1",
		Nodes:  []NodeSnapshot{{ID: 1, GPUProduct: "A100", GPUAllocatable: 0, GPUTotal: 8, CPUAllocatable: 8}},
	}}
	w := Workload{GPUProduct: "H100", GPUCount: 1, CPUCores: 1}
	if p := Score(w, clusters, DefaultWeights()); p.OK {
		t.Fatal("expected infeasible")
	}
}

func TestScore_emptyClusters(t *testing.T) {
	if p := Score(Workload{GPUCount: 1}, nil, DefaultWeights()); p.OK {
		t.Fatal("expected no placement")
	}
}

func TestScore_cpuAndMemoryConstraints(t *testing.T) {
	clusters := []ClusterSnapshot{{
		CpodID: "c1",
		Nodes: []NodeSnapshot{
			{ID: 1, NodeName: "low-mem", GPUProduct: "G", GPUAllocatable: 4, GPUTotal: 4, CPUAllocatable: 8, MemAllocatable: 100},
			{ID: 2, NodeName: "ok", GPUProduct: "G", GPUAllocatable: 4, GPUTotal: 4, CPUAllocatable: 8, MemAllocatable: 1000},
		},
	}}
	w := Workload{GPUProduct: "G", GPUCount: 1, CPUCores: 4, MemBytes: 500}
	p := Score(w, clusters, Weights{ResourceHeadroom: 1})
	if !p.OK || p.Candidate.Node.NodeName != "ok" {
		t.Fatalf("expected node ok, got %+v", p.Candidate.Node)
	}
}

func TestScore_gpuTypeOptional(t *testing.T) {
	clusters := []ClusterSnapshot{{
		CpodID: "c1",
		Nodes:  []NodeSnapshot{{ID: 1, GPUProduct: "G", GPUAllocatable: 2, GPUTotal: 4, CPUAllocatable: 4}},
	}}
	w := Workload{GPUProduct: "", GPUCount: 0, CPUCores: 1}
	p := Score(w, clusters, DefaultWeights())
	if !p.OK {
		t.Fatal("expected placement without gpu requirement")
	}
}

func TestNormalizeScores_flatNonZero(t *testing.T) {
	got := normalizeScores([]int64{10, 10, 10})
	for i, v := range got {
		if v != MaxNodeScore {
			t.Fatalf("index %d: want %d got %d", i, MaxNodeScore, v)
		}
	}
}

func TestNormalizeScores_flatZero(t *testing.T) {
	got := normalizeScores([]int64{0, 0})
	for i, v := range got {
		if v != 0 {
			t.Fatalf("index %d: want 0 got %d", i, v)
		}
	}
}

func TestNormalizeScores_spread(t *testing.T) {
	got := normalizeScores([]int64{0, 50, 100})
	want := []int64{0, 50, 100}
	for i := range want {
		if got[i] != want[i] {
			t.Fatalf("index %d: want %d got %d", i, want[i], got[i])
		}
	}
}

func TestNormalizeScores_empty(t *testing.T) {
	if normalizeScores(nil) != nil {
		t.Fatal("expected nil")
	}
}

func TestScore_zeroWeightsStillPicksDeterministically(t *testing.T) {
	clusters := []ClusterSnapshot{
		{CpodID: "b", Nodes: []NodeSnapshot{{ID: 1, GPUProduct: "G", GPUAllocatable: 4, GPUTotal: 4, CPUAllocatable: 4}}},
		{CpodID: "a", Nodes: []NodeSnapshot{{ID: 2, GPUProduct: "G", GPUAllocatable: 4, GPUTotal: 4, CPUAllocatable: 4}}},
	}
	w := Workload{GPUProduct: "G", GPUCount: 1, CPUCores: 1}
	p := Score(w, clusters, Weights{})
	if !p.OK {
		t.Fatal("expected placement")
	}
	if p.Candidate.Cluster.CpodID != "a" {
		t.Fatalf("tie-break should pick lexicographic cpod a, got %s", p.Candidate.Cluster.CpodID)
	}
}

func TestScore_tieBreakByNodeName(t *testing.T) {
	clusters := []ClusterSnapshot{{
		CpodID: "c1",
		Nodes: []NodeSnapshot{
			{ID: 1, NodeName: "z", GPUProduct: "G", GPUAllocatable: 4, GPUTotal: 4, CPUAllocatable: 4},
			{ID: 2, NodeName: "a", GPUProduct: "G", GPUAllocatable: 4, GPUTotal: 4, CPUAllocatable: 4},
		},
	}}
	w := Workload{GPUProduct: "G", GPUCount: 1, CPUCores: 1}
	p := Score(w, clusters, Weights{})
	if p.Candidate.Node.NodeName != "a" {
		t.Fatalf("expected node a, got %s", p.Candidate.Node.NodeName)
	}
}

func TestScore_singleDimensionHeadroomOnly(t *testing.T) {
	clusters := []ClusterSnapshot{
		{CpodID: "tight", Nodes: []NodeSnapshot{{ID: 1, GPUProduct: "G", GPUAllocatable: 2, GPUTotal: 8, CPUAllocatable: 4}}},
		{CpodID: "wide", Nodes: []NodeSnapshot{{ID: 2, GPUProduct: "G", GPUAllocatable: 8, GPUTotal: 8, CPUAllocatable: 4}}},
	}
	w := Workload{GPUProduct: "G", GPUCount: 1, CPUCores: 1}
	p := Score(w, clusters, Weights{ResourceHeadroom: 1})
	if p.Candidate.Cluster.CpodID != "wide" {
		t.Fatalf("expected wide cluster, got %s", p.Candidate.Cluster.CpodID)
	}
}

func TestScore_costPrefersCheaperWhenOtherEqual(t *testing.T) {
	clusters := []ClusterSnapshot{
		{CpodID: "expensive", Nodes: []NodeSnapshot{{ID: 1, GPUProduct: "G", GPUAllocatable: 8, GPUTotal: 8, CPUAllocatable: 4}}, GPUPricePerHour: 100},
		{CpodID: "cheap", Nodes: []NodeSnapshot{{ID: 2, GPUProduct: "G", GPUAllocatable: 8, GPUTotal: 8, CPUAllocatable: 4}}, GPUPricePerHour: 1},
	}
	w := Workload{GPUProduct: "G", GPUCount: 1, CPUCores: 1}
	p := Score(w, clusters, Weights{Cost: 1})
	if p.Candidate.Cluster.CpodID != "cheap" {
		t.Fatalf("expected cheap cluster, got %s", p.Candidate.Cluster.CpodID)
	}
}

func TestScore_cacheLocalityAllOrNothing(t *testing.T) {
	clusters := []ClusterSnapshot{
		{CpodID: "none", Nodes: []NodeSnapshot{{ID: 1, GPUProduct: "G", GPUAllocatable: 4, GPUTotal: 4, CPUAllocatable: 4}}, CacheIDs: map[string]struct{}{}},
		{CpodID: "half", Nodes: []NodeSnapshot{{ID: 2, GPUProduct: "G", GPUAllocatable: 4, GPUTotal: 4, CPUAllocatable: 4}}, CacheIDs: map[string]struct{}{"a": {}}},
	}
	w := Workload{GPUProduct: "G", GPUCount: 1, CPUCores: 1, CacheIDs: []string{"a", "b"}}
	if scoreCacheLocality(w, Candidate{Cluster: clusters[1]}) != 50 {
		t.Fatalf("expected 50%% cache score, got %d", scoreCacheLocality(w, Candidate{Cluster: clusters[1]}))
	}
}

func TestScore_deterministicRepeatedCalls(t *testing.T) {
	clusters := testClusterGrid(3, 2)
	w := Workload{GPUProduct: "G", GPUCount: 1, CPUCores: 1, CacheIDs: []string{"m1"}}
	first := Score(w, clusters, DefaultWeights())
	for i := 0; i < 20; i++ {
		p := Score(w, clusters, DefaultWeights())
		if p.Candidate.Node.ID != first.Candidate.Node.ID || p.TotalScore != first.TotalScore {
			t.Fatalf("iteration %d: non-deterministic result", i)
		}
	}
}

func TestScoreForCluster_winnerGetsFullBreakdown(t *testing.T) {
	clusters := []ClusterSnapshot{
		{CpodID: "win", Nodes: []NodeSnapshot{{ID: 1, GPUProduct: "G", GPUAllocatable: 8, GPUTotal: 8, CPUAllocatable: 4}}},
	}
	w := Workload{GPUProduct: "G", GPUCount: 1, CPUCores: 1}
	p := ScoreForCluster(w, clusters, DefaultWeights(), "win")
	if !p.OK || len(p.Breakdown) == 0 {
		t.Fatalf("expected breakdown on success, got %+v", p)
	}
}

func TestCacheHitRatio(t *testing.T) {
	c := ClusterSnapshot{CacheIDs: map[string]struct{}{"x": {}, "y": {}}}
	if cacheHitRatio(c, nil) != 1 {
		t.Fatal("empty required should be 1")
	}
	if cacheHitRatio(c, []string{"x", "z"}) != 0.5 {
		t.Fatal("expected half hit")
	}
}

func testClusterGrid(cpods, nodesPer int) []ClusterSnapshot {
	out := make([]ClusterSnapshot, 0, cpods)
	for i := 0; i < cpods; i++ {
		cs := ClusterSnapshot{CpodID: string(rune('a' + i)), CacheIDs: map[string]struct{}{}}
		for j := 0; j < nodesPer; j++ {
			cs.Nodes = append(cs.Nodes, NodeSnapshot{
				ID: int64(i*10 + j), CpodID: cs.CpodID, NodeName: "n",
				GPUProduct: "G", GPUAllocatable: 4, GPUTotal: 8, CPUAllocatable: 8,
			})
		}
		out = append(out, cs)
	}
	return out
}
