package schedule

// Score selects the best candidate using the default kube-style scheduling framework.
func Score(workload Workload, clusters []ClusterSnapshot, weights Weights) Placement {
	return DefaultFramework(weights).Schedule(workload, clusters)
}

// ScoreForCluster returns the global best placement only if it belongs to requestCpodID.
func ScoreForCluster(workload Workload, clusters []ClusterSnapshot, weights Weights, requestCpodID string) Placement {
	p := Score(workload, clusters, weights)
	if !p.OK || p.Candidate.Cluster.CpodID != requestCpodID {
		return Placement{OK: false, TotalScore: p.TotalScore, PluginScores: p.PluginScores}
	}
	return p
}

// FeasibleCandidates lists candidates passing all Filter plugins (test / debug helper).
func FeasibleCandidates(workload Workload, clusters []ClusterSnapshot) []Candidate {
	fw := DefaultFramework(DefaultWeights())
	state := NewCycleState()
	var out []Candidate
	for _, c := range enumerateCandidates(clusters) {
		if fw.runFilters(state, workload, c) {
			out = append(out, c)
		}
	}
	return out
}
