package schedule

// normalizeHigherBetter maps values into [0,1] where larger raw input is better.
func normalizeHigherBetter(values []float64) []float64 {
	if len(values) == 0 {
		return nil
	}
	min, max := values[0], values[0]
	for _, v := range values[1:] {
		if v < min {
			min = v
		}
		if v > max {
			max = v
		}
	}
	out := make([]float64, len(values))
	if max == min {
		for i := range out {
			out[i] = 0.5
		}
		return out
	}
	for i, v := range values {
		out[i] = (v - min) / (max - min)
	}
	return out
}
