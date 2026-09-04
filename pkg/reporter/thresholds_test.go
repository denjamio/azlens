package reporter

import (
	"testing"

	"github.com/fatih/color"
)

// The band helpers must return the exact package color objects so a colored
// cell always maps back to the documented threshold table.
func TestSeverityBandsFollowIndustryThresholds(t *testing.T) {
	cases := []struct {
		name string
		got  *color.Color
		want *color.Color
	}{
		// Error rate: <1% healthy, 1-5% review, >=5% critical (SRE error budget)
		{"error rate healthy", errorRateColor(0.5), nil},
		{"error rate warn", errorRateColor(1.0), colorYellow},
		{"error rate crit", errorRateColor(5.0), colorRed},
		// API latency: <300ms healthy, >=1s critical (1-second rule)
		{"api latency healthy", apiLatencyColor(250), nil},
		{"api latency warn", apiLatencyColor(300), colorYellow},
		{"api latency crit", apiLatencyColor(1000), colorRed},
		// DB latency: <100ms healthy, >=1s critical (long_query_time)
		{"db latency healthy", dbLatencyColor(80), nil},
		{"db latency warn", dbLatencyColor(100), colorYellow},
		{"db latency crit", dbLatencyColor(1000), colorRed},
		// Cache latency: <5ms healthy, >=25ms critical (sub-ms round-trip budget)
		{"cache latency healthy", cacheLatencyColor(2), nil},
		{"cache latency warn", cacheLatencyColor(5), colorYellow},
		{"cache latency crit", cacheLatencyColor(25), colorRed},
		// N+1: >=5 calls review, >=15 critical (analyzer defaults)
		{"n+1 healthy", nPlusOneColor(4), nil},
		{"n+1 warn", nPlusOneColor(5), colorYellow},
		{"n+1 crit", nPlusOneColor(15), colorRed},
		// Scan ratio: >=100x review, >=1000x likely missing index (Percona)
		{"scan ratio healthy", scanRatioColor(50, 100), nil},
		{"scan ratio warn", scanRatioColor(100, 1), colorYellow},
		{"scan ratio crit", scanRatioColor(100000, 10), colorRed},
		{"scan ratio zero returned", scanRatioColor(5000, 0), nil},
		{"scan ratio zero examined", scanRatioColor(0, 100), nil},
		// Regression deltas: +15% warn, +30% crit, <=-15% improved (analyzer)
		{"delta healthy", deltaPctColor(5), nil},
		{"delta warn", deltaPctColor(15), colorYellow},
		{"delta crit", deltaPctColor(30), colorRed},
		{"delta improved", deltaPctColor(-15), colorGreen},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if tc.got != tc.want {
				t.Errorf("got %v, want %v", tc.got, tc.want)
			}
		})
	}
}

func TestLatencyColorForDepTypeUsesClassBands(t *testing.T) {
	// 400ms is yellow for APIs but critical-band for cache round-trips and
	// still-review for DB statements: the class changes the verdict
	if got := latencyColorForDepType("HTTP", 400); got != colorYellow {
		t.Errorf("HTTP 400ms should be yellow, got %v", got)
	}
	if got := latencyColorForDepType("Redis", 400); got != colorRed {
		t.Errorf("Redis 400ms should be red, got %v", got)
	}
	if got := latencyColorForDepType("SQL", 400); got != colorYellow {
		t.Errorf("SQL 400ms should be yellow, got %v", got)
	}
	if got := latencyColorForDepType("postgresql", 80); got != nil {
		t.Errorf("PostgreSQL 80ms should be uncolored, got %v", got)
	}
	if got := latencyColorForDepType("", 1500); got != colorRed {
		t.Errorf("unknown type falls back to API band: got %v", got)
	}
}
