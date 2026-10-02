package schedule

import "time"

// WeightsFromConfig maps YAML weights into scheduling weights.
// If all four values are zero, DefaultWeights is used (config omitted).
// Individual zero weights disable that dimension (scaleWeight skips it).
func WeightsFromConfig(resourceHeadroom, loadBalance, cacheLocality, cost float64) Weights {
	if resourceHeadroom == 0 && loadBalance == 0 && cacheLocality == 0 && cost == 0 {
		return DefaultWeights()
	}
	return Weights{
		ResourceHeadroom: resourceHeadroom,
		LoadBalance:      loadBalance,
		CacheLocality:    cacheLocality,
		Cost:             cost,
	}
}

// ParseFreshWindow parses a duration string; empty or invalid values use fallback.
func ParseFreshWindow(raw string, fallback time.Duration) time.Duration {
	if raw == "" {
		return fallback
	}
	d, err := time.ParseDuration(raw)
	if err != nil || d <= 0 {
		return fallback
	}
	return d
}
