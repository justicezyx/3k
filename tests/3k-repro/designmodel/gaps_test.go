package designmodel

import "testing"

// These tests encode sentences from the revised design note.
// They pass by construction: the missing write is absent because this file
// does not perform it. They do not call NascentCore/3k and are not a
// reproduction of shipped behavior. Shipped-code checks live next to the
// real packages.

// §9.2: "Debit CAS: `inv_version` (and optional `reserved_gpu`) on `sys_cpod_node`; `WHERE inv_version=? AND gpu_allocatable>=?`."
// §9.2: "`allocatable = max(0, observed − reserved)`. Never clobber reservations."
// §9.8 names the columns and does not name a job id or a decrease.

type reservedNode struct {
	invVersion     int
	gpuAllocatable int
	reservedGPU    int
}

func debitCAS(n *reservedNode, seenVersion, need int) bool {
	if n.invVersion != seenVersion || n.gpuAllocatable < need {
		return false
	}
	n.invVersion++
	return true
}

func allocatableFromObserved(observed, reserved int) int {
	v := observed - reserved
	if v < 0 {
		return 0
	}
	return v
}

// applyReservedDecrease is empty on purpose: no sentence names a decrease
// on success, failure, stop, overdraft, or heartbeat expiry.
func applyReservedDecrease(event string, n *reservedNode) bool {
	switch event {
	case "success", "failure", "stop", "overdraft", "heartbeat-expiry":
		return false
	default:
		return false
	}
}

func TestP3_ReservedGPUHasNoJobKeyAndNoDecrease(t *testing.T) {
	n := &reservedNode{invVersion: 3, gpuAllocatable: 8, reservedGPU: 8}
	if !debitCAS(n, 3, 4) {
		t.Fatal("debit WHERE did not match")
	}
	// The debit statement does not carry a job id. There is no field to set.
	if n.reservedGPU != 8 {
		t.Fatalf("debit invented a reserved_gpu write: %d", n.reservedGPU)
	}
	observedFree := 8
	if got := allocatableFromObserved(observedFree, n.reservedGPU); got != 0 {
		t.Fatalf("allocatable = %d, want max(0, 8-8)", got)
	}
	for _, event := range []string{"success", "failure", "stop", "overdraft", "heartbeat-expiry"} {
		if applyReservedDecrease(event, n) {
			t.Fatalf("design named a reserved_gpu decrease on %s", event)
		}
	}
	if n.reservedGPU != 8 {
		t.Fatalf("reserved_gpu changed to %d with no named decrease and no job key", n.reservedGPU)
	}
}

// §9.3: "Drain: island marks draining (who may: owner or ops — still OPEN in scale packet; do not invent)."
// §9.3: "Central clears sticky `cpod_id` and re-admits."
// obtain_status, a product check, and a reservation release are not in that paragraph.

type drainJob struct {
	cpodID              string
	reservedGPU         int
	obtainStatusWritten bool
	productChecked      bool
	reservationReleased bool
}

func markDraining(actor string) bool {
	// The note leaves the actor OPEN. Naming owner or ops here would invent it.
	switch actor {
	case "owner", "ops":
		return false
	default:
		return false
	}
}

func reAdmit(j *drainJob) {
	j.cpodID = ""
}

func TestP13_DrainMarkerIsUnnamed(t *testing.T) {
	if markDraining("owner") || markDraining("ops") {
		t.Fatal("a drain writer is named; the note leaves the actor OPEN")
	}
	j := &drainJob{cpodID: "island-a", reservedGPU: 8}
	reAdmit(j)
	if j.cpodID != "" {
		t.Fatal("re-admit did not clear sticky cpod_id")
	}
	if j.obtainStatusWritten || j.productChecked || j.reservationReleased {
		t.Fatal("re-admit named a status write, a product check, or a reservation release")
	}
	if j.reservedGPU != 8 {
		t.Fatalf("reservation released without a named write: %d", j.reservedGPU)
	}
}

// §3 M7: "`updated_at > NOW()-30 MINUTE` is eligibility. It is not fencing."
// §9.2: "Keep 30m as health only (M7)."
// Failure row: "Island death, sticky job | Stranded | Drain + re-admit (Phase B)."
// No sentence ties the 30-minute predicate to that drain mark.

func freshEnough(ageMinutes int) bool {
	return ageMinutes <= 30
}

func silenceDrains(ageMinutes int, cpodID string) (drained bool, cleared string) {
	_ = ageMinutes
	return false, cpodID
}

func TestP4_SilenceDoesNotDrain(t *testing.T) {
	if freshEnough(31) {
		t.Fatal("31 minutes of silence is still eligibility")
	}
	if !freshEnough(30) {
		t.Fatal("30 minutes was treated as fencing")
	}
	drained, cpodID := silenceDrains(31, "island-a")
	if drained || cpodID != "island-a" {
		t.Fatalf("30-minute silence drained=%v cpod_id=%q; the note does not tie the predicate to drain", drained, cpodID)
	}
}

