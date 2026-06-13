package service

import (
	"testing"

	"github.com/1CSerg/mcp-semantic-search-zvec-go/internal/config"
)

func TestSearchStatsPerformance(t *testing.T) {
	stats := NewSearchStats(config.SearchConfig{
		SlowThresholdSeconds: 1,
		DegradeRatio:         2,
		StatsWindow:          5,
		StatsMinSamples:      3,
	})
	for _, ms := range []float64{100, 110, 120} {
		stats.Record(ms)
	}
	perf := stats.Performance(100)
	if perf["total_ms"] != float64(100) {
		t.Fatalf("total_ms=%v", perf["total_ms"])
	}
	if perf["degraded"] != false {
		t.Fatalf("degraded=%v", perf["degraded"])
	}

	stats.Record(500)
	perf = stats.Performance(500)
	if perf["degraded"] != true {
		t.Fatalf("degraded=%v", perf)
	}
	if perf["reason"] != "median_ratio" {
		t.Fatalf("reason=%v", perf["reason"])
	}
}

func TestSearchStatsAbsoluteSlow(t *testing.T) {
	stats := NewSearchStats(config.SearchConfig{
		SlowThresholdSeconds: 0.05,
		DegradeRatio:         2,
		StatsWindow:          5,
		StatsMinSamples:      5,
	})
	perf := stats.Performance(200)
	if perf["slow"] != true {
		t.Fatalf("slow=%v", perf)
	}
	if perf["reason"] != "absolute_slow_threshold" {
		t.Fatalf("reason=%v", perf["reason"])
	}
}

func TestSearchStatsSnapshot(t *testing.T) {
	stats := NewSearchStats(config.SearchConfig{})
	stats.Record(42)
	snap := stats.Snapshot()
	if snap["samples"] != 1 {
		t.Fatalf("samples=%v", snap["samples"])
	}
	if snap["last_ms"] != float64(42) {
		t.Fatalf("last_ms=%v", snap["last_ms"])
	}
}
