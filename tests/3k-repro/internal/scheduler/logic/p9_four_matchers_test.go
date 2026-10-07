package logic

import (
	"testing"

	"sxwl/3k/internal/scheduler/model"
	"sxwl/3k/pkg/storage"
)

// TestP9_FourMatchersAndTwoSkipCapacity runs the four assign paths in CpodJob.
// Train requires one CPU. Inference requires 4 CPU and 50GiB. Jupyter with an
// empty GPU product assigns with no node check. App jobs assign with no
// capacity predicate at all.
func TestP9_FourMatchersAndTwoSkipCapacity(t *testing.T) {
	t.Run("train needs one cpu", func(t *testing.T) {
		h := newHarness(t)
		id := h.insertNode("island-a", "n0", "A100", 8, 0, 64<<30)
		h.exec(`UPDATE sys_cpod_node SET cpu_allocatable = 0 WHERE id = ?`, id)
		h.insertTrain("train-nocpu", "user-1", "", "A100", 1, model.StatusObtainNeedSend, model.StatusNotAssigned)
		resp := h.pull("island-a")
		if contains(trainNames(resp), "train-nocpu") {
			t.Fatal("train assigned with cpu_allocatable 0")
		}

		// The floor is 1, not a higher CPU requirement. cpu_allocatable 0 is
		// rejected above; 1 CPU with a free GPU is accepted.
		h2 := newHarness(t)
		h2.insertNode("island-a", "n0", "A100", 8, 1, 64<<30)
		h2.insertTrain("train-onecpu", "user-1", "", "A100", 1, model.StatusObtainNeedSend, model.StatusNotAssigned)
		resp = h2.pull("island-a")
		if !contains(trainNames(resp), "train-onecpu") {
			t.Fatal("train was not assigned at cpu_allocatable 1")
		}
	})

	t.Run("inference needs 4 cpu and 50GiB", func(t *testing.T) {
		h := newHarness(t)
		h.insertNode("island-a", "n0", "A100", 8, 3, 1<<30)
		h.exec(`INSERT INTO sys_inference
			(service_name, user_id, new_user_id, cpod_id, status, obtain_status, billing_status, gpu_number, gpu_type, metadata, url)
			VALUES ('infer-tight', 1, 'user-1', '', ?, 0, 1, 1, 'A100', '{}', '')`, model.StatusNotAssigned)
		resp := h.pull("island-a")
		if len(resp.InferenceServiceList) != 0 {
			t.Fatal("inference assigned without 4 CPU and 50GiB")
		}

		h2 := newHarness(t)
		h2.insertNode("island-a", "n0", "A100", 8, 4, storage.GBToBytes(50))
		h2.exec(`INSERT INTO sys_inference
			(service_name, user_id, new_user_id, cpod_id, status, obtain_status, billing_status, gpu_number, gpu_type, metadata, url)
			VALUES ('infer-ok', 1, 'user-1', '', ?, 0, 1, 1, 'A100', '{}', '')`, model.StatusNotAssigned)
		resp = h2.pull("island-a")
		if len(resp.InferenceServiceList) != 1 {
			t.Fatal("inference was not assigned when CPU and memory floors were met")
		}
	})

	t.Run("jupyter empty gpu product skips the node", func(t *testing.T) {
		h := newHarness(t)
		h.exec(`INSERT INTO sys_jupyterlab
			(job_name, user_id, new_user_id, cpod_id, status, gpu_count, gpu_prod, cpu_count, mem_count, resource)
			VALUES ('jb-empty', 1, 'user-1', '', ?, 8, '', 32, ?, '{}')`,
			model.StatusNotAssigned, storage.GBToBytes(64))
		resp := h.pull("island-a")
		if len(resp.JupyterlabList) != 1 {
			t.Fatalf("empty-product jupyter not assigned with zero nodes: %#v", resp.JupyterlabList)
		}
		var cpod string
		if err := h.db.QueryRow(`SELECT cpod_id FROM sys_jupyterlab WHERE job_name = 'jb-empty'`).Scan(&cpod); err != nil {
			t.Fatal(err)
		}
		if cpod != "island-a" {
			t.Fatalf("jupyter cpod_id = %q", cpod)
		}
	})

	t.Run("app job skips capacity", func(t *testing.T) {
		h := newHarness(t)
		h.exec(`INSERT INTO sys_app (app_id, app_name, user_id, ` + "`desc`" + `, crd, status) VALUES ('app-1', 'app', 'user-1', '', '{}', 1)`)
		h.exec(`INSERT INTO sys_app_job (job_name, user_id, app_id, app_name, instance_name, cpod_id, status, billing_status, url, meta)
			VALUES ('appjob-1', 'user-1', 'app-1', 'app', 'inst', '', ?, 1, '', '')`, model.StatusNotAssigned)
		resp := h.pull("island-a")
		if len(resp.AppJobList) != 1 {
			t.Fatalf("app job not assigned with zero nodes: %#v", resp.AppJobList)
		}
		var cpod string
		if err := h.db.QueryRow(`SELECT cpod_id FROM sys_app_job WHERE job_name = 'appjob-1'`).Scan(&cpod); err != nil {
			t.Fatal(err)
		}
		if cpod != "island-a" {
			t.Fatalf("app cpod_id = %q", cpod)
		}
	})
}
