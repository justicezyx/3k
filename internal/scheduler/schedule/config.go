package schedule

import "time"

// WeightsFromConfig builds scoring weights from YAML-style config values.
// Zero values fall back to defaults for that field.
func WeightsFromConfig(resourceHeadroom, loadBalance, cacheLocality, cost float64) Weights {
	w := DefaultWeights()
	if resourceHeadroom > 0 {
		w.ResourceHeadroom = resourceHeadroom
	}
	if loadBalance > 0 {
		w.LoadBalance = loadBalance
	}
	if cacheLocality > 0 {
		w.CacheLocality = cacheLocality
	}
	if cost > 0 {
		w.Cost = cost
	}
	return w
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
