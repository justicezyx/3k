package logic

import (
	"context"
	"testing"

	"sxwl/3k/internal/scheduler/types"
)

// TestP18_BalanceReadBeforeInsert shows JobCreate rejects only balance < 0
// and then inserts. It does not debit user_balance and it does not take a
// row lock. Two sequential creates on 0.00 both succeed; that is the check,
// not a concurrency rate.
func TestP18_BalanceReadBeforeInsert(t *testing.T) {
	h := newHarness(t)
	h.exec(`INSERT INTO user_balance (user_id, new_user_id, balance) VALUES (1, 'user-1', 0.00)`)

	for i := 0; i < 2; i++ {
		_, err := NewJobCreateLogic(context.Background(), h.svc).JobCreate(&types.JobCreateReq{
			UserID:    "user-1",
			GpuNumber: 1,
			GpuType:   "A100",
			JobType:   "PyTorch",
		})
		if err != nil {
			t.Fatalf("create %d on balance 0.00: %v", i, err)
		}
	}
	var balance float64
	if err := h.db.QueryRow(`SELECT balance FROM user_balance WHERE new_user_id = 'user-1'`).Scan(&balance); err != nil {
		t.Fatal(err)
	}
	if balance != 0 {
		t.Fatalf("balance changed to %.2f; create debited the row", balance)
	}
	var jobs int
	if err := h.db.QueryRow(`SELECT COUNT(*) FROM sys_user_job WHERE new_user_id = 'user-1'`).Scan(&jobs); err != nil {
		t.Fatal(err)
	}
	if jobs != 2 {
		t.Fatalf("job rows = %d, want 2", jobs)
	}

	_, err := NewJobCreateLogic(context.Background(), h.svc).JobCreate(&types.JobCreateReq{
		UserID: "user-broke", GpuNumber: 1, GpuType: "A100", JobType: "PyTorch",
	})
	if err == nil {
		t.Fatal("create with no balance row succeeded; the read is not the < 0 check")
	}
	h.exec(`INSERT INTO user_balance (user_id, new_user_id, balance) VALUES (2, 'user-neg', -1.00)`)
	_, err = NewJobCreateLogic(context.Background(), h.svc).JobCreate(&types.JobCreateReq{
		UserID: "user-neg", GpuNumber: 1, GpuType: "A100", JobType: "PyTorch",
	})
	if err == nil {
		t.Fatal("negative balance was accepted")
	}
	var neg float64
	if err := h.db.QueryRow(`SELECT balance FROM user_balance WHERE new_user_id = 'user-neg'`).Scan(&neg); err != nil {
		t.Fatal(err)
	}
	if neg != -1 {
		t.Fatalf("rejected create changed negative balance to %.2f", neg)
	}
}
