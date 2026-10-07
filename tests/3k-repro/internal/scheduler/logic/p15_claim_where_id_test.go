package logic

import (
	"strings"
	"testing"
)

// TestP15_ClaimWhereIDMissesTrainKey runs the claim statement from the design
// note against the shipped train table. The train primary key is job_id.
// There is no id column, so WHERE id = ? does not update the row. The same
// shape matches sys_inference, whose primary key is id.
func TestP15_ClaimWhereIDMissesTrainKey(t *testing.T) {
	h := newHarness(t)
	h.insertTrain("train-key", "user-1", "", "A100", 1, 0, 0)

	_, err := h.db.Exec(`UPDATE sys_user_job SET cpod_id = ? WHERE id = ? AND (cpod_id = '' OR cpod_id IS NULL)`, "island-a", 1)
	if err == nil {
		t.Fatal("design claim UPDATE ... WHERE id = ? succeeded on sys_user_job")
	}
	if !strings.Contains(strings.ToLower(err.Error()), "unknown") && !strings.Contains(err.Error(), "id") {
		t.Fatalf("unexpected error: %v", err)
	}
	cpod, _, _, _ := h.trainRow("train-key")
	if cpod != "" {
		t.Fatalf("train cpod_id changed to %q despite the failed claim", cpod)
	}

	var jobID int
	if err := h.db.QueryRow(`SELECT job_id FROM sys_user_job WHERE job_name = 'train-key'`).Scan(&jobID); err != nil {
		t.Fatal(err)
	}
	res, err := h.db.Exec(`UPDATE sys_user_job SET cpod_id = ? WHERE job_id = ? AND (cpod_id = '' OR cpod_id IS NULL)`, "island-a", jobID)
	if err != nil {
		t.Fatal(err)
	}
	n, _ := res.RowsAffected()
	if n != 1 {
		t.Fatalf("WHERE job_id affected %d rows; train key is not job_id", n)
	}

	h.exec(`INSERT INTO sys_inference
		(service_name, user_id, new_user_id, cpod_id, status, gpu_number, gpu_type, metadata, url)
		VALUES ('infer-key', 1, 'user-1', '', 0, 1, 'A100', '{}', '')`)
	var inferID int
	if err := h.db.QueryRow(`SELECT id FROM sys_inference WHERE service_name = 'infer-key'`).Scan(&inferID); err != nil {
		t.Fatal(err)
	}
	res, err = h.db.Exec(`UPDATE sys_inference SET cpod_id = ? WHERE id = ? AND (cpod_id = '' OR cpod_id IS NULL)`, "island-a", inferID)
	if err != nil {
		t.Fatal(err)
	}
	n, _ = res.RowsAffected()
	if n != 1 {
		t.Fatalf("inference WHERE id affected %d", n)
	}
}
