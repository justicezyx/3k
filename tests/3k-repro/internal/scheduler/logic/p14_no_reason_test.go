package logic

import (
	"reflect"
	"testing"

	"sxwl/3k/internal/scheduler/model"
	"sxwl/3k/internal/scheduler/types"
)

// TestP14_FilterMissHasNoReason leaves an unmatched train job at need-send.
// types.Job has ObtainStatus and no reason field.
func TestP14_FilterMissHasNoReason(t *testing.T) {
	h := newHarness(t)
	h.insertNode("island-a", "n0", "H100", 8, 8, 64<<30)
	h.insertTrain("train-miss", "user-1", "", "A100", 1, model.StatusObtainNeedSend, model.StatusNotAssigned)
	resp := h.pull("island-a")
	if len(resp.JobList) != 0 {
		t.Fatalf("filter miss returned jobs: %#v", resp.JobList)
	}
	_, obtain, _, _ := h.trainRow("train-miss")
	if obtain != model.StatusObtainNeedSend {
		t.Fatalf("obtain_status = %d, want need-send", obtain)
	}
	jobType := reflect.TypeOf(types.Job{})
	if _, ok := jobType.FieldByName("Reason"); ok {
		t.Fatal("types.Job has a Reason field")
	}
	for i := 0; i < jobType.NumField(); i++ {
		f := jobType.Field(i)
		if f.Tag.Get("json") == "reason" || f.Tag.Get("json") == "reason,omitempty" {
			t.Fatalf("types.Job json field %s is a reason", f.Name)
		}
	}
}
