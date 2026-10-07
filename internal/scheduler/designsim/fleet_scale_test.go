package designsim_test

import (
	"fmt"
	"testing"
	"time"

	"sxwl/3k/internal/scheduler/designsim"
	"sxwl/3k/internal/scheduler/schedule"
)

func largeFleetShort(t *testing.T) {
	if testing.Short() {
		t.Skip("skip large GPU fleet simulation in -short")
	}
}

func TestLargeScale_fleetCapacity(t *testing.T) {
	largeFleetShort(t)
	fleet := designsim.LargePretrainFleet()
	total := designsim.FleetTotalGPUs(fleet)
	t.Logf("Large pretrain fleet: %d nodes, %d total GPUs across %d CPods",
		countNodes(fleet), total, len(fleet))
	for _, c := range fleet {
		var gpus int64
		for _, n := range c.Nodes {
			gpus += n.GPUAllocatable
		}
		t.Logf("  %s: %d nodes, %d GPUs, price=%.1f", c.CpodID, len(c.Nodes), gpus, c.GPUPricePerHour)
	}
	if total < 3000 {
		t.Fatalf("expected multi-kGPU fleet for megajob sim, got %d", total)
	}
}

func TestLargeScale_512GPU_singleJob(t *testing.T) {
	largeFleetShort(t)
	fleet := designsim.LargePretrainFleet()
	wl := designsim.LargePretrainWorkloads()["llm_pretrain_512gpu"]
	t.Logf("Job: %s workers=%d gpus/worker=%d total=%d (500-class tier)",
		wl.Name, wl.WorkerCount, wl.GPUsPerWorker, wl.TotalGPUs())

	start := time.Now()
	pBase := designsim.Place(designsim.PolicyBaselineShipped, wl, fleet, schedule.DefaultWeights())
	pGang := designsim.Place(designsim.PolicyCPodGangBottleneck, wl, fleet, schedule.DefaultWeights())
	elapsed := time.Since(start)

	t.Logf("Feasible CPods (gang): %d / %d", designsim.CountFeasibleCPods(wl, fleet), len(fleet))
	t.Logf("baseline -> %s (%.2fms)", designsim.FormatPlacement(pBase), ms(elapsed))
	t.Logf("gang     -> %s", designsim.FormatPlacement(pGang))

	if pBase.OK {
		t.Log("CRITIQUE baseline: placed 512-GPU job as single-node demand — unrealistic vs PyTorchJob gang")
	} else {
		t.Log("baseline INFEASIBLE: no node has 512 GPUs (expected)")
	}
	if !pGang.OK {
		t.Fatal("512-GPU gang should fit on h100-prod-a (192 nodes)")
	}
	if pGang.CpodID != "cpod-h100-prod-a" {
		t.Logf("NOTE: gang picked %s (cache/cost/headroom tradeoff; prod-a has full cache)", pGang.CpodID)
	}
}

func TestLargeScale_1000GPU_singleJob(t *testing.T) {
	largeFleetShort(t)
	fleet := designsim.LargePretrainFleet()
	wl := designsim.LargePretrainWorkloads()["llm_pretrain_1000gpu"]
	t.Logf("Job: %d workers × %d GPU = %d GPUs", wl.WorkerCount, wl.GPUsPerWorker, wl.TotalGPUs())

	pBase := designsim.Place(designsim.PolicyBaselineShipped, wl, fleet, schedule.DefaultWeights())
	pGang := designsim.Place(designsim.PolicyCPodGangBottleneck, wl, fleet, schedule.DefaultWeights())

	t.Logf("baseline -> %s", designsim.FormatPlacement(pBase))
	t.Logf("gang     -> %s", designsim.FormatPlacement(pGang))

	if pBase.OK {
		t.Error("baseline should not place 1000 GPUs on one node")
	}
	if !pGang.OK {
		t.Fatal("1000-GPU gang should fit: prod-a 1536 or prod-a+b combined not needed (192*8=1536 >= 1000)")
	}
	// 125 nodes required; prod-a has 192
	if pGang.CpodID != "cpod-h100-prod-a" && pGang.CpodID != "cpod-h100-prod-b" {
		t.Errorf("unexpected CPod for 1k H100 job: %s", pGang.CpodID)
	}
}

func TestLargeScale_online_two512_then_1000(t *testing.T) {
	largeFleetShort(t)
	fleet := designsim.LargePretrainFleet()
	catalog := designsim.LargePretrainWorkloads()
	w := schedule.DefaultWeights()

	queue := []designsim.GangSpec{
		catalog["llm_pretrain_512gpu"],
		catalog["llm_pretrain_512gpu"],
		catalog["llm_pretrain_1000gpu"],
	}

	for _, pol := range []designsim.PolicyName{designsim.PolicyBaselineShipped, designsim.PolicyCPodGangBottleneck} {
		start := time.Now()
		m := designsim.RunOnline(pol, fleet, queue, w)
		t.Logf("[%s] online 512+512+1000: placed=%d infeas=%d maxUtil=%.3f elapsed=%.2fs",
			pol, m.Placed, m.Infeasible, m.MaxClusterUtil, time.Since(start).Seconds())
		for i, d := range m.Decisions {
			t.Logf("  step %d %s -> %s", i+1, queue[i].Name, designsim.FormatPlacement(d))
		}
		if pol == designsim.PolicyBaselineShipped && m.Placed > 0 {
			t.Log("CRITIQUE baseline: any placement used wrong single-node semantics")
		}
		if pol == designsim.PolicyCPodGangBottleneck && m.Infeasible > 0 {
			t.Logf("CRITIQUE gang online: %d infeas after prior jobs — fleet fragmentation or insufficient total GPUs", m.Infeasible)
		}
	}
}

