package logic

import (
	"context"
	"testing"

	"sxwl/3k/internal/scheduler/types"
)

// TestP2_HeartbeatOverwritesDebitedAllocatable shows POST /cpod/status writes
// gpu_allocatable from the heartbeat's observed free count. A prior debit
// stored in that same column is replaced. The node table has no reserved column.
func TestP2_HeartbeatOverwritesDebitedAllocatable(t *testing.T) {
	h := newHarness(t)
	id := h.insertNode("island-a", "node-0", "A100", 8, 16, 64<<30)
	h.exec(`UPDATE sys_cpod_node SET gpu_allocatable = 0 WHERE id = ?`, id)

	var reservedCols int
	if err := h.db.QueryRow(`SELECT COUNT(*) FROM information_schema.columns
		WHERE table_schema = ? AND table_name = 'sys_cpod_node' AND column_name = 'reserved_gpu'`, h.name).Scan(&reservedCols); err != nil {
		t.Fatal(err)
	}
	if reservedCols != 0 {
		t.Fatalf("sys_cpod_node has reserved_gpu (%d); the clobber premise changed", reservedCols)
	}

	_, err := NewCpodStatusLogic(context.Background(), h.svc).CpodStatus(&types.CPODStatusReq{
		CPODID: "island-a",
		UserID: "owner",
		ResourceInfo: types.ResourceInfo{
			CPODID:      "island-a",
			CPODVersion: "v1.0",
			Nodes: []types.NodeInfo{{
				Name:           "node-0",
				GPUTotal:       8,
				GPUAllocatable: 8, // observed free; pods are not bound yet
				GPUInfo:        types.GPUInfo{Vendor: "nvidia", Prod: "A100"},
				CPUInfo:        types.CPUInfo{Cores: 16},
				MemInfo:        types.MemInfo{Size: 65536},
			}},
		},
	})
	if err != nil {
		t.Fatal(err)
	}
	gpu, _ := h.nodeAlloc(id)
	if gpu != 8 {
		t.Fatalf("gpu_allocatable = %d after heartbeat, want 8 (debit of 0 was overwritten)", gpu)
	}
}
