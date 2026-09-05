package reporter

import (
	"strings"

	"github.com/fatih/color"
)

// Production severity bands: single source of truth for every threshold-driven
// color in the terminal output. Each value is anchored on an industry standard
// (SRE error budgets, MySQL long_query_time, Percona query-analysis heuristics,
// common API latency tiers, azlens analyzer defaults) so a color means the
// same thing in every table:
//
//	green  = healthy
//	yellow = review / degraded
//	red    = critical

// Error rate (HTTP 5xx / exceptions), in % of requests.
// Basis: SRE error-budget practice for the common 99% SLO tier (1% budget)
// with the widely used 5% degraded ceiling found in APM alert defaults.
const (
	ErrorRateWarn = 1.0
	ErrorRateCrit = 5.0
)

// Latency bands in milliseconds, per telemetry class.
// API/gRPC endpoints: <300ms fast, >=1s crosses the universal "1-second rule".
// DB statements: <100ms healthy, >=1s is MySQL long_query_time territory.
// Cache round-trips (Redis): sub-millisecond ideal; >5ms review.
const (
	APILatencyWarnMs   = 300.0
	APILatencyCritMs   = 1000.0
	DBLatencyWarnMs    = 100.0
	DBLatencyCritMs    = 1000.0
	CacheLatencyWarnMs = 5.0
	CacheLatencyCritMs = 25.0
)

// Scan ratio: rows examined per row returned by a SQL statement.
// Basis: standard EXPLAIN / Percona query-analysis heuristic — scanning >=100
// rows per returned row deserves review; >=1000 signals a likely missing index.
const (
	ScanRatioWarn = 100.0
	ScanRatioCrit = 1000.0
)

// SQL calls per request for N+1 detection.
// Basis: azlens analysis defaults (fan-out flagged at >=5 average calls,
// critical at >=15 or a +100% spike).
const (
	NPlusOneWarn = 5.0
	NPlusOneCrit = 15.0
)

// Regression deltas, in percent change.
// Basis: azlens analysis regression thresholds (latency warning at +15%,
// critical at +30%, meaningful improvement at <= -15%), which mirror common
// APM regression monitors; the N+1 spike band uses the same defaults
// (warning at +40%, critical at +100%).
const (
	RegressionWarnPct    = 15.0
	RegressionCritPct    = 30.0
	RegressionImprovePct = -15.0
	FanoutSpikeWarnPct   = 40.0
	FanoutSpikeCritPct   = 100.0
)

// errorRateColor applies the SRE error-budget band
func errorRateColor(pct float64) *color.Color {
	return bandColor(pct, ErrorRateWarn, ErrorRateCrit)
}

// apiLatencyColor applies the HTTP/gRPC endpoint latency band
func apiLatencyColor(ms float64) *color.Color {
	return bandColor(ms, APILatencyWarnMs, APILatencyCritMs)
}

// dbLatencyColor applies the SQL statement latency band
func dbLatencyColor(ms float64) *color.Color {
	return bandColor(ms, DBLatencyWarnMs, DBLatencyCritMs)
}

// cacheLatencyColor applies the cache round-trip latency band
func cacheLatencyColor(ms float64) *color.Color {
	return bandColor(ms, CacheLatencyWarnMs, CacheLatencyCritMs)
}

// nPlusOneColor applies the SQL-calls-per-request band
func nPlusOneColor(calls float64) *color.Color {
	return bandColor(calls, NPlusOneWarn, NPlusOneCrit)
}

// latencyColorForDepType picks the latency band matching the dependency class:
// database statements, cache round-trips, and everything else (HTTP, gRPC, ...)
// get their own reference values instead of one arbitrary compromise
func latencyColorForDepType(depType string, ms float64) *color.Color {
	switch strings.ToLower(strings.TrimSpace(depType)) {
	case "redis", "azure redis":
		return cacheLatencyColor(ms)
	case "sql", "azure sql", "postgresql", "postgres", "mysql":
		return dbLatencyColor(ms)
	default:
		return apiLatencyColor(ms)
	}
}

// scanRatioColor colors a rows-examined cell using the examined/returned
// ratio (the missing-index signal), not the absolute row count
func scanRatioColor(examined, returned int64) *color.Color {
	if examined <= 0 || returned <= 0 {
		return nil
	}
	return bandColor(float64(examined)/float64(returned), ScanRatioWarn, ScanRatioCrit)
}
