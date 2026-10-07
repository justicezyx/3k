package logic

import (
	"context"
	"encoding/json"
	"strings"
	"testing"

	"sxwl/3k/internal/scheduler/types"
)

// payoutName is true when a table or route name is a payout, not a consumer
// balance or billing path. The scan is every loaded table and every Path in
// routes.go, so a new name such as user_payout or /pay/settle fails.
func payoutName(name string) bool {
	n := strings.ToLower(name)
	for _, word := range []string{"payout", "payee", "disburse", "settlement", "settle"} {
		if strings.Contains(n, word) {
			return true
		}
	}
	return false
}

func routePaths(routes string) []string {
	var paths []string
	for _, line := range strings.Split(routes, "\n") {
		line = strings.TrimSpace(line)
		if !strings.HasPrefix(line, "Path:") {
			continue
		}
		start := strings.Index(line, `"`)
		end := strings.LastIndex(line, `"`)
		if start < 0 || end <= start {
			continue
		}
		paths = append(paths, line[start+1:end])
	}
	return paths
}

// TestP23_IslandOwnerPaymentHasNoAPI shows the loaded schema has the consumer
// billing tables and no payout table, and routes.go has consumer /pay paths
// and no payout path.
func TestP23_IslandOwnerPaymentHasNoAPI(t *testing.T) {
	h := newHarness(t)
	rows, err := h.db.Query(`SELECT table_name FROM information_schema.tables WHERE table_schema = ?`, h.name)
	if err != nil {
		t.Fatal(err)
	}
	defer rows.Close()
	tables := map[string]bool{}
	for rows.Next() {
		var name string
		if err := rows.Scan(&name); err != nil {
			t.Fatal(err)
		}
		tables[name] = true
		if payoutName(name) {
			t.Fatalf("schema has payout table %s", name)
		}
	}
	for _, need := range []string{"user_balance", "user_billing", "user_recharge"} {
		if !tables[need] {
			t.Fatalf("consumer billing table %s is missing", need)
		}
	}

	routes := osRead(t, "internal/scheduler/handler/routes.go")
	paths := routePaths(routes)
	if len(paths) == 0 {
		t.Fatal("routes.go yielded no Path entries")
	}
	seenBalance := false
	for _, path := range paths {
		if payoutName(path) {
			t.Fatalf("routes.go has payout path %s", path)
		}
		if path == "/pay/balance" {
			seenBalance = true
		}
	}
	if !seenBalance {
		t.Fatal("routes.go is missing /pay/balance; file read hit the wrong source")
	}
}

// TestP29_ReplicaInferHasNoReplicaColumn shows sys_inference has no replica
// column. InferenceDeploy writes min and max instance bounds into metadata
// JSON and still stores one cpod_id.
func TestP29_ReplicaInferHasNoReplicaColumn(t *testing.T) {
	h := newHarness(t)
	rows, err := h.db.Query(`SELECT column_name FROM information_schema.columns
		WHERE table_schema = ? AND table_name = 'sys_inference'`, h.name)
	if err != nil {
		t.Fatal(err)
	}
	defer rows.Close()
	cols := map[string]bool{}
	for rows.Next() {
		var c string
		if err := rows.Scan(&c); err != nil {
			t.Fatal(err)
		}
		cols[c] = true
	}
	for _, banned := range []string{"replicas", "min_replicas", "max_replicas", "min_instances", "max_instances"} {
		if cols[banned] {
			t.Fatalf("sys_inference has column %s", banned)
		}
	}
	if !cols["cpod_id"] || !cols["metadata"] {
		t.Fatal("expected cpod_id and metadata columns")
	}

	h.exec(`INSERT INTO user_balance (user_id, new_user_id, balance) VALUES (1, 'user-1', 1.00)`)
	resp, err := NewInferenceDeployLogic(context.Background(), h.svc).InferenceDeploy(&types.InferenceDeployReq{
		UserID:       "user-1",
		GpuModel:     "A100",
		GpuCount:     1,
		MinInstances: 2,
		MaxInstances: 4,
		CpodID:       "island-a",
	})
	if err != nil {
		t.Fatal(err)
	}
	var cpod, meta string
	if err := h.db.QueryRow(`SELECT cpod_id, metadata FROM sys_inference WHERE service_name = ?`, resp.ServiceName).Scan(&cpod, &meta); err != nil {
		t.Fatal(err)
	}
	if cpod != "island-a" {
		t.Fatalf("cpod_id = %q", cpod)
	}
	var decoded types.InferenceService
	if err := json.Unmarshal([]byte(meta), &decoded); err != nil {
		t.Fatal(err)
	}
	if decoded.MinInstances != 2 || decoded.MaxInstances != 4 {
		t.Fatalf("writer stored min=%d max=%d meta=%s", decoded.MinInstances, decoded.MaxInstances, meta)
	}
}

// TestP31_NotBannedIsConfigMapNotATable rejects the puller from Config.BannedCpod
// before the node query. sys_cpod_node has no ban column.
func TestP31_NotBannedIsConfigMapNotATable(t *testing.T) {
	h := newHarness(t)
	var banCols int
	if err := h.db.QueryRow(`SELECT COUNT(*) FROM information_schema.columns
		WHERE table_schema = ? AND table_name = 'sys_cpod_node'
		AND column_name IN ('banned', 'ban', 'banned_at')`, h.name).Scan(&banCols); err != nil {
		t.Fatal(err)
	}
	if banCols != 0 {
		t.Fatalf("node table has a ban column (%d)", banCols)
	}
	id := h.insertNode("island-a", "n0", "A100", 8, 8, 64<<30)
	h.insertTrain("train-ban", "user-1", "", "A100", 1, 0, 0)
	h.svc.Config.BannedCpod["island-a"] = "1"
	_, err := NewCpodJobLogic(context.Background(), h.svc).CpodJob(&types.CpodJobReq{CpodId: "island-a"})
	if err == nil || !strings.Contains(err.Error(), "illegal") {
		t.Fatalf("banned puller error = %v", err)
	}
	cpod, _, _, _ := h.trainRow("train-ban")
	if cpod != "" {
		t.Fatalf("banned puller still claimed the job onto %q", cpod)
	}
	gpu, _ := h.nodeAlloc(id)
	if gpu != 8 {
		t.Fatalf("banned puller debited the node to %d", gpu)
	}
}
