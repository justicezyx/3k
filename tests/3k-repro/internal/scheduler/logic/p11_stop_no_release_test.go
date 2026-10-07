package logic

import (
	"context"
	"testing"

	"sxwl/3k/internal/scheduler/model"
	"sxwl/3k/internal/scheduler/types"
)

// TestP11_StopAndOverdraftDoNotRelease shows user stop and the overdraft
// delete path set deleted or stopped and leave cpod_id and the node debit in place.
func TestP11_StopAndOverdraftDoNotRelease(t *testing.T) {
	t.Run("user stop", func(t *testing.T) {
		h := newHarness(t)
		id := h.insertNode("island-a", "n0", "A100", 8, 8, 64<<30)
		h.exec(`UPDATE sys_cpod_node SET gpu_allocatable = 0 WHERE id = ?`, id)
		h.insertTrain("train-stop", "user-1", "island-a", "A100", 8, model.StatusObtainNeedSend, model.StatusRunning)
		_, err := NewJobStopLogic(context.Background(), h.svc).JobStop(&types.JobStopReq{JobId: "train-stop"})
		if err != nil {
			t.Fatal(err)
		}
		cpod, _, deleted, _ := h.trainRow("train-stop")
		if deleted != 1 {
			t.Fatalf("deleted = %d", deleted)
		}
		if cpod != "island-a" {
			t.Fatalf("stop cleared cpod_id to %q", cpod)
		}
		gpu, _ := h.nodeAlloc(id)
		if gpu != 0 {
			t.Fatalf("stop restored gpu_allocatable to %d", gpu)
		}
	})

	t.Run("overdraft delete", func(t *testing.T) {
		h := newHarness(t)
		id := h.insertNode("island-a", "n0", "A100", 8, 8, 64<<30)
		h.exec(`UPDATE sys_cpod_node SET gpu_allocatable = 0 WHERE id = ?`, id)
		h.insertTrain("train-od", "user-1", "island-a", "A100", 8, model.StatusObtainNeedSend, model.StatusRunning)
		h.exec(`INSERT INTO sys_inference
			(service_name, user_id, new_user_id, cpod_id, status, obtain_status, billing_status, gpu_number, gpu_type, metadata, url)
			VALUES ('infer-od', 1, 'user-1', 'island-a', ?, 0, 1, 1, 'A100', '{}', '')`, model.StatusRunning)
		_, err := NewJobsDelLogic(context.Background(), h.svc).JobsDel(&types.JobsDelReq{ToUser: "user-1"})
		if err != nil {
			t.Fatal(err)
		}
		cpod, _, deleted, _ := h.trainRow("train-od")
		if deleted != int(model.JobDeleted) || cpod != "island-a" {
			t.Fatalf("train deleted=%d cpod=%q", deleted, cpod)
		}
		var inferCpod string
		var inferStatus int
		if err := h.db.QueryRow(`SELECT cpod_id, status FROM sys_inference WHERE service_name = 'infer-od'`).Scan(&inferCpod, &inferStatus); err != nil {
			t.Fatal(err)
		}
		if inferStatus != model.StatusStopped || inferCpod != "island-a" {
			t.Fatalf("inference status=%d cpod=%q", inferStatus, inferCpod)
		}
		gpu, _ := h.nodeAlloc(id)
		if gpu != 0 {
			t.Fatalf("overdraft stop restored gpu_allocatable to %d", gpu)
		}
	})
}
