package schedule

import (
	"fmt"
	"sort"
)

// MaxNodeScore matches kube-scheduler's default per-plugin score ceiling after normalization.
const MaxNodeScore int64 = 100

// Code mirrors core/v1 PodScheduled reasons where useful for logs.
const (
	ReasonUnschedulable = "Unschedulable"
)

// Status is the result of a Filter plugin (same role as framework.Status in kube-scheduler).
type Status struct {
	Code   int
	Reason string
}

func (s *Status) IsSuccess() bool {
	return s == nil || s.Code == 0
}

func NewStatus(reason string) *Status {
	return &Status{Code: 1, Reason: reason}
}

// CycleState carries per-scheduling-cycle data (extension point for future PreFilter/Reserve plugins).
type CycleState struct {
	data map[string]interface{}
}

func NewCycleState() *CycleState {
	return &CycleState{data: make(map[string]interface{})}
}

// FilterPlugin implements hard constraints (kube Filter extension point).
type FilterPlugin interface {
	Name() string
	Filter(state *CycleState, workload Workload, candidate Candidate) *Status
}

// ScorePlugin implements soft preferences (kube Score extension point).
type ScorePlugin interface {
	Name() string
	Score(state *CycleState, workload Workload, candidate Candidate) (int64, *Status)
}

// ScorePluginWeight binds a score plugin to a profile weight (kube plugin config weight).
type ScorePluginWeight struct {
	Plugin ScorePlugin
	Weight int64
}

// Framework runs the kube-style scheduling cycle: Filter → Score → NormalizeScore → weighted sum.
type Framework struct {
	filterPlugins []FilterPlugin
	scorePlugins  []ScorePluginWeight
}

// NewFramework builds a scheduler profile from plugins.
func NewFramework(filters []FilterPlugin, scores []ScorePluginWeight) *Framework {
	return &Framework{
		filterPlugins: filters,
		scorePlugins:  scores,
	}
}

// DefaultFramework returns the built-in profile used by the global scheduler.
func DefaultFramework(weights Weights) *Framework {
	return NewFramework(
		defaultFilterPlugins(),
		defaultScorePlugins(weights),
	)
}

// Schedule picks the highest-scoring feasible (cluster, node) for workload.
func (f *Framework) Schedule(workload Workload, clusters []ClusterSnapshot) Placement {
	state := NewCycleState()
	candidates := enumerateCandidates(clusters)
	if len(candidates) == 0 {
		return Placement{OK: false}
	}

	feasible := make([]Candidate, 0, len(candidates))
	for _, c := range candidates {
		if f.runFilters(state, workload, c) {
			feasible = append(feasible, c)
		}
	}
	if len(feasible) == 0 {
		return Placement{OK: false}
	}

	type candidateScore struct {
		idx        int
		total      int64
		pluginRows []PluginScore
	}
	scored := make([]candidateScore, len(feasible))
	for i := range feasible {
		scored[i] = candidateScore{idx: i, pluginRows: make([]PluginScore, 0, len(f.scorePlugins))}
	}

	for _, spw := range f.scorePlugins {
		if spw.Weight <= 0 {
			continue
		}
		raw := make([]int64, len(feasible))
		for i, c := range feasible {
			s, status := spw.Plugin.Score(state, workload, c)
			if !status.IsSuccess() {
				raw[i] = 0
				continue
			}
			raw[i] = s
		}
		normalized := normalizePluginScores(raw)
		for i := range feasible {
			weighted := normalized[i] * spw.Weight
			scored[i].total += weighted
			scored[i].pluginRows = append(scored[i].pluginRows, PluginScore{
				Name:       spw.Plugin.Name(),
				Score:      raw[i],
				Normalized: normalized[i],
				Weight:     spw.Weight,
				Weighted:   weighted,
			})
		}
	}

	sort.Slice(scored, func(i, j int) bool {
		if scored[i].total != scored[j].total {
			return scored[i].total > scored[j].total
		}
		ci, cj := feasible[scored[i].idx], feasible[scored[j].idx]
		if ci.Cluster.CpodID != cj.Cluster.CpodID {
			return ci.Cluster.CpodID < cj.Cluster.CpodID
		}
		return ci.Node.NodeName < cj.Node.NodeName
	})

	best := scored[0]
	return Placement{
		OK:           true,
		Candidate:    feasible[best.idx],
		TotalScore:   best.total,
		PluginScores: best.pluginRows,
	}
}

func (f *Framework) runFilters(state *CycleState, workload Workload, candidate Candidate) bool {
	for _, plugin := range f.filterPlugins {
		if status := plugin.Filter(state, workload, candidate); !status.IsSuccess() {
			return false
		}
	}
	return true
}

func enumerateCandidates(clusters []ClusterSnapshot) []Candidate {
	var out []Candidate
	for _, cluster := range clusters {
		for _, node := range cluster.Nodes {
			out = append(out, Candidate{
				Cluster: cluster,
				Node:    node,
			})
		}
	}
	return out
}

// normalizePluginScores implements the same scaling as kube-scheduler ScorePlugin NormalizeScore.
func normalizePluginScores(scores []int64) []int64 {
	if len(scores) == 0 {
		return nil
	}
	min, max := scores[0], scores[0]
	for _, s := range scores[1:] {
		if s < min {
			min = s
		}
		if s > max {
			max = s
		}
	}
	out := make([]int64, len(scores))
	for i, s := range scores {
		if max == min {
			if s > 0 {
				out[i] = MaxNodeScore
			} else {
				out[i] = 0
			}
			continue
		}
		out[i] = MaxNodeScore * (s - min) / (max - min)
	}
	return out
}

// weightsToPluginWeights converts YAML fractional weights to kube-style integer plugin weights.
func weightsToPluginWeights(w Weights) map[string]int64 {
	return map[string]int64{
		PluginResourceHeadroom: scaleWeight(w.ResourceHeadroom),
		PluginLoadBalance:      scaleWeight(w.LoadBalance),
		PluginCacheLocality:    scaleWeight(w.CacheLocality),
		PluginNodeCost:         scaleWeight(w.Cost),
	}
}

func scaleWeight(v float64) int64 {
	if v <= 0 {
		return 0
	}
	return int64(v * 100)
}

// PluginScore is one score plugin's contribution after normalization (for audit logs).
type PluginScore struct {
	Name       string
	Score      int64
	Normalized int64
	Weight     int64
	Weighted   int64
}

func (p PluginScore) String() string {
	return fmt.Sprintf("%s raw=%d norm=%d weight=%d weighted=%d", p.Name, p.Score, p.Normalized, p.Weight, p.Weighted)
}