func TestLargeScale_online_order_1000_before_512(t *testing.T) {
	largeFleetShort(t)
	fleet := designsim.LargePretrainFleet()
	catalog := designsim.LargePretrainWorkloads()
	w := schedule.DefaultWeights()
	pol := designsim.PolicyCPodGangBottleneck

	bigFirst := []designsim.GangSpec{
		catalog["llm_pretrain_1000gpu"],
		catalog["llm_pretrain_512gpu"],
	}
	smallFirst := []designsim.GangSpec{
		catalog["llm_pretrain_512gpu"],
		catalog["llm_pretrain_1000gpu"],
	}

	m1 := designsim.RunOnline(pol, fleet, bigFirst, w)
	m2 := designsim.RunOnline(pol, fleet, smallFirst, w)
	o1 := designsim.CompositeObjective(m1, len(bigFirst))
	o2 := designsim.CompositeObjective(m2, len(smallFirst))

	t.Logf("Order big-then-small: placed=%d infeas=%d obj=%.2f", m1.Placed, m1.Infeasible, o1)
	t.Logf("Order small-then-big: placed=%d infeas=%d obj=%.2f", m2.Placed, m2.Infeasible, o2)
	if m1.Infeasible != m2.Infeasible {
		t.Log("CRITIQUE: job order changed feasibility — online greedy packing; consider reservations for 512+ GPU jobs")
	}
}

func TestLargeScale_500portal_vs_512gang(t *testing.T) {
	largeFleetShort(t)
	fleet := designsim.LargePretrainFleet()
	w500 := designsim.LargePretrainWorkloads()["llm_pretrain_500gpu_portal_rounded"]
	w512 := designsim.LargePretrainWorkloads()["llm_pretrain_512gpu"]
	t.Logf("Portal gpu_number=500 → %d workers (%d GPUs); explicit 512 tier → %d workers",
		w500.WorkerCount, w500.TotalGPUs(), w512.WorkerCount)

	p500 := designsim.Place(designsim.PolicyCPodGangBottleneck, w500, fleet, schedule.DefaultWeights())
	p512 := designsim.Place(designsim.PolicyCPodGangBottleneck, w512, fleet, schedule.DefaultWeights())
	t.Logf("500-rounded -> %s", designsim.FormatPlacement(p500))
	t.Logf("512 tier   -> %s", designsim.FormatPlacement(p512))
}

func TestLargeScale_weightSweep_megajob(t *testing.T) {
	largeFleetShort(t)
	fleet := designsim.LargePretrainFleet()
	jobs := []designsim.GangSpec{
		designsim.LargePretrainWorkloads()["llm_pretrain_1000gpu"],
	}
	pol := designsim.PolicyCPodGangBottleneck

	presets := []struct {
		name string
		w    schedule.Weights
	}{
		{"default", schedule.DefaultWeights()},
		{"cache_heavy", schedule.Weights{ResourceHeadroom: 0.25, LoadBalance: 0.15, CacheLocality: 0.45, Cost: 0.15}},
		{"headroom_heavy", schedule.Weights{ResourceHeadroom: 0.45, LoadBalance: 0.2, CacheLocality: 0.2, Cost: 0.15}},
		{"cost_to_a800", schedule.Weights{ResourceHeadroom: 0.2, LoadBalance: 0.2, CacheLocality: 0.2, Cost: 0.4}},
	}

	for _, p := range presets {
		m := designsim.RunOnline(pol, fleet, jobs, p.w)
		place := designsim.FormatPlacement(m.Decisions[0])
		t.Logf("1k GPU job weights=%s -> %s", p.name, place)
	}
	t.Log("At 512–1000 GPU scale, cache_heavy keeps jobs on cpod-h100-prod-a; high cost may steer among H100 CPods (prod-b), not A800, when GPUProduct=H100")
}

// TestLargeScale_fragmentation_1000_after_two512 documents that prod-a alone cannot hold 512+512+1000 but fleet can via prod-b.
func TestLargeScale_fragmentation_1000_after_two512(t *testing.T) {
	largeFleetShort(t)
	fleet := designsim.LargePretrainFleet()
	catalog := designsim.LargePretrainWorkloads()
	onlyA := []schedule.ClusterSnapshot{fleet[0]} // 192 nodes
	m := designsim.RunOnline(designsim.PolicyCPodGangBottleneck, onlyA, []designsim.GangSpec{
		catalog["llm_pretrain_512gpu"],
		catalog["llm_pretrain_512gpu"],
		catalog["llm_pretrain_1000gpu"],
	}, schedule.DefaultWeights())
	t.Logf("Single CPod (192 nodes): placed=%d infeas=%d — need 64+64+125=253 nodes for queue", m.Placed, m.Infeasible)
	if m.Infeasible == 0 {
		t.Log("unexpected: third job should fail on one 192-node CPod after two 512 jobs")
	}
}

func countNodes(fleet []schedule.ClusterSnapshot) int {
	n := 0
	for _, c := range fleet {
		n += len(c.Nodes)
	}
	return n
}

func ms(d time.Duration) float64 {
	return float64(d.Nanoseconds()) / 1e6
}

func ExampleLargePretrainFleet() {
	fleet := designsim.LargePretrainFleet()
	fmt.Printf("nodes=%d gpus=%d\n", countNodes(fleet), designsim.FleetTotalGPUs(fleet))
	// Output: nodes=448 gpus=3584
}
