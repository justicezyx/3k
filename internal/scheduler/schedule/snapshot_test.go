package schedule

import (
	"testing"
	"time"

	"sxwl/3k/internal/scheduler/model"
)

func TestBuildClusterSnapshots_groupsAndBans(t *testing.T) {
	nodes := []*model.SysCpodNode{
		{Id: 1, CpodId: "c1", NodeName: "n1", GpuProd: "H100", GpuAllocatable: 4, GpuTotal: 8, CpuAllocatable: 16, MemAllocatable: 1e9, UpdatedAt: time.Now()},
		{Id: 2, CpodId: "c2", NodeName: "n1", GpuProd: "H100", GpuAllocatable: 2, GpuTotal: 8, CpuAllocatable: 16, MemAllocatable: 1e9, UpdatedAt: time.Now()},
	}
	caches := []*model.SysCpodCache{
		{CpodId: "c1", DataId: "model-a"},
	}
	banned := map[string]string{"c2": ""}
	price := map[string]float64{"H100": 12.5}

	snap := BuildClusterSnapshots(nodes, caches, banned, price)
	if len(snap) != 1 {
		t.Fatalf("expected 1 cluster after ban, got %d", len(snap))
	}
	if snap[0].CpodID != "c1" {
		t.Fatalf("expected c1, got %s", snap[0].CpodID)
	}
	if _, ok := snap[0].CacheIDs["model-a"]; !ok {
		t.Fatal("expected cache model-a")
	}
	if snap[0].GPUPricePerHour != 12.5 {
		t.Fatalf("expected price 12.5, got %f", snap[0].GPUPricePerHour)
	}
}

func TestBuildClusterSnapshots_emptyCacheMap(t *testing.T) {
	nodes := []*model.SysCpodNode{
		{Id: 1, CpodId: "c1", NodeName: "n1", GpuProd: "G", GpuAllocatable: 1, GpuTotal: 1, CpuAllocatable: 1, MemAllocatable: 1},
	}
	snap := BuildClusterSnapshots(nodes, nil, nil, nil)
	if snap[0].CacheIDs == nil || len(snap[0].CacheIDs) != 0 {
		t.Fatal("expected empty cache map")
	}
}

func TestApplyPlacement_mutatesNodeCapacity(t *testing.T) {
	clusters := []ClusterSnapshot{{
		CpodID: "c1",
		Nodes:  []NodeSnapshot{{ID: 7, GPUAllocatable: 8, CPUAllocatable: 16, MemAllocatable: 1000}},
	}}
	p := Placement{
		OK: true,
		Candidate: Candidate{
			Cluster: clusters[0],
			Node:    clusters[0].Nodes[0],
		},
	}
	w := Workload{GPUCount: 3, CPUCores: 4, MemBytes: 200}
	ApplyPlacement(clusters, p, w)
	n := clusters[0].Nodes[0]
	if n.GPUAllocatable != 5 || n.CPUAllocatable != 12 || n.MemAllocatable != 800 {
		t.Fatalf("unexpected capacities: gpu=%d cpu=%d mem=%d", n.GPUAllocatable, n.CPUAllocatable, n.MemAllocatable)
	}
}

func TestApplyPlacement_noOpWhenNotOK(t *testing.T) {
	clusters := []ClusterSnapshot{{
		CpodID: "c1",
		Nodes:  []NodeSnapshot{{ID: 1, GPUAllocatable: 8}},
	}}
	ApplyPlacement(clusters, Placement{OK: false}, Workload{GPUCount: 4})
	if clusters[0].Nodes[0].GPUAllocatable != 8 {
		t.Fatal("should not mutate")
	}
}

func TestFreshNodeCutoff(t *testing.T) {
	window := 30 * time.Minute
	before := time.Now().Add(-window)
	got := FreshNodeCutoff(window)
	if got.After(before.Add(time.Second)) || got.Before(before.Add(-time.Second)) {
		t.Fatalf("cutoff out of range: %v vs ~%v", got, before)
	}
}
