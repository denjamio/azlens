// Package model defines the azlens domain models shared across every layer
// (configuration, query building, analysis, and reporting).
package model

import (
	"regexp"
	"strings"
	"time"
)

// TimeWindow defines a start and end time for metrics collection
type TimeWindow struct {
	Start time.Time `json:"start"`
	End   time.Time `json:"end"`
	Label string    `json:"label,omitempty"`
}

// LatencyPercentiles contains calculated percentiles in milliseconds
type LatencyPercentiles struct {
	Min float64 `json:"min"`
	Avg float64 `json:"avg"`
	P50 float64 `json:"p50"`
	P75 float64 `json:"p75"`
	P90 float64 `json:"p90"`
	P95 float64 `json:"p95"`
	P99 float64 `json:"p99"`
	Max float64 `json:"max"`
}

// RequestMetric represents aggregated performance metrics for a specific endpoint or overall
type RequestMetric struct {
	Name        string             `json:"name"`
	TotalCalls  int64              `json:"total_calls"`
	FailedCalls int64              `json:"failed_calls"`
	ErrorRate   float64            `json:"error_rate"` // 0.0 to 100.0%
	RPS         float64            `json:"rps"`
	HTTP2xx     int64              `json:"http_2xx,omitempty"`
	HTTP4xx     int64              `json:"http_4xx,omitempty"`
	HTTP5xx     int64              `json:"http_5xx,omitempty"`
	Latency     LatencyPercentiles `json:"latency"`
}

// DependencyMetric represents database or external API call metrics
type DependencyMetric struct {
	Target      string             `json:"target"`
	Type        string             `json:"type"` // SQL, PostgreSQL, CosmosDB, Redis, HTTP, gRPC
	Name        string             `json:"name"` // Command, SQL Query, or HTTP Route
	TotalCalls  int64              `json:"total_calls"`
	FailedCalls int64              `json:"failed_calls"`
	ErrorRate   float64            `json:"error_rate"`
	Latency     LatencyPercentiles `json:"latency"`
}

// ErrorSummary represents grouped exceptions or HTTP 5xx errors
type ErrorSummary struct {
	Type          string    `json:"type"`           // e.g. "System.NullReferenceException", "500 Internal Server Error"
	Message       string    `json:"message"`        // Error message / innermostMessage
	Count         int64     `json:"count"`          // Number of occurrences
	FirstSeen     time.Time `json:"first_seen"`     // Timestamp first seen
	LastSeen      time.Time `json:"last_seen"`      // Timestamp last seen
	AffectedPaths []string  `json:"affected_paths"` // Endpoints throwing this error
}

// instrumentationNoiseRe matches exceptions raised by the auto-instrumentation
// SDK itself (e.g. OpenTelemetry failing to hook a framework module), capturing
// the framework or module it was hooking
var instrumentationNoiseRe = regexp.MustCompile(`(?i)exception occurred when instrumenting\s*:?\s*(\S*)`)

// InstrumentationTarget reports whether the error was emitted by the
// auto-instrumentation layer itself rather than application code, and if so,
// which framework or module it was hooking ("fastapi", "flask", ...).
// These are deployment/packaging signals (a missing module in the runtime
// image), not API failures.
func (e ErrorSummary) InstrumentationTarget() (string, bool) {
	m := instrumentationNoiseRe.FindStringSubmatch(e.Message)
	if m == nil {
		return "", false
	}
	target := strings.Trim(m[1], ".,'\"")
	return target, true
}

// RegressionSeverity denotes the severity of a detected regression
type RegressionSeverity string

const (
	SeverityNone     RegressionSeverity = "OK"
	SeverityImprove  RegressionSeverity = "IMPROVED"
	SeverityWarning  RegressionSeverity = "WARNING"
	SeverityCritical RegressionSeverity = "CRITICAL"
)

// MetricDelta captures the difference and percentage change between baseline and current
type MetricDelta struct {
	MetricName  string             `json:"metric_name"`
	Baseline    float64            `json:"baseline"`
	Current     float64            `json:"current"`
	Delta       float64            `json:"delta"`
	Percentage  float64            `json:"percentage"`
	Unit        string             `json:"unit"`
	Severity    RegressionSeverity `json:"severity"`
	Explanation string             `json:"explanation"`
}

// WindowMetrics bundles all telemetry queried for a single time window
// (fetched in one batched KQL request: overall, endpoints, dependencies, errors, fan-out)
type WindowMetrics struct {
	Overall   RequestMetric
	Endpoints []RequestMetric
	Deps      []DependencyMetric
	Errors    []ErrorSummary
	Fanout    []FanoutMetric
}

