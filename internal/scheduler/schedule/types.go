package schedule

import "time"

// Workload is the unit of global scheduling.
type Workload struct {
	GPUProduct string
	GPUCount   int64
	CPUCores   int64
	MemBytes   int64

	PinCluster string
	CacheIDs   []string
}

// NodeSnapshot is schedulable capacity on one Kubernetes node.
type NodeSnapshot struct {
	ID             int64
	CpodID         string
	NodeName       string
	GPUProduct     string
	GPUAllocatable int64
	GPUTotal       int64
	CPUAllocatable int64
	MemAllocatable int64
	UpdatedAt      time.Time
}

// ClusterSnapshot aggregates one CPod for scoring.
type ClusterSnapshot struct {
	CpodID          string
	Nodes           []NodeSnapshot
	CacheIDs        map[string]struct{}
	GPUPricePerHour float64
}

// Candidate is a feasible placement target.
type Candidate struct {
	Cluster ClusterSnapshot
	Node    NodeSnapshot
}

// Placement records the outcome of scheduling one workload.
type Placement struct {
	OK         bool
	Candidate  Candidate
	TotalScore int64
	Breakdown  []ScoreBreakdown
}

// Weights configures soft scoring dimensions. Zero weight disables a dimension.
type Weights struct {
	ResourceHeadroom float64
	LoadBalance      float64
	CacheLocality    float64
	Cost             float64
}

// DefaultWeights returns production-style defaults documented in GLOBAL_SCHEDULER_SCORING.md.
func DefaultWeights() Weights {
	return Weights{
		ResourceHeadroom: 0.35,
		LoadBalance:      0.25,
		CacheLocality:    0.25,
		Cost:             0.15,
	}
}
