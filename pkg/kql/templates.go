package kql

import (
	"fmt"
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

// BuildMySQLSlowLogsQuery builds KQL query for MySQL engine slow query logs from Log Analytics
func BuildMySQLSlowLogsQuery(start, end time.Time, dbName string, slowest bool, topN int) TargetQuery {
	dbFilter := ""
	if dbName != "" {
		dbFilter = fmt.Sprintf("\n| where Db =~ '%s' or DatabaseName_s =~ '%s'", sanitize(dbName), sanitize(dbName))
	}
	if topN <= 0 {
		topN = 15
	}

	var query string
	if slowest {
		query = fmt.Sprintf(`MySqlSlowLogs
| where TimeGenerated between (datetime('%s') .. datetime('%s'))%s
| extend QueryDurationMs = toint(coalesce(QueryTime_s, 0.0) * 1000.0)
| extend SqlText = substring(coalesce(Query_s, SqlText_s, SqlText, ""), 0, 300)
| project TimeGenerated, QueryDurationMs, SqlText
| order by QueryDurationMs desc
| take %d`, FormatTime(start), FormatTime(end), dbFilter, topN)
	} else {
		query = fmt.Sprintf(`MySqlSlowLogs
| where TimeGenerated between (datetime('%s') .. datetime('%s'))%s
| extend QueryDurationMs = toint(coalesce(QueryTime_s, 0.0) * 1000.0)
| extend SqlText = substring(coalesce(Query_s, SqlText_s, SqlText, ""), 0, 300)
| summarize
    Executions = count(),
    TotalDurationMs = sum(QueryDurationMs),
    AvgDurationMs = round(avg(QueryDurationMs), 1),
    MaxDurationMs = max(QueryDurationMs)
by SqlText
| order by TotalDurationMs desc
| take %d`, FormatTime(start), FormatTime(end), dbFilter, topN)
	}

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
	roleFilter := ""
	if len(target.Roles) > 0 {
		roleFilter = fmt.Sprintf("\n    | where %s", equalityExpr("cloud_RoleName", target.Roles))
	}
	syntheticFilter := ""
	if target.ExcludesSynthetic() {
		syntheticFilter = "\n    | where isempty(operation_SyntheticSource) and isempty(syntheticSource)"
	}
	probeFilter := ""
	if target.ExcludesProbes() {
		probeFilter = "\n    | where tostring(customDimensions['User-Agent']) !has 'kube-probe' and not(operation_Name has_any ('/healthz', '/readyz', '/livez', '/health', '/ping', '/actuator/health'))"
	}
	if topN <= 0 {
		topN = 15
	}
	query := fmt.Sprintf(`union isfuzzy=true
(
    traces
    | where timestamp between (datetime('%s') .. datetime('%s'))%s%s%s
    | where message has "deprecated" or message has "deprecation"
    | where not(message startswith "SELECT" or message startswith "INSERT" or message startswith "UPDATE" or message startswith "DELETE" or message startswith "/*")
    | where (severityLevel >= 2) or (message has_any ('DEPRECATION WARNING', 'DeprecationWarning', '[DEP', 'is deprecated', 'has been deprecated', 'deprecated in', 'deprecated and will be removed'))
    | project timestamp, message, operation_Name
),
(
    exceptions
    | where timestamp between (datetime('%s') .. datetime('%s'))%s%s%s
    | where type has "deprecat" or message has "deprecat" or innermodel_message has "deprecat"
    | project timestamp, message = coalesce(innermodel_message, message), operation_Name
)
| where isnotempty(message)
| extend NormalizedMsg = replace_regex(message, @":\d+", @":<line>")
| summarize
    Count = count(),
    FirstSeen = min(timestamp),
    LastSeen = max(timestamp),
    AffectedEndpoints = make_set(operation_Name, 5)
  by CleanMessage = substring(NormalizedMsg, 0, 250)
| order by Count desc
| take %d`, FormatTime(start), FormatTime(end), roleFilter, syntheticFilter, probeFilter,
		FormatTime(start), FormatTime(end), roleFilter, syntheticFilter, probeFilter, topN)

	return TargetQuery{Query: query, Backend: BackendAppInsights}
}
