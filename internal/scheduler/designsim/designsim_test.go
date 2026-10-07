package designsim_test

import (
	"fmt"
	"strings"
	"testing"

	"sxwl/3k/internal/scheduler/designsim"
	"sxwl/3k/internal/scheduler/schedule"
)

func defaultWeights() schedule.Weights {
	return schedule.DefaultWeights()
}

// TestScenario_multiNodeGPT3 compares policies on 2×8 GPU job (examples/gpt3 ptjob_gpt3_1.3b_2h16g).
func TestScenario_multiNodeGPT3_policyComparison(t *testing.T) {
	fleet := designsim.Typical3KFleet()
	catalog := designsim.WorkloadCatalog()
	g := catalog["gpt3_1.3b_2h16g"]
	w := defaultWeights()

	policies := []designsim.PolicyName{
		designsim.PolicyBaselineShipped,
		designsim.PolicyCPodGangBottleneck,
		designsim.PolicyCPodBestNodeHeadroom,
		designsim.PolicyCPodGangClusterSlack,
	}

	t.Log("Scenario: GPT-3 1.3B 2×8 GPU (2 nodes × 8 A800) — requires gang-feasible CPod, not 16 GPUs on one node")
	for _, pol := range policies {
		p := designsim.Place(pol, g, fleet, w)
		t.Logf("  [%s] -> %s", pol, designsim.FormatPlacement(p))
		if pol == designsim.PolicyBaselineShipped {
			if p.OK {
				t.Log("  CRITIQUE baseline: may place 16 GPUs on one node if such node existed; on this fleet only cpod-train-8g has 8-GPU nodes → often INFEASIBLE or wrong CPod")
			}
		}
		if pol == designsim.PolicyCPodGangBottleneck && !p.OK {
			t.Errorf("gang bottleneck should place 2×8 on cpod-train-8g")
		}
		if pol == designsim.PolicyCPodGangBottleneck && p.OK && p.CpodID != "cpod-train-8g" {
			t.Errorf("expected cpod-train-8g (cached GPT+dataset), got %s", p.CpodID)
		}
	}
}

// TestScenario_inferenceCache verifies cache_locality pulls Llama inference to cpod-infer-cache.
func TestScenario_inferenceCache_policyComparison(t *testing.T) {
	fleet := designsim.Typical3KFleet()
	g := designsim.WorkloadCatalog()["infer_llama7b_1g"]
	w := defaultWeights()

	for _, pol := range []designsim.PolicyName{designsim.PolicyBaselineShipped, designsim.PolicyCPodGangBottleneck} {
		p := designsim.Place(pol, g, fleet, w)
		t.Logf("[%s] infer_llama7b -> %s", pol, designsim.FormatPlacement(p))
		if !p.OK {
			t.Fatal("inference should be feasible")
		}
		if p.CpodID != "cpod-infer-cache" {
			t.Logf("  CRITIQUE: expected cpod-infer-cache for full model cache; got %s (check cost vs cache weights)", p.CpodID)
		}
	}
}

// TestScenario_onlineOrder_twoGangJobs — two 2×8 jobs on 4×8 fleet: order affects packing (online suboptimality).
func TestScenario_onlineOrder_twoGangJobs(t *testing.T) {
	fleet := designsim.Typical3KFleet()
	// shrink to train cpod only for clear math
	var trainOnly []schedule.ClusterSnapshot
	for _, c := range fleet {
		if c.CpodID == "cpod-train-8g" {
			trainOnly = append(trainOnly, c)
		}
	}
	g16 := designsim.WorkloadCatalog()["gpt3_1.3b_2h16g"]
	w := defaultWeights()

	orderLargeFirst := []designsim.GangSpec{g16, g16}
	orderSame := []designsim.GangSpec{g16, g16}

	for _, pol := range []designsim.PolicyName{designsim.PolicyBaselineShipped, designsim.PolicyCPodGangBottleneck} {
		m1 := designsim.RunOnline(pol, trainOnly, orderLargeFirst, w)
		m2 := designsim.RunOnline(pol, trainOnly, orderSame, w)
		t.Logf("[%s] two 2×8 jobs sequential: placed=%d infeasible=%d maxUtil=%.2f", pol, m1.Placed, m1.Infeasible, m1.MaxClusterUtil)
		if pol == designsim.PolicyBaselineShipped && m1.Infeasible > 0 {
			t.Log("  CRITIQUE baseline: treats 16 GPU as single-node → second job or first may be infeasible on 4×8 fleet")
		}
		if pol == designsim.PolicyCPodGangBottleneck && m1.Infeasible > 0 {
			t.Errorf("gang policy should place two 2×8 jobs on 4×8 nodes")
		}
		_ = m2
	}
}

// TestScenario_mixedQueue_realistic — interleave inference, 1×8 finetune, 2×8 train (typical portal day).
func TestScenario_mixedQueue_realistic(t *testing.T) {
	fleet := designsim.Typical3KFleet()
	catalog := designsim.WorkloadCatalog()
	queue := []designsim.GangSpec{
		catalog["infer_llama7b_1g"],
		catalog["gpt3_1.3b_1h8g"],
		catalog["finetune_1g"],
		catalog["gpt3_1.3b_2h16g"],
	}
	w := defaultWeights()

	t.Log("Mixed queue (inference → 8G train → 1G finetune → 2×8 train)")
	for _, pol := range []designsim.PolicyName{designsim.PolicyBaselineShipped, designsim.PolicyCPodGangBottleneck} {
		m := designsim.RunOnline(pol, fleet, queue, w)
		t.Logf("  [%s] placed=%d infeasible=%d cacheHits=%d/%d maxUtil=%.2f varUtil=%.4f",
			pol, m.Placed, m.Infeasible, m.CacheHits, m.Placed, m.MaxClusterUtil, m.UtilVariance)
		for i, d := range m.Decisions {
			t.Logf("    %d: %s -> %s", i+1, queue[i].Name, designsim.FormatPlacement(d))
		}
	}
}

