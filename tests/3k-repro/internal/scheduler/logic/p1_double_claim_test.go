package logic

import (
	"context"
	"sync"
	"testing"

	"sxwl/3k/internal/scheduler/model"
	"sxwl/3k/internal/scheduler/types"
)

// minDoubleClaims is the floor for 30 overlapping pulls. The gate used to be
// both == 0, so 1/30 passed. 20/30 still allows run-to-run movement (27 and
// 28 have both been observed) and rejects a rare hit.
const minDoubleClaims = 20

// TestP1_DoubleClaim_BothPullersGetJob shows two callers of CpodJob can both
// return one unassigned train job. The claim is UPDATE ... WHERE job_id = ?
// with the sql.Result discarded, so the loser is not skipped. The row still
// stores one cpod_id. Statements are autocommit; the session isolation is
// REPEATABLE-READ and is not what lets both callers emit.
func TestP1_DoubleClaim_BothPullersGetJob(t *testing.T) {
	h := newHarness(t)
	const n = 30
	both := 0
	for i := 0; i < n; i++ {
		if _, err := h.db.Exec(`DELETE FROM sys_user_job`); err != nil {
			t.Fatal(err)
		}
		if _, err := h.db.Exec(`DELETE FROM sys_cpod_node`); err != nil {
			t.Fatal(err)
		}
		h.insertNode("island-a", "a-0", "A100", 8, 8, 64<<30)
		h.insertNode("island-b", "b-0", "A100", 8, 8, 64<<30)
		h.insertTrain("train-double", "user-1", "", "A100", 1, model.StatusObtainNeedSend, model.StatusNotAssigned)

		var start sync.WaitGroup
		start.Add(1)
		var ready sync.WaitGroup
		ready.Add(2)
		var done sync.WaitGroup
		done.Add(2)
		type result struct {
			names []string
			err   error
		}
		out := make([]result, 2)
		for k, id := range []string{"island-a", "island-b"} {
			k, id := k, id
			go func() {
				defer done.Done()
				ready.Done()
				start.Wait()
				resp, err := NewCpodJobLogic(context.Background(), h.svc).CpodJob(&types.CpodJobReq{CpodId: id})
				out[k] = result{names: trainNames(resp), err: err}
			}()
		}
		ready.Wait()
		start.Done()
		done.Wait()

		if out[0].err != nil || out[1].err != nil {
			t.Fatalf("run %d errors: %v %v", i, out[0].err, out[1].err)
		}
		if contains(out[0].names, "train-double") && contains(out[1].names, "train-double") {
			oneStoredClaim(t, h, out[0].names, out[1].names)
			both++
		}
	}
	t.Logf("P1 double-claim count %d/%d (both pullers received train-double; gate is >= %d)", both, n, minDoubleClaims)
	if both < minDoubleClaims {
		t.Fatalf("double claim %d/%d is below the gate of %d/%d", both, n, minDoubleClaims, n)
	}

	// A later pull from the other island does not emit. The overlap is the
	// concurrent window: the update is WHERE job_id and sql.Result is discarded.
	if _, err := h.db.Exec(`DELETE FROM sys_user_job`); err != nil {
		t.Fatal(err)
	}
	if _, err := h.db.Exec(`DELETE FROM sys_cpod_node`); err != nil {
		t.Fatal(err)
	}
	h.insertNode("island-a", "a-0", "A100", 8, 8, 64<<30)
	h.insertNode("island-b", "b-0", "A100", 8, 8, 64<<30)
	h.insertTrain("train-seq", "user-1", "", "A100", 1, model.StatusObtainNeedSend, model.StatusNotAssigned)
	first := h.pull("island-a")
	second := h.pull("island-b")
	if !contains(trainNames(first), "train-seq") {
		t.Fatal("first island did not receive train-seq")
	}
	if contains(trainNames(second), "train-seq") {
		t.Fatal("sequential second island also received train-seq")
	}
	cpod, _, _, _ := h.trainRow("train-seq")
	if cpod != "island-a" {
		t.Fatalf("sequential claim stored cpod_id %q", cpod)
	}
}

// oneStoredClaim checks the double response against the single train row.
func oneStoredClaim(t *testing.T, h *harness, a, b []string) {
	t.Helper()
	if !contains(a, "train-double") || !contains(b, "train-double") {
		t.Fatal("caller did not pass a double claim")
	}
	var rows int
	var cpod string
	err := h.db.QueryRow(`SELECT COUNT(*), IFNULL(MAX(cpod_id), '') FROM sys_user_job WHERE job_name = 'train-double'`).Scan(&rows, &cpod)
	if err != nil {
		t.Fatal(err)
	}
	if rows != 1 {
		t.Fatalf("train-double rows = %d, want 1", rows)
	}
	if cpod != "island-a" && cpod != "island-b" {
		t.Fatalf("stored cpod_id = %q, want one of the two islands", cpod)
	}
}
