package schedule

import (
	"sxwl/3k/pkg/consts"
	"sxwl/3k/pkg/storage"
)

const (
	InferenceCPUCores int64 = 4
)

func InferenceMemBytes() int64 {
	return storage.GBToBytes(50)
}

func TrainingCacheIDs(modelName, datasetName string) []string {
	return UniqueCacheIDs(resourceCacheIDs(modelName, datasetName, ""))
}

func InferenceCacheIDs(modelName, adapterName string) []string {
	return UniqueCacheIDs(resourceCacheIDs(modelName, "", adapterName))
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

// UniqueCacheIDs deduplicates while preserving order.
func UniqueCacheIDs(ids []string) []string {
	if len(ids) == 0 {
		return nil
	}
	seen := make(map[string]struct{}, len(ids))
	out := make([]string, 0, len(ids))
	for _, id := range ids {
		if id == "" {
			continue
		}
		if _, ok := seen[id]; ok {
			continue
		}
		seen[id] = struct{}{}
		out = append(out, id)
	}
	return out
}
