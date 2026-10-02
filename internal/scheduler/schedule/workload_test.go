package schedule

import (
	"strings"
	"testing"
)

func TestTrainingCacheIDs(t *testing.T) {
	ids := TrainingCacheIDs("meta-llama/Llama-2-7b", "user-ds")
	if len(ids) != 2 {
		t.Fatalf("expected 2 ids, got %d", len(ids))
	}
	for _, id := range ids {
		if !strings.HasPrefix(id, "model-storage-") && !strings.HasPrefix(id, "dataset-storage-") {
			t.Fatalf("unexpected id %s", id)
		}
	}
}

func TestInferenceCacheIDs_adapterOnly(t *testing.T) {
	ids := InferenceCacheIDs("m", "a")
	if len(ids) != 2 {
		t.Fatalf("expected model+adapter ids, got %v", ids)
	}
}

func TestUniqueCacheIDs(t *testing.T) {
	got := UniqueCacheIDs([]string{"a", "b", "a", ""})
	if len(got) != 2 || got[0] != "a" || got[1] != "b" {
		t.Fatalf("got %v", got)
	}
}
