package designsim

import (
	"sxwl/3k/internal/scheduler/schedule"
	"sxwl/3k/pkg/consts"
	"sxwl/3k/pkg/storage"
)

// Typical3KFleet is a small multi-CPod fleet inspired by repo examples (8-GPU nodes, H100/A800 mix, cache + price skew).
func Typical3KFleet() []schedule.ClusterSnapshot {
	modelLlama7B := storage.ModelCRDName(storage.ResourceToOSSPath(consts.Model, "meta-llama/Llama-2-7b"))
	modelGPT13B := storage.ModelCRDName(storage.ResourceToOSSPath(consts.Model, "damo/nlp_gpt3_text-generation_1.3B"))
	datasetPoetry := storage.DatasetCRDName(storage.ResourceToOSSPath(consts.Dataset, "chinese-poetry-collection"))

	return []schedule.ClusterSnapshot{
		{
			CpodID:          "cpod-dev-small",
			GPUPricePerHour: 2.5,
			CacheIDs:        map[string]struct{}{},
			Nodes: []schedule.NodeSnapshot{
				{ID: 101, NodeName: "dev-n1", GPUProduct: "A800", GPUAllocatable: 4, GPUTotal: 4, CPUAllocatable: 64, MemAllocatable: 512 << 30},
				{ID: 102, NodeName: "dev-n2", GPUProduct: "A800", GPUAllocatable: 4, GPUTotal: 4, CPUAllocatable: 64, MemAllocatable: 512 << 30},
			},
		},
		{
			CpodID:          "cpod-train-8g",
			GPUPricePerHour: 4.0,
			CacheIDs: map[string]struct{}{
				modelGPT13B:   {},
				datasetPoetry: {},
			},
			Nodes: []schedule.NodeSnapshot{
				{ID: 201, NodeName: "train-n1", GPUProduct: "A800", GPUAllocatable: 8, GPUTotal: 8, CPUAllocatable: 128, MemAllocatable: 1 << 40},
				{ID: 202, NodeName: "train-n2", GPUProduct: "A800", GPUAllocatable: 8, GPUTotal: 8, CPUAllocatable: 128, MemAllocatable: 1 << 40},
				{ID: 203, NodeName: "train-n3", GPUProduct: "A800", GPUAllocatable: 8, GPUTotal: 8, CPUAllocatable: 128, MemAllocatable: 1 << 40},
				{ID: 204, NodeName: "train-n4", GPUProduct: "A800", GPUAllocatable: 8, GPUTotal: 8, CPUAllocatable: 128, MemAllocatable: 1 << 40},
			},
		},
		{
			CpodID:          "cpod-infer-cache",
			GPUPricePerHour: 5.5,
			CacheIDs: map[string]struct{}{
				modelLlama7B: {},
				modelGPT13B:  {},
			},
			Nodes: []schedule.NodeSnapshot{
				{ID: 301, NodeName: "infer-n1", GPUProduct: "H100", GPUAllocatable: 8, GPUTotal: 8, CPUAllocatable: 128, MemAllocatable: 1 << 40},
				{ID: 302, NodeName: "infer-n2", GPUProduct: "H100", GPUAllocatable: 8, GPUTotal: 8, CPUAllocatable: 128, MemAllocatable: 1 << 40},
			},
		},
	}
}

// WorkloadCatalog returns named gang specs aligned with examples/ and portalsync GPU splitting (8 GPUs per worker when total >= 8).
func WorkloadCatalog() map[string]GangSpec {
	modelLlama7B := storage.ModelCRDName(storage.ResourceToOSSPath(consts.Model, "meta-llama/Llama-2-7b"))
	modelGPT13B := storage.ModelCRDName(storage.ResourceToOSSPath(consts.Model, "damo/nlp_gpt3_text-generation_1.3B"))
	datasetPoetry := storage.DatasetCRDName(storage.ResourceToOSSPath(consts.Dataset, "chinese-poetry-collection"))

	return map[string]GangSpec{
		// examples/gpt3 ptjob 1h8g — single-node 8×GPU finetune
		"gpt3_1.3b_1h8g": {
			Name: "gpt3_1.3b_1h8g", GPUProduct: "A800", GPUsPerWorker: 8, WorkerCount: 1,
			CPUCores: 16, CacheIDs: []string{modelGPT13B, datasetPoetry},
		},
		// examples/gpt3 ptjob 2h16g — 2 nodes × 8 GPUs (torchrun nnodes=2)
		"gpt3_1.3b_2h16g": {
			Name: "gpt3_1.3b_2h16g", GPUProduct: "A800", GPUsPerWorker: 8, WorkerCount: 2,
			CPUCores: 32, CacheIDs: []string{modelGPT13B, datasetPoetry},
		},
		// examples/pytorch-multinode — 2×4 GPU
		"pytorch_multinode_2h8g": {
			Name: "pytorch_multinode_2h8g", GPUProduct: "A800", GPUsPerWorker: 4, WorkerCount: 2,
			CPUCores: 8, CacheIDs: []string{},
		},
		// Inference Llama-7B — single GPU, cache-sensitive (KServe-style)
		"infer_llama7b_1g": {
			Name: "infer_llama7b_1g", GPUProduct: "H100", GPUsPerWorker: 1, WorkerCount: 1,
			CPUCores: 4, MemBytes: schedule.InferenceMemBytes(), CacheIDs: []string{modelLlama7B},
		},
		// Small finetune / Jupyter — 1×A800
		"finetune_1g": {
			Name: "finetune_1g", GPUProduct: "A800", GPUsPerWorker: 1, WorkerCount: 1,
			CPUCores: 8, CacheIDs: []string{modelGPT13B},
		},
		// Portal gpu_number=16 interpreted as one-node demand (shipped WNS quirk for multi-node totals)
		"portal_total16_as_single": {
			Name: "portal_total16_as_single", GPUProduct: "A800", GPUsPerWorker: 16, WorkerCount: 1,
			CPUCores: 32, CacheIDs: []string{modelGPT13B},
		},
	}
}

func CloneClusters(in []schedule.ClusterSnapshot) []schedule.ClusterSnapshot {
	out := make([]schedule.ClusterSnapshot, len(in))
	for i, c := range in {
		out[i] = c
		out[i].Nodes = append([]schedule.NodeSnapshot(nil), c.Nodes...)
		if c.CacheIDs != nil {
			out[i].CacheIDs = make(map[string]struct{}, len(c.CacheIDs))
			for k, v := range c.CacheIDs {
				out[i].CacheIDs[k] = v
			}
		}
	}
	return out
}