// DiffReport holds full pre-vs-post deploy regression analysis
type DiffReport struct {
	AppName         string             `json:"app_name"`
	BaselineWindow  TimeWindow         `json:"baseline_window"`
	CurrentWindow   TimeWindow         `json:"current_window"`
	OverallVerdict  RegressionSeverity `json:"overall_verdict"`
	SummaryDeltas   []MetricDelta      `json:"summary_deltas"`
	EndpointDeltas  []EndpointDiff     `json:"endpoint_deltas"`
	NewErrors       []ErrorSummary     `json:"new_errors"`
	RegressedDeps   []DependencyDiff   `json:"regressed_deps"`
	NewDependencies []DependencyMetric `json:"new_dependencies,omitempty"`
	FanoutDeltas    []FanoutDiff       `json:"fanout_deltas,omitempty"`
	RootCauseHints  []string           `json:"root_cause_hints,omitempty"`
}

// FanoutDiff compares SQL fan-out / N+1 metrics before and after deploy
type FanoutDiff struct {
	Endpoint      string             `json:"endpoint"`
	BaselineCalls float64            `json:"baseline_calls"`
	CurrentCalls  float64            `json:"current_calls"`
	DeltaPct      float64            `json:"delta_pct"`
	Severity      RegressionSeverity `json:"severity"`
}

// EndpointDiff compares a single endpoint before and after
type EndpointDiff struct {
	Name        string             `json:"name"`
	Baseline    RequestMetric      `json:"baseline"`
	Current     RequestMetric      `json:"current"`
	P95DeltaPct float64            `json:"p95_delta_pct"`
	ErrDeltaPct float64            `json:"err_delta_pct"`
	Severity    RegressionSeverity `json:"severity"`
}

// DependencyDiff compares a dependency before and after
type DependencyDiff struct {
	Name        string             `json:"name"`
	Type        string             `json:"type"`
	Target      string             `json:"target"`
	Baseline    DependencyMetric   `json:"baseline"`
	Current     DependencyMetric   `json:"current"`
	P95DeltaPct float64            `json:"p95_delta_pct"`
	Severity    RegressionSeverity `json:"severity"`
}

// GenericQueryResult holds columns and rows for arbitrary KQL queries
type GenericQueryResult struct {
	Columns []string        `json:"columns"`
	Rows    [][]interface{} `json:"rows"`
}

// SlowLogEntry represents a parsed MySQL engine slow query log entry
type SlowLogEntry struct {
	Timestamp    time.Time `json:"timestamp"`
	DurationSec  float64   `json:"duration_s"`
	DurationMs   float64   `json:"duration_ms"`
	RowsExamined int64     `json:"rows_examined"`
	RowsSent     int64     `json:"rows_sent"`
	SQLText      string    `json:"sql_text"`
}

// SlowLogGroup aggregates MySQL slow query log entries that share the same
// normalized SQL fingerprint (string and numeric literals masked): how many
// times the query shape executed plus its duration and rows profile
type SlowLogGroup struct {
	Fingerprint     string    `json:"fingerprint"`
	Executions      int64     `json:"executions"`
	AvgMs           float64   `json:"avg_ms"`
	MaxMs           float64   `json:"max_ms"`
	TotalMs         float64   `json:"total_ms"`
	AvgRowsExamined float64   `json:"avg_rows_examined"`
	LastSeen        time.Time `json:"last_seen"`
}

// FanoutMetric measures N+1 and database fan-out per endpoint
type FanoutMetric struct {
	Endpoint              string  `json:"endpoint"`
	TotalRequests         int64   `json:"total_requests"`
	AvgSQLCalls           float64 `json:"avg_sql_calls"`
	MaxSQLCalls           int64   `json:"max_sql_calls"`
	AvgSQLDurationMs      float64 `json:"avg_sql_duration_ms"`
	AvgEndpointDurationMs float64 `json:"avg_endpoint_duration_ms"`
}

// LatencyBreakdown breaks down where time was spent per endpoint
type LatencyBreakdown struct {
	Endpoint       string  `json:"endpoint"`
	AvgDurationMs  float64 `json:"avg_duration_ms"`
	PctDatabase    float64 `json:"pct_database"`
	PctExternalAPI float64 `json:"pct_external_api"`
	PctCache       float64 `json:"pct_cache"`
	PctAppCode     float64 `json:"pct_app_code"`
}

// DeprecationSummary represents grouped framework and library deprecation warnings
type DeprecationSummary struct {
	Message           string    `json:"message"`
	Count             int64     `json:"count"`
	FirstSeen         time.Time `json:"first_seen"`
	LastSeen          time.Time `json:"last_seen"`
	AffectedEndpoints []string  `json:"affected_endpoints"`
}
