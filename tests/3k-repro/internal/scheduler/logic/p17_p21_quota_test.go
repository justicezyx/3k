package logic

import (
	"context"
	"testing"

	"sxwl/3k/internal/scheduler/job"
	"sxwl/3k/internal/scheduler/model"
)

// TestP17_QuotaCountsOnlyRunning shows CheckQuota sums running train, inference,
// and jupyter GPUs, returns ok when the quota row is missing, and does not
// read app jobs. Pending inference and a non-running jupyter stay out of the sum.
func TestP17_QuotaCountsOnlyRunning(t *testing.T) {
	h := newHarness(t)
	h.exec(`INSERT INTO sys_quota (user_id, new_user_id, resource, quota) VALUES (1, 'user-1', 'A100', 4)`)
	h.insertTrain("train-pending", "user-1", "", "A100", 4, model.StatusObtainNeedSend, model.StatusNotAssigned)

	ok, left, err := job.CheckQuota(context.Background(), h.svc, "user-1", "A100", 4)
	if err != nil {
		t.Fatal(err)
	}
	if !ok || left != 4 {
		t.Fatalf("pending 4-GPU job consumed quota: ok=%v left=%d", ok, left)
	}

	h.exec(`UPDATE sys_user_job SET work_status = ? WHERE job_name = 'train-pending'`, model.StatusRunning)
	ok, left, err = job.CheckQuota(context.Background(), h.svc, "user-1", "A100", 1)
	if err != nil {
		t.Fatal(err)
	}
	if ok || left != 0 {
		t.Fatalf("running train was not the whole quota: ok=%v left=%d", ok, left)
	}

	h.exec(`UPDATE sys_user_job SET deleted = 1 WHERE job_name = 'train-pending'`)
	h.exec(`INSERT INTO sys_inference
		(service_name, user_id, new_user_id, cpod_id, status, gpu_number, gpu_type, metadata, url)
		VALUES ('infer-pending', 1, 'user-1', '', ?, 4, 'A100', '{}', '')`, model.StatusNotAssigned)
	ok, left, err = job.CheckQuota(context.Background(), h.svc, "user-1", "A100", 4)
	if err != nil || !ok || left != 4 {
		t.Fatalf("non-running inference was counted: ok=%v left=%d err=%v", ok, left, err)
	}

	h.exec(`UPDATE sys_inference SET status = ? WHERE service_name = 'infer-pending'`, model.StatusRunning)
	ok, left, err = job.CheckQuota(context.Background(), h.svc, "user-1", "A100", 1)
	if err != nil {
		t.Fatal(err)
	}
	if ok || left != 0 {
		t.Fatalf("running inference was not summed: ok=%v left=%d", ok, left)
	}

	h.exec(`UPDATE sys_inference SET status = ? WHERE service_name = 'infer-pending'`, model.StatusStopped)
	h.exec(`INSERT INTO sys_jupyterlab
		(job_name, user_id, new_user_id, cpod_id, status, gpu_count, gpu_prod, cpu_count, mem_count, resource)
		VALUES ('jb-pending', 1, 'user-1', '', ?, 4, 'A100', 1, 0, '{}')`, model.StatusNotAssigned)
	ok, left, err = job.CheckQuota(context.Background(), h.svc, "user-1", "A100", 4)
	if err != nil || !ok || left != 4 {
		t.Fatalf("non-running jupyter was counted: ok=%v left=%d err=%v", ok, left, err)
	}

	h.exec(`UPDATE sys_jupyterlab SET status = ? WHERE job_name = 'jb-pending'`, model.StatusRunning)
	ok, left, err = job.CheckQuota(context.Background(), h.svc, "user-1", "A100", 1)
	if err != nil {
		t.Fatal(err)
	}
	if ok || left != 0 {
		t.Fatalf("running jupyter was not summed: ok=%v left=%d", ok, left)
	}

	h.exec(`DELETE FROM sys_quota WHERE new_user_id = 'user-1'`)
	ok, _, err = job.CheckQuota(context.Background(), h.svc, "user-1", "A100", 100)
	if err != nil {
		t.Fatal(err)
	}
	if !ok {
		t.Fatal("missing quota row rejected the job")
	}

	h.exec(`INSERT INTO sys_quota (user_id, new_user_id, resource, quota) VALUES (1, 'user-1', 'A100', 1)`)
	h.exec(`UPDATE sys_jupyterlab SET status = ? WHERE job_name = 'jb-pending'`, model.StatusStopped)
	h.exec(`INSERT INTO sys_app_job (job_name, user_id, app_id, app_name, instance_name, cpod_id, status, billing_status, url, meta)
		VALUES ('app-running', 'user-1', 'app-1', 'app', 'inst', 'island-a', ?, 1, '', '')`, model.StatusRunning)
	ok, left, err = job.CheckQuota(context.Background(), h.svc, "user-1", "A100", 1)
	if err != nil {
		t.Fatal(err)
	}
	if !ok || left != 1 {
		t.Fatalf("running app job was counted: ok=%v left=%d", ok, left)
	}
}

// TestP21_QuotaKeyIsUserNotOrg shows the admit lookup is (new_user_id, resource).
// sys_quota has no org column, so a second user with no row is allowed.
func TestP21_QuotaKeyIsUserNotOrg(t *testing.T) {
	h := newHarness(t)
	var orgCols int
	if err := h.db.QueryRow(`SELECT COUNT(*) FROM information_schema.columns
		WHERE table_schema = ? AND table_name = 'sys_quota'
		AND column_name IN ('org', 'org_id', 'company_id')`, h.name).Scan(&orgCols); err != nil {
		t.Fatal(err)
	}
	if orgCols != 0 {
		t.Fatalf("sys_quota has an org column (%d)", orgCols)
	}
	h.exec(`INSERT INTO sys_user (new_user_id, username, email, user_type, company_id) VALUES
		('user-a', 'a', 'a@example.com', 2, 'org-1'),
		('user-b', 'b', 'b@example.com', 2, 'org-1')`)
	h.exec(`INSERT INTO sys_quota (user_id, new_user_id, resource, quota) VALUES (1, 'user-a', 'A100', 1)`)

	ok, _, err := job.CheckQuota(context.Background(), h.svc, "user-a", "A100", 2)
	if err != nil {
		t.Fatal(err)
	}
	if ok {
		t.Fatal("user-a was allowed past a quota of 1")
	}
	ok, _, err = job.CheckQuota(context.Background(), h.svc, "user-b", "A100", 8)
	if err != nil {
		t.Fatal(err)
	}
	if !ok {
		t.Fatal("user-b in the same company was rejected; admit is not an org sum")
	}
}