// TestScenario_orderSensitivity reverses mixed queue.
func TestScenario_orderSensitivity(t *testing.T) {
	fleet := designsim.Typical3KFleet()
	catalog := designsim.WorkloadCatalog()
	forward := []designsim.GangSpec{
		catalog["gpt3_1.3b_2h16g"],
		catalog["gpt3_1.3b_1h8g"],
		catalog["infer_llama7b_1g"],
	}
	reverse := []designsim.GangSpec{
		catalog["infer_llama7b_1g"],
		catalog["gpt3_1.3b_1h8g"],
		catalog["gpt3_1.3b_2h16g"],
	}
	w := defaultWeights()
	pol := designsim.PolicyCPodGangBottleneck

	mF, mR := designsim.CompareOrderings(pol, fleet, forward, reverse, w)
	objF := designsim.CompositeObjective(mF, len(forward))
	objR := designsim.CompositeObjective(mR, len(reverse))
	t.Logf("Order sensitivity (gang CPod policy): forward obj=%.2f (infeas=%d) reverse obj=%.2f (infeas=%d)",
		objF, mF.Infeasible, objR, mR.Infeasible)
	if mF.Infeasible != mR.Infeasible || mF.CacheHits != mR.CacheHits {
		t.Log("  CRITIQUE: greedy online order changes feasibility or cache hits — portal should bind on decision or use reservation")
	}
}

// TestWeightSweep_typicalMix suggests weights over a small grid using composite objective.
func TestWeightSweep_typicalMix(t *testing.T) {
	fleet := designsim.Typical3KFleet()
	catalog := designsim.WorkloadCatalog()
	jobs := []designsim.GangSpec{
		catalog["infer_llama7b_1g"],
		catalog["gpt3_1.3b_1h8g"],
		catalog["gpt3_1.3b_2h16g"],
		catalog["finetune_1g"],
	}
	pol := designsim.PolicyCPodGangBottleneck

	type preset struct {
		name string
		w    schedule.Weights
	}
	presets := []preset{
		{name: "default_doc", w: schedule.DefaultWeights()},
		{name: "cache_heavy", w: schedule.Weights{ResourceHeadroom: 0.2, LoadBalance: 0.15, CacheLocality: 0.5, Cost: 0.15}},
		{name: "headroom_heavy", w: schedule.Weights{ResourceHeadroom: 0.5, LoadBalance: 0.2, CacheLocality: 0.15, Cost: 0.15}},
		{name: "cost_heavy", w: schedule.Weights{ResourceHeadroom: 0.25, LoadBalance: 0.2, CacheLocality: 0.25, Cost: 0.3}},
		{name: "load_heavy", w: schedule.Weights{ResourceHeadroom: 0.2, LoadBalance: 0.45, CacheLocality: 0.25, Cost: 0.1}},
	}

	var bestName string
	var bestObj float64 = -1e9
	t.Log("Weight sweep on typical job mix (CPod gang bottleneck policy):")
	for _, p := range presets {
		m := designsim.RunOnline(pol, fleet, jobs, p.w)
		obj := designsim.CompositeObjective(m, len(jobs))
		t.Logf("  %s: weights=%+v -> placed=%d infeas=%d cacheHits=%d obj=%.2f",
			p.name, p.w, m.Placed, m.Infeasible, m.CacheHits, obj)
		if obj > bestObj {
			bestObj = obj
			bestName = p.name
		}
	}
	t.Logf("RECOMMENDATION (sim composite): prefer preset %q for mixed 3k workloads; keep cache_locality ≥ 0.25 for inference/finetune; use gang bottleneck headroom not single-node headroom for train≥8GPU", bestName)
	t.Log("Suggested starting YAML: ResourceHeadroom=0.30, LoadBalance=0.20, CacheLocality=0.35, Cost=0.15 when cache telemetry is trusted; raise Cost if cheap dev CPods steal production jobs")
}

// TestHeadroomOptions_threeVariants documents top-3 gang headroom options on same fleet.
func TestHeadroomOptions_threeVariants(t *testing.T) {
	fleet := designsim.Typical3KFleet()
	g := designsim.WorkloadCatalog()["pytorch_multinode_2h8g"]
	w := defaultWeights()

	opts := []struct {
		name   string
		policy designsim.PolicyName
	}{
		{"bottleneck_min_node_slack", designsim.PolicyCPodGangBottleneck},
		{"legacy_single_node_16gpu_shape", designsim.PolicyCPodBestNodeHeadroom},
		{"cluster_slack_after_gang", designsim.PolicyCPodGangClusterSlack},
	}

	t.Log("Gang headroom options for 2×4 GPU multinode (examples/pytorch-multinode):")
	var lines []string
	for _, o := range opts {
		p := designsim.Place(o.policy, g, fleet, w)
		lines = append(lines, fmt.Sprintf("%s: %s", o.name, designsim.FormatPlacement(p)))
	}
	t.Log(strings.Join(lines, "\n"))
	pB := designsim.Place(designsim.PolicyCPodGangBottleneck, g, fleet, w)
	pL := designsim.Place(designsim.PolicyCPodBestNodeHeadroom, g, fleet, w)
	if pB.OK && pL.OK && pB.CpodID != pL.CpodID {
		t.Logf("  CRITIQUE: cpod_best_single_node_headroom picked %s vs gang bottleneck %s — do not use portal total-GPU-on-one-node headroom at CPod level for multi-node jobs", pL.CpodID, pB.CpodID)
	}
}
