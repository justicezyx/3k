package schedule

import (
	"testing"
	"time"
)

func TestWeightsFromConfig_partialOverride(t *testing.T) {
	w := WeightsFromConfig(0.5, 0, 0, 0)
	if w.ResourceHeadroom != 0.5 {
		t.Fatalf("headroom=%f", w.ResourceHeadroom)
	}
	if w.LoadBalance != DefaultWeights().LoadBalance {
		t.Fatal("expected default load balance when zero in config")
	}
}

func TestWeightsFromConfig_allZeroUsesDefaults(t *testing.T) {
	w := WeightsFromConfig(0, 0, 0, 0)
	d := DefaultWeights()
	if w != d {
		t.Fatalf("got %+v want %+v", w, d)
	}
}

func TestParseFreshWindow(t *testing.T) {
	fallback := 10 * time.Minute
	if ParseFreshWindow("", fallback) != fallback {
		t.Fatal("empty")
	}
	if ParseFreshWindow("bad", fallback) != fallback {
		t.Fatal("bad")
	}
	if ParseFreshWindow("45m", fallback) != 45*time.Minute {
		t.Fatal("45m")
	}
	if ParseFreshWindow("-1m", fallback) != fallback {
		t.Fatal("negative")
	}
}
