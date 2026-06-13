package service

import (
	"sort"
	"sync"

	"github.com/1CSerg/mcp-semantic-search-zvec-go/internal/config"
)

// SearchStats tracks rolling search latency metrics.
type SearchStats struct {
	mu            sync.Mutex
	samples       []float64
	window        int
	minSamples    int
	slowThreshold float64
	degradeRatio  float64
}

// NewSearchStats creates a metrics tracker from search config.
func NewSearchStats(cfg config.SearchConfig) *SearchStats {
	window := cfg.StatsWindow
	if window <= 0 {
		window = config.DefaultStatsWindow
	}
	minSamples := cfg.StatsMinSamples
	if minSamples <= 0 {
		minSamples = 5
	}
	slow := cfg.SlowThresholdSeconds
	if slow <= 0 {
		slow = config.DefaultSlowSearchSeconds
	}
	ratio := cfg.DegradeRatio
	if ratio <= 0 {
		ratio = config.DefaultDegradeRatio
	}
	return &SearchStats{
		window:        window,
		minSamples:    minSamples,
		slowThreshold: slow,
		degradeRatio:  ratio,
	}
}

// Record adds a search latency sample in milliseconds.
func (s *SearchStats) Record(ms float64) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.samples = append(s.samples, ms)
	if len(s.samples) > s.window {
		s.samples = s.samples[len(s.samples)-s.window:]
	}
}

// Performance evaluates latency against thresholds.
func (s *SearchStats) Performance(ms float64) map[string]any {
	degraded, slow, reason := s.evaluate(ms)
	out := map[string]any{
		"total_ms": ms,
		"degraded": degraded,
		"slow":     slow,
	}
	if reason != "" {
		out["reason"] = reason
	}
	return out
}

func (s *SearchStats) evaluate(ms float64) (degraded, slow bool, reason string) {
	slow = ms >= s.slowThreshold*1000
	s.mu.Lock()
	defer s.mu.Unlock()
	if len(s.samples) < s.minSamples {
		if slow {
			reason = "absolute_slow_threshold"
			return false, slow, reason
		}
		return false, slow, ""
	}
	median := medianCopy(s.samples)
	if median > 0 && ms > median*s.degradeRatio {
		degraded = true
		reason = "median_ratio"
	}
	if slow && reason == "" {
		reason = "absolute_slow_threshold"
	}
	return degraded, slow, reason
}

// Snapshot returns metrics for index_status.
func (s *SearchStats) Snapshot() map[string]any {
	s.mu.Lock()
	defer s.mu.Unlock()
	out := map[string]any{
		"samples":                len(s.samples),
		"slow_threshold_seconds": s.slowThreshold,
		"degrade_ratio":          s.degradeRatio,
		"stats_window":           s.window,
		"stats_min_samples":      s.minSamples,
	}
	if len(s.samples) > 0 {
		out["median_ms"] = medianCopy(s.samples)
		out["last_ms"] = s.samples[len(s.samples)-1]
	}
	return out
}

func medianCopy(values []float64) float64 {
	if len(values) == 0 {
		return 0
	}
	cp := append([]float64(nil), values...)
	sort.Float64s(cp)
	mid := len(cp) / 2
	if len(cp)%2 == 0 {
		return (cp[mid-1] + cp[mid]) / 2
	}
	return cp[mid]
}