// §1: "Match work to spare capacity, meter quota/balance, without operating every DC's kubelet."
// §9.1 steps end at heartbeat. Bill, cron, and balance stop are not in that list.

func TestP19_ControlLoopHasNoBillWrite(t *testing.T) {
	steps := []string{
		"create",
		"admit",
		"place",
		"bind",
		"deliver",
		"reconcile",
		"heartbeat",
	}
	for _, s := range steps {
		if s == "bill" || s == "balance-stop" {
			t.Fatalf("control loop names %s", s)
		}
	}
	type job struct {
		balance float64
		running bool
		billed  bool
		stopped bool
	}
	j := &job{balance: -5, running: true}
	for _, s := range steps {
		switch s {
		case "create", "admit", "place", "bind", "deliver", "reconcile", "heartbeat":
		default:
			t.Fatalf("unexpected step %s", s)
		}
	}
	if j.billed || j.stopped || !j.running {
		t.Fatalf("written loop metered or stopped a negative balance: %+v", *j)
	}
	if j.balance >= 0 {
		t.Fatal("scenario was not insolvent")
	}
}

// §9.4: "resource headroom, load balance, cache locality, cost."
// sys_cpod_cache does not appear in that section, so cache locality has no input.

type scoreIsland struct {
	headroom     int
	load         int
	cost         int
	cacheObjects int
}

func softScore(i scoreIsland) int {
	// cacheObjects is not a named input of the written score.
	return i.headroom - i.load - i.cost
}

func TestP24_CacheLocalityHasNoNamedInput(t *testing.T) {
	cached := scoreIsland{headroom: 2, load: 1, cost: 1, cacheObjects: 100}
	empty := scoreIsland{headroom: 2, load: 1, cost: 1, cacheObjects: 0}
	if softScore(cached) != softScore(empty) {
		t.Fatal("cache locality changed the score without a named input")
	}
	if cached.cacheObjects == empty.cacheObjects {
		t.Fatal("the two islands do not differ in cache")
	}
}

// §9.3: "CKPT contract: durable copy off-island (OSS) or PVC not cascaded on drain."
// Flush, a signal, and which copy the next island mounts are absent.
// The next sentence only "clears sticky `cpod_id` and re-admits."

type ckptContract struct {
	offIslandOSS   bool
	pvcNotCascaded bool
	flushSignal    string
	resumeMount    string
}

func designCKPT() ckptContract {
	return ckptContract{offIslandOSS: true, pvcNotCascaded: true}
}

func TestP27_CheckpointContractHasNoFlushOrResume(t *testing.T) {
	c := designCKPT()
	if !c.offIslandOSS && !c.pvcNotCascaded {
		t.Fatal("contract dropped both alternatives the note names")
	}
	cpodID := "island-a"
	cpodID = "" // the only named follow-up write
	if cpodID != "" {
		t.Fatal("re-admit left sticky cpod_id")
	}
	if c.flushSignal != "" || c.resumeMount != "" {
		t.Fatalf("design named flush %q or resume mount %q", c.flushSignal, c.resumeMount)
	}
}

// §2.1 uses P4 for quota, org, and balance.
// §9.1 uses that same id for an optional create-time bind, and says a poll
// still scores across the fleet when inbound notify is forbidden.
// Admit leaves cpod_id empty. Create-time bind writes it and does not read balance.

type p4Job struct {
	balance float64
	quotaOK bool
	cpodID  string
}

func applyP4AsAdmit(j p4Job) p4Job {
	if j.balance < 0 || !j.quotaOK {
		j.cpodID = ""
		return j
	}
	// Admit does not bind.
	j.cpodID = ""
	return j
}

func applyP4AsCreateTimeBind(j p4Job) p4Job {
	// This reading writes cpod_id at create and does not consult quota or balance.
	j.cpodID = "island-a"
	return j
}

func TestP28_P4MeansAdmitAndCreateTimeBind(t *testing.T) {
	admitted := applyP4AsAdmit(p4Job{balance: 1, quotaOK: true})
	if admitted.cpodID != "" {
		t.Fatal("the admit reading of P4 bound cpod_id at create")
	}
	bound := applyP4AsCreateTimeBind(p4Job{balance: -1, quotaOK: false})
	if bound.cpodID == "" {
		t.Fatal("the create-time-bind reading of P4 did not write cpod_id")
	}
	if bound.balance >= 0 || bound.quotaOK {
		t.Fatal("create-time bind reading was given a job that also passes admit")
	}
	if admitted.cpodID == bound.cpodID {
		t.Fatal("the two readings of P4 produced the same bind result")
	}
}
