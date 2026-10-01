package schedule

import "time"

// WorkloadKind identifies schedulable work types on the global scheduler.
type WorkloadKind int

const (
	WorkloadTraining WorkloadKind = iota
	WorkloadInference
	WorkloadJupyterLab
	WorkloadAppJob
)

// Workload is the unit of global scheduling.
type Workload struct {
	ID   string
	Kind WorkloadKind

	GPUProduct string
	GPUCount   int64
	CPUCores   int64
	MemBytes   int64

	// PinCluster, when non-empty, restricts placement to that CPod.
	PinCluster string
	// CacheIDs lists model/dataset/adapter ids that benefit from cluster-local cache.
	CacheIDs []string
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
	CpodID string
	Nodes  []NodeSnapshot
	// CacheIDs is the set of data_ids present on this cluster (from sys_cpod_cache).
	CacheIDs map[string]struct{}
	// GPUPricePerHour is optional; zero means cost factor is skipped.
	GPUPricePerHour float64
}

// Candidate is a feasible placement target.
type Candidate struct {
	Cluster ClusterSnapshot
	Node    NodeSnapshot
}

// FactorScore records one scoring dimension for explainability.
type FactorScore struct {
	Name       string
	Raw        float64
	Normalized float64
	Weight     float64
	Weighted   float64
}

// Placement records the outcome of scheduling one workload.
type Placement struct {
	OK         bool
	Candidate  Candidate
	TotalScore float64
	Factors    []FactorScore
}

// Weights configures soft scoring dimensions. Zero weight disables a factor.
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
