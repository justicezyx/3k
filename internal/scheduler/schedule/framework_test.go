package schedule

import "testing"

func TestNormalizePluginScores_flat(t *testing.T) {
	got := normalizePluginScores([]int64{10, 10, 10})
	for i, v := range got {
		if v != MaxNodeScore {
			t.Fatalf("index %d: kube flat rule want %d got %d", i, MaxNodeScore, v)
		}
	}
	gotZero := normalizePluginScores([]int64{0, 0})
	for i, v := range gotZero {
		if v != 0 {
			t.Fatalf("index %d: want 0 got %d", i, v)
		}
	}
}

func TestFramework_filterResourcesFit(t *testing.T) {
	clusters := []ClusterSnapshot{{
		CpodID: "c1",
		Nodes:  []NodeSnapshot{{ID: 1, GPUProduct: "A100", GPUAllocatable: 0, GPUTotal: 8, CPUAllocatable: 8}},
	}}
	w := Workload{GPUProduct: "H100", GPUCount: 1, CPUCores: 1}
	p := DefaultFramework(DefaultWeights()).Schedule(w, clusters)
	if p.OK {
		t.Fatal("expected no feasible node")
	}
}

func TestFramework_pluginNamesMatchKubeAnalogues(t *testing.T) {
	fw := DefaultFramework(DefaultWeights())
	if len(fw.filterPlugins) < 2 {
		t.Fatal("expected default filters")
	}
	if fw.filterPlugins[0].Name() != PluginCpodSelector {
		t.Fatalf("filter[0]=%s", fw.filterPlugins[0].Name())
	}
	if fw.filterPlugins[1].Name() != PluginNodeResourcesFit {
		t.Fatalf("filter[1]=%s", fw.filterPlugins[1].Name())
	}
}
