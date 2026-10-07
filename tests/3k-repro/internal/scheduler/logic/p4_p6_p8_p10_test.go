package logic

import (
	"testing"

	"sxwl/3k/internal/scheduler/model"
)

// TestP6_SixteenGPUJobNeverFitsTwoEightGPUNodes places a 16-GPU train job
// against two 8-GPU nodes on one island. CpodJob compares one row's
// GpuAllocatable to GpuNumber, so the job stays need-send.
func TestP6_SixteenGPUJobNeverFitsTwoEightGPUNodes(t *testing.T) {
	h := newHarness(t)
	a := h.insertNode("island-a", "n0", "A100", 8, 16, 64<<30)
	b := h.insertNode("island-a", "n1", "A100", 8, 16, 64<<30)
	h.insertTrain("train-16", "user-1", "", "A100", 16, model.StatusObtainNeedSend, model.StatusNotAssigned)

	resp := h.pull("island-a")
	if contains(trainNames(resp), "train-16") {
		t.Fatal("16-GPU job was placed on a single 8-GPU node")
	}
	cpod, obtain, _, _ := h.trainRow("train-16")
	if cpod != "" || obtain != model.StatusObtainNeedSend {
		t.Fatalf("job cpod_id=%q obtain=%d, want unassigned need-send", cpod, obtain)
	}
	ga, _ := h.nodeAlloc(a)
	gb, _ := h.nodeAlloc(b)
	if ga != 8 || gb != 8 {
		t.Fatalf("node allocatable changed to %d and %d", ga, gb)
	}
}

// TestP8_PresetCpodIDSkipsGPUCount returns a train row whose cpod_id is already
// the caller, and an inference row already StatusAssigned, without checking
// GpuAllocatable.
func TestP8_PresetCpodIDSkipsGPUCount(t *testing.T) {
	h := newHarness(t)
	node := h.insertNode("island-a", "n0", "A100", 1, 8, 64<<30)
	h.insertTrain("train-pinned", "user-1", "island-a", "A100", 16, model.StatusObtainNeedSend, model.StatusNotAssigned)
	h.exec(`INSERT INTO sys_inference
		(service_name, user_id, new_user_id, cpod_id, status, obtain_status, billing_status, gpu_number, gpu_type, metadata, url)
		VALUES ('infer-pinned', 1, 'user-1', 'island-a', ?, 0, 1, 16, 'A100', '{}', '')`, model.StatusAssigned)

	resp := h.pull("island-a")
	if !contains(trainNames(resp), "train-pinned") {
		t.Fatalf("pinned train was not returned: %#v", resp.JobList)
	}
	if len(resp.InferenceServiceList) != 1 || resp.InferenceServiceList[0].CpodId != "island-a" {
		t.Fatalf("pinned inference not returned: %#v", resp.InferenceServiceList)
	}
	gpu, _ := h.nodeAlloc(node)
	if gpu != 1 {
		t.Fatalf("node gpu_allocatable changed from 1 to %d; pin path debited", gpu)
	}
}

// TestP10_EmptyGPUTypeMatchesAnyNode matches a train job with an empty GPU
// type onto a node of a different product that has zero free GPUs, as long as
// that node has one free CPU.
func TestP10_EmptyGPUTypeMatchesAnyNode(t *testing.T) {
	h := newHarness(t)
	id := h.insertNode("island-a", "n0", "H100", 0, 1, 64<<30)
	h.exec(`UPDATE sys_cpod_node SET gpu_allocatable = 0, gpu_total = 0 WHERE id = ?`, id)
	h.insertTrain("train-empty-type", "user-1", "", "", 8, model.StatusObtainNeedSend, model.StatusNotAssigned)

	resp := h.pull("island-a")
	if !contains(trainNames(resp), "train-empty-type") {
		t.Fatal("empty GPU type was not matched onto the H100 node with 0 free GPUs")
	}
	cpod, _, _, _ := h.trainRow("train-empty-type")
	if cpod != "island-a" {
		t.Fatalf("cpod_id = %q", cpod)
	}
	gpu, _ := h.nodeAlloc(id)
	if gpu != -8 {
		t.Fatalf("gpu_allocatable = %d, want -8 (matched with no free GPU)", gpu)
	}
}

// TestP4_StaleNodeDropsCandidateButStickyJobStillEmitted shows the 30-minute
// predicate only filters candidate nodes. A job already stamped with this
// cpod_id is returned with no freshness check, and nothing clears that id.
func TestP4_StaleNodeDropsCandidateButStickyJobStillEmitted(t *testing.T) {
	t.Run("stale candidate is not a place", func(t *testing.T) {
		h := newHarness(t)
		id := h.insertNode("island-a", "n0", "A100", 8, 8, 64<<30)
		h.staleNode(id)
		h.insertTrain("train-stale", "user-1", "", "A100", 1, model.StatusObtainNeedSend, model.StatusNotAssigned)
		resp := h.pull("island-a")
		if contains(trainNames(resp), "train-stale") {
			t.Fatal("job was placed on a node silent for 31 minutes")
		}
		cpod, obtain, _, _ := h.trainRow("train-stale")
		if cpod != "" || obtain != model.StatusObtainNeedSend {
			t.Fatalf("stale place wrote cpod=%q obtain=%d", cpod, obtain)
		}
	})

	t.Run("sticky job is re-emitted without a fresh node", func(t *testing.T) {
		h := newHarness(t)
		id := h.insertNode("island-a", "n0", "A100", 8, 8, 64<<30)
		h.staleNode(id)
		h.insertTrain("train-sticky", "user-1", "island-a", "A100", 8, model.StatusObtainNeedSend, model.StatusNotAssigned)
		resp := h.pull("island-a")
		if !contains(trainNames(resp), "train-sticky") {
			t.Fatal("sticky job was not re-emitted")
		}
		cpod, _, _, _ := h.trainRow("train-sticky")
		if cpod != "island-a" {
			t.Fatalf("sticky cpod_id cleared to %q", cpod)
		}
		var drainCols int
		if err := h.db.QueryRow(`SELECT COUNT(*) FROM information_schema.columns
			WHERE table_schema = ? AND table_name = 'sys_cpod_node'
			AND column_name IN ('draining', 'drain')`, h.name).Scan(&drainCols); err != nil {
			t.Fatal(err)
		}
		if drainCols != 0 {
			t.Fatalf("node table has a drain column (%d)", drainCols)
		}
	})
}
