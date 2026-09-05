package kql

import (
	"fmt"
	"strings"
	"time"

	"github.com/denjamio/azlens/pkg/config"
)

// FormatTime formats a Go time.Time for KQL iso8601 string
func FormatTime(t time.Time) string {
	return t.UTC().Format(time.RFC3339Nano)
}

// mustBuilder creates a builder for an internal, whitelisted table literal.
func mustBuilder(table string) *QueryBuilder {
	b, err := NewBuilder(table)
	if err != nil {
		panic(fmt.Sprintf("kql: internal builder for disallowed table %q: %v", table, err))
	}
	return b
}

// BuildRequestsSummaryQuery builds KQL query for overall requests metrics
func BuildRequestsSummaryQuery(start, end time.Time, target config.TargetConfig) TargetQuery {
	b := mustBuilder("requests")
	return b.WithTimeRange(start, end).
		WithTarget(target).
		BuildRequestsSummary()
}

// BuildEndpointsSummaryQuery builds KQL query for per-endpoint request percentiles
func BuildEndpointsSummaryQuery(start, end time.Time, target config.TargetConfig, topN int) TargetQuery {
	b := mustBuilder("requests")
	return b.WithTimeRange(start, end).
		WithTarget(target).
		WithLimit(topN).
		BuildEndpointsSummary()
}

// BuildSlowDependenciesQuery builds KQL query for slow SQL queries and remote HTTP dependencies
func BuildSlowDependenciesQuery(start, end time.Time, target config.TargetConfig, depType string, topN int) TargetQuery {
	b := mustBuilder("dependencies")
	return b.WithTimeRange(start, end).
		WithTarget(target).
		WithLimit(topN).
		BuildDependenciesSummary(depType)
}

// BuildExceptionsSummaryQuery builds KQL query for grouped exceptions
func BuildExceptionsSummaryQuery(start, end time.Time, target config.TargetConfig, topN int) TargetQuery {
	b := mustBuilder("exceptions")
	return b.WithTimeRange(start, end).
		WithTarget(target).
		WithLimit(topN).
		BuildExceptionsSummary()
}

// BuildMySQLSlowLogsQuery builds KQL query for MySQL Flexible Server slow query logs
// ordered by execution duration descending (slowest queries first).
func BuildMySQLSlowLogsQuery(start, end time.Time, dbName string, topN int) TargetQuery {
	if strings.TrimSpace(dbName) == "" {
		return TargetQuery{Backend: BackendLogAnalytics, Err: ErrMissingDatabase}
	}
	dbFilter := fmt.Sprintf("\n| where Db =~ '%s'", sanitize(dbName))
	if topN <= 0 {
		topN = 15
	}

	query := fmt.Sprintf(`MySqlSlowLogs
| where TimeGenerated between (datetime('%s') .. datetime('%s'))%s
| extend Duration_s = round(toreal(QueryDurationMs) / 1000.0, 3)
| project TimeGenerated, Duration_s, QueryDurationMs, RowsExamined, RowsSent, SqlText
| top %d by QueryDurationMs desc`, FormatTime(start), FormatTime(end), dbFilter, topN)

	return TargetQuery{Query: query, Backend: BackendLogAnalytics}
}

// BuildMySQLSlowLogsGroupedQuery builds KQL query aggregating MySQL Flexible
// Server slow query logs by a normalized SQL fingerprint produced by the shared
// sqlFingerprintSteps pipeline (comments stripped, literals and numbers masked,
// IN-list lengths collapsed, whitespace and casing normalized), reporting
// execution count, duration statistics, and rows examined per query shape,
// ordered by total accumulated duration descending (highest overall impact
// first).
func BuildMySQLSlowLogsGroupedQuery(start, end time.Time, dbName string, topN int) TargetQuery {
	if strings.TrimSpace(dbName) == "" {
		return TargetQuery{Backend: BackendLogAnalytics, Err: ErrMissingDatabase}
	}
	dbFilter := fmt.Sprintf("\n| where Db =~ '%s'", sanitize(dbName))
	if topN <= 0 {
		topN = 15
	}

	lastStep := len(sqlFingerprintSteps)
	query := fmt.Sprintf(`MySqlSlowLogs
| where TimeGenerated between (datetime('%s') .. datetime('%s'))%s
| extend F0 = tostring(SqlText)
%s| extend SqlFingerprint = tolower(F%d)
| summarize
    Executions = count(),
    AvgMs = round(avg(QueryDurationMs), 1),
    MaxMs = max(QueryDurationMs),
    TotalMs = round(sum(QueryDurationMs), 1),
    AvgRowsExamined = round(avg(todouble(RowsExamined)), 0),
    LastSeen = max(TimeGenerated)
  by SqlFingerprint
| project SqlFingerprint, Executions, AvgMs, MaxMs, TotalMs, AvgRowsExamined, LastSeen
| top %d by TotalMs desc`, FormatTime(start), FormatTime(end), dbFilter, buildFingerprintExtends(), lastStep, topN)

	return TargetQuery{Query: query, Backend: BackendLogAnalytics}
}

