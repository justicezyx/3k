package schedule

import (
	"sxwl/3k/pkg/consts"
	"sxwl/3k/pkg/storage"
)

// Default inference footprint (matches legacy cpod_job_logic).
const (
	InferenceCPUCores int64 = 4
)

func InferenceMemBytes() int64 {
	return storage.GBToBytes(50)
}

// TrainingCacheIDs maps finetune/train job model/dataset names to sys_cpod_cache data_id keys.
func TrainingCacheIDs(modelName, datasetName string) []string {
	return resourceCacheIDs(modelName, datasetName, "")
}

// InferenceCacheIDs maps inference model and optional adapter to cache data_id keys.
func InferenceCacheIDs(modelName, adapterName string) []string {
	return resourceCacheIDs(modelName, "", adapterName)
}

func resourceCacheIDs(modelName, datasetName, adapterName string) []string {
	var ids []string
	if modelName != "" {
		ids = append(ids, storage.ModelCRDName(storage.ResourceToOSSPath(consts.Model, modelName)))
	}
	if datasetName != "" {
		ids = append(ids, storage.DatasetCRDName(storage.ResourceToOSSPath(consts.Dataset, datasetName)))
	}
	if adapterName != "" {
		ids = append(ids, storage.AdapterCRDName(storage.ResourceToOSSPath(consts.Adapter, adapterName)))
	}
	return ids
}
