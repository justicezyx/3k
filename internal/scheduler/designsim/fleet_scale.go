package designsim

import (
	"fmt"

	"sxwl/3k/internal/scheduler/schedule"
	"sxwl/3k/pkg/consts"
	"sxwl/3k/pkg/storage"
)

// Standard8GPUNode matches 3k training examples (torchrun --nproc_per_node=8).
const Standard8GPUNode = 8

// BuildHomogeneousCPod synthesizes a CPod of nodeCount eight-GPU (or gpusPerNode) workers.
func BuildHomogeneousCPod(cpodID, gpuProduct string, nodeCount, gpusPerNode int, price float64, cacheIDs map[string]struct{}, idOffset int64) schedule.ClusterSnapshot {
	nodes := make([]schedule.NodeSnapshot, 0, nodeCount)
	for i := 0; i < nodeCount; i++ {
		nodes = append(nodes, schedule.NodeSnapshot{
			ID:             idOffset + int64(i),
			NodeName:       fmt.Sprintf("%s-node-%04d", cpodID, i),
			GPUProduct:     gpuProduct,
			GPUAllocatable: int64(gpusPerNode),
			GPUTotal:       int64(gpusPerNode),
			CPUAllocatable: 128,
			MemAllocatable: 1 << 40,
		})
	}
	if cacheIDs == nil {
		cacheIDs = map[string]struct{}{}
	}
	return schedule.ClusterSnapshot{
		CpodID:          cpodID,
		GPUPricePerHour: price,
		CacheIDs:        cacheIDs,
		Nodes:           nodes,
	}
}

// LargePretrainFleet models a federated pool sized for 500–1000+ GPU jobs (8-GPU nodes, 3k-style).
// Total capacity ≈ 3,584 GPUs (448×8) so one 1k-GPU gang fits with headroom; two 512-GPU jobs compete.
func LargePretrainFleet() []schedule.ClusterSnapshot {
	model70B := storage.ModelCRDName(storage.ResourceToOSSPath(consts.Model, "meta-llama/Llama-2-70b"))
	checkpoint := storage.ModelCRDName(storage.ResourceToOSSPath(consts.Model, "internal/pretrain-checkpoint-v3"))
	corpus := storage.DatasetCRDName(storage.ResourceToOSSPath(consts.Dataset, "internal/tokenized-corpus-v3"))

	return []schedule.ClusterSnapshot{
		BuildHomogeneousCPod("cpod-h100-prod-a", "H100", 192, Standard8GPUNode, 5.8, map[string]struct{}{
			model70B: {}, checkpoint: {}, corpus: {},
		}, 10_000),
		BuildHomogeneousCPod("cpod-h100-prod-b", "H100", 128, Standard8GPUNode, 5.5, map[string]struct{}{
			checkpoint: {}, corpus: {},
		}, 20_000),
		BuildHomogeneousCPod("cpod-a800-batch", "A800", 96, Standard8GPUNode, 3.2, map[string]struct{}{
			corpus: {},
		}, 30_000),
		BuildHomogeneousCPod("cpod-a800-spot", "A800", 32, Standard8GPUNode, 2.1, map[string]struct{}{}, 40_000),
	}
}

// GangFromTotalGPUs builds a gang spec for portal-style totals (rounds up to full 8-GPU workers).
func GangFromTotalGPUs(name, gpuProduct string, totalGPUs, gpusPerWorker, cpuPerWorker int64, cacheIDs []string) GangSpec {
	if gpusPerWorker <= 0 {
		gpusPerWorker = Standard8GPUNode
	}
	workers := (totalGPUs + gpusPerWorker - 1) / gpusPerWorker
	return GangSpec{
		Name:          name,
		GPUProduct:    gpuProduct,
		GPUsPerWorker: gpusPerWorker,
		WorkerCount:   workers,
		CPUCores:      cpuPerWorker,
		CacheIDs:      cacheIDs,
	}
}

// LargePretrainWorkloads returns ~500-class (512 GPU) and 1000-GPU pretrain gangs.
func LargePretrainWorkloads() map[string]GangSpec {
	model70B := storage.ModelCRDName(storage.ResourceToOSSPath(consts.Model, "meta-llama/Llama-2-70b"))
	checkpoint := storage.ModelCRDName(storage.ResourceToOSSPath(consts.Model, "internal/pretrain-checkpoint-v3"))
	corpus := storage.DatasetCRDName(storage.ResourceToOSSPath(consts.Dataset, "internal/tokenized-corpus-v3"))
	cache := []string{model70B, checkpoint, corpus}

	return map[string]GangSpec{
		// ~500 GPU tier: 64×8 = 512 (common padding for "500 GPU" job requests)
		"llm_pretrain_512gpu": GangFromTotalGPUs("llm_pretrain_512gpu", "H100", 512, 8, 32, cache),
		// 1000 GPU tier: 125×8 = 1000 exactly
		"llm_pretrain_1000gpu": GangFromTotalGPUs("llm_pretrain_1000gpu", "H100", 1000, 8, 32, cache),
		// 504 GPU (portal gpu_number=500 rounded to workers): 63×8
		"llm_pretrain_500gpu_portal_rounded": GangFromTotalGPUs("llm_pretrain_500gpu_portal_rounded", "H100", 500, 8, 32, cache),
	}
}

// FleetTotalGPUs sums allocatable GPUs in a fleet.
func FleetTotalGPUs(clusters []schedule.ClusterSnapshot) int64 {
	var n int64
	for _, c := range clusters {
		for _, node := range c.Nodes {
			n += node.GPUAllocatable
		}
	}
	return n
}

// CountFeasibleCPods returns how many CPods can fit gang g.
func CountFeasibleCPods(g GangSpec, clusters []schedule.ClusterSnapshot) int {
	n := 0
	for _, c := range clusters {
		if ClusterFitsGang(g, c) {
			n++
		}
	}
	return n
}
