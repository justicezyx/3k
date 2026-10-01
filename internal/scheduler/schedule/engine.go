package schedule

// Score selects the best (cluster, node) for workload using the internal global scheduler.
func Score(workload Workload, clusters []ClusterSnapshot, weights Weights) Placement {
	return schedule(workload, clusters, weights)
}

// ScoreForCluster returns the global best placement only if it belongs to requestCpodID.
func ScoreForCluster(workload Workload, clusters []ClusterSnapshot, weights Weights, requestCpodID string) Placement {
	p := Score(workload, clusters, weights)
	if !p.OK || p.Candidate.Cluster.CpodID != requestCpodID {
		return Placement{OK: false, TotalScore: p.TotalScore, Breakdown: p.Breakdown}
	}
	return p
}

// FeasibleCandidates lists candidates that pass hard resource and affinity checks.
func FeasibleCandidates(workload Workload, clusters []ClusterSnapshot) []Candidate {
	var out []Candidate
	for _, c := range enumerateCandidates(clusters) {
		if feasibleCandidate(workload, c) {
			out = append(out, c)
		}
	}
	return out
}
