package schedule

import (
	"testing"
	"time"
)

func TestWeightsFromConfig_allZeroUsesDefaults(t *testing.T) {
	w := WeightsFromConfig(0, 0, 0, 0)
	if w != DefaultWeights() {
		t.Fatalf("got %+v want %+v", w, DefaultWeights())
	}
}

func TestWeightsFromConfig_explicitZerosDisableDimensions(t *testing.T) {
	w := WeightsFromConfig(1, 0, 0, 0)
	if w.ResourceHeadroom != 1 || w.LoadBalance != 0 {
		t.Fatalf("got %+v", w)
	}
}

func TestWeightsFromConfig_yamlValues(t *testing.T) {
	w := WeightsFromConfig(0.35, 0.25, 0.25, 0.15)
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
