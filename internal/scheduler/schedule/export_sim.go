package schedule

// ClusterLoadBalanceRaw is the load_balance dimension for a CPod (same for all nodes in the cluster).
func ClusterLoadBalanceRaw(cluster ClusterSnapshot) int64 {
	return scoreLoadBalance(Workload{}, Candidate{Cluster: cluster})
}

// ClusterCacheLocalityRaw is cache_locality for workload on a CPod.
func ClusterCacheLocalityRaw(workload Workload, cluster ClusterSnapshot) int64 {
	return scoreCacheLocality(workload, Candidate{Cluster: cluster})
}

// ClusterCostRaw is the cost dimension for a CPod.
func ClusterCostRaw(cluster ClusterSnapshot) int64 {
	return scoreCost(Workload{}, Candidate{Cluster: cluster})
}

// NormalizeScores min-max normalizes raw scores across candidates (exported for design simulations).
func NormalizeScores(scores []int64) []int64 {
	return normalizeScores(scores)
}