// BuildFanoutSummaryQuery builds KQL query for SQL fan-out & N+1 detection
func BuildFanoutSummaryQuery(start, end time.Time, target config.TargetConfig, topN int) TargetQuery {
	b := mustBuilder("requests")
	return b.WithTimeRange(start, end).
		WithTarget(target).
		WithLimit(topN).
		BuildFanoutSummary()
}

// BuildLatencyBreakdownQuery builds KQL query for latency breakdown across DB, APIs, cache, and app code
func BuildLatencyBreakdownQuery(start, end time.Time, target config.TargetConfig, topN int) TargetQuery {
	b := mustBuilder("requests")
	return b.WithTimeRange(start, end).
		WithTarget(target).
		WithLimit(topN).
		BuildLatencyBreakdown()
}

// BuildDeprecationsQuery builds high-performance, noise-filtered KQL query to find deprecation warnings
func BuildDeprecationsQuery(start, end time.Time, target config.TargetConfig, topN int) TargetQuery {
	if strings.TrimSpace(target.RoleName) == "" {
		return TargetQuery{Backend: BackendAppInsights, Err: ErrMissingRole}
	}
	roleFilter := fmt.Sprintf("\n    | where %s", equalityExpr("cloud_RoleName", target.RoleName))
	syntheticFilter := "\n    | where isempty(column_ifexists('operation_SyntheticSource', ''))"
	probeFilter := "\n    | where not(operation_Name has_any ('/healthz', '/readyz', '/livez', '/startupz', '/health', '/healthcheck', '/ping', '/status', '/ready', '/live', '/up', 'rails/health', 'HealthController', '/actuator/health', '/actuator/info') or tostring(column_ifexists('customDimensions', dynamic(null))['User-Agent']) has_any ('kube-probe', 'GoogleHC', 'ELB-HealthChecker', 'ReadyForTraffic', 'Consul', 'Prometheus'))"

	if topN <= 0 {
		topN = 15
	}
	query := fmt.Sprintf(`union isfuzzy=true
(
    traces
    | where timestamp between (datetime('%s') .. datetime('%s'))%s%s%s
    | where message has_any ("deprecated", "deprecation", "deprecations", "obsolete", "RemovedInDjango")
    | where not(message startswith "SELECT" or message startswith "INSERT" or message startswith "UPDATE" or message startswith "DELETE" or message startswith "/*" or message startswith "SET ")
    | where (severityLevel >= 2) or (message has_any (
        'DEPRECATION WARNING', 'DeprecationWarning', 'PendingDeprecationWarning',
        'RemovedInDjango', '[DEP', '[DEPRECATION]', 'is deprecated', 'has been deprecated',
        'deprecated in', 'deprecated and will be removed', 'will be removed in',
        'is obsolete', 'CS0618', 'CS0612'
    ))
    | project timestamp, message, operation_Name
),
(
    exceptions
    | where timestamp between (datetime('%s') .. datetime('%s'))%s%s%s
    | where type has_any ("deprecated", "deprecation", "obsolete", "deprecat", "RemovedInDjango") or column_ifexists('outerMessage', '') has_any ("deprecated", "deprecation", "obsolete", "RemovedInDjango") or column_ifexists('message', '') has_any ("deprecated", "deprecation", "obsolete", "RemovedInDjango")
    | project timestamp, message = iff(isnotempty(column_ifexists('outerMessage', '')), column_ifexists('outerMessage', ''), column_ifexists('message', '')), operation_Name
)
| where isnotempty(message)
| extend NormalizedMsg = replace_regex(message, @":\d+", @":<line>")
| extend NormalizedMsg = replace_regex(NormalizedMsg, @"\(node:\d+\)", @"(node:<pid>)")
| extend NormalizedMsg = replace_regex(NormalizedMsg, @"0x[0-9a-fA-F]+", @"0x<hex>")
| summarize
    Count = count(),
    FirstSeen = min(timestamp),
    LastSeen = max(timestamp),
    AffectedEndpoints = make_set(operation_Name, 5)
  by CleanMessage = substring(NormalizedMsg, 0, 250)
| top %d by Count desc`, FormatTime(start), FormatTime(end), roleFilter, syntheticFilter, probeFilter,
		FormatTime(start), FormatTime(end), roleFilter, syntheticFilter, probeFilter, topN)

	return TargetQuery{Query: query, Backend: BackendAppInsights}
}
