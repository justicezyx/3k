package schedule

import "testing"

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
			CacheIDs: map[string]struct{}{"model-llama": {}},
			GPUPricePerHour: 10,
		},
	}

	w := Workload{
		ID: "job-1", Kind: WorkloadTraining,
		GPUProduct: "H100", GPUCount: 1, CPUCores: 1,
		CacheIDs: []string{"model-llama"},
	}

	p := Score(w, clusters, DefaultWeights())
	if !p.OK {
		t.Fatal("expected feasible placement")
	}
	if p.Candidate.Cluster.CpodID != "cpod-b" {
		t.Fatalf("expected cpod-b (cache + headroom), got %s score=%f", p.Candidate.Cluster.CpodID, p.TotalScore)
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

func TestNormalizeHigherBetter_flat(t *testing.T) {
	got := normalizeHigherBetter([]float64{3, 3, 3})
	for i, v := range got {
		if v != 0.5 {
			t.Fatalf("index %d: want 0.5 got %v", i, v)
		}
	}
}
