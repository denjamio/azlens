// Package kql implements the safe, parameterized KQL query builder for
// Azure Data Explorer (App Insights and Log Analytics) telemetry.
package kql

import (
	"fmt"
	"strings"
	"time"

	"github.com/denjamio/azlens/pkg/config"
)

// Backend identifies the Azure telemetry service target
type Backend string

const (
	BackendAppInsights  Backend = "app-insights"
	BackendLogAnalytics Backend = "log-analytics"
)

// TargetQuery represents a fully constructed KQL statement bound to its target backend
type TargetQuery struct {
	Query   string
	Backend Backend
}

func (t TargetQuery) String() string {
	return t.Query
}

// BackendForTable determines the telemetry backend for an Azure table
func BackendForTable(table string) Backend {
	switch strings.ToLower(table) {
	case "mysqlslowlogs":
		return BackendLogAnalytics
	default:
		return BackendAppInsights
	}
}

// AllowedTableNames whitelist to prevent table tampering
var AllowedTableNames = map[string]bool{
	"requests":      true,
	"dependencies":  true,
	"exceptions":    true,
	"traces":        true,
	"MySqlSlowLogs": true,
}

// QueryBuilder provides a safe, parameterized, high-performance KQL generator
type QueryBuilder struct {
	table     string
	backend   Backend
	startTime time.Time
	endTime   time.Time
	target    config.TargetConfig
	limit     int
}

// NewBuilder creates a new QueryBuilder for a specific table
func NewBuilder(table string) (*QueryBuilder, error) {
	if !AllowedTableNames[table] {
		return nil, fmt.Errorf("unsupported or disallowed KQL table: %s", table)
	}
	return &QueryBuilder{
		table:   table,
		backend: BackendForTable(table),
		limit:   15,
	}, nil
}

// WithTimeRange sets the time window safely
func (b *QueryBuilder) WithTimeRange(start, end time.Time) *QueryBuilder {
	b.startTime = start
	b.endTime = end
	return b
}

// WithTarget attaches profile target and filter definitions
func (b *QueryBuilder) WithTarget(target config.TargetConfig) *QueryBuilder {
	b.target = target
	return b
}

// WithLimit sets the maximum rows to return (bounded between 1 and 500)
func (b *QueryBuilder) WithLimit(limit int) *QueryBuilder {
	if limit <= 0 {
		limit = 15
	}
	if limit > 500 {
		limit = 500
	}
	b.limit = limit
	return b
}

// equalityExpr builds a case-insensitive equality KQL expression over a column:
// =~ for a single value, in~ for several (both leverage the same semantics)
func equalityExpr(column string, values config.StringList) string {
	if len(values) == 1 {
		return fmt.Sprintf("%s =~ '%s'", column, sanitize(values[0]))
	}
	quoted := make([]string, len(values))
	for i, v := range values {
		quoted[i] = fmt.Sprintf("'%s'", sanitize(v))
	}
	return fmt.Sprintf("%s in~ (%s)", column, strings.Join(quoted, ", "))
}

// tokenExpr builds a term-indexed KQL match over a column: has for a single
// value, has_any for several (inverted-term-index friendly)
func tokenExpr(column string, values config.StringList) string {
	if len(values) == 1 {
		return fmt.Sprintf("%s has '%s'", column, sanitize(values[0]))
	}
	quoted := make([]string, len(values))
	for i, v := range values {
		quoted[i] = fmt.Sprintf("'%s'", sanitize(v))
	}
	return fmt.Sprintf("%s has_any (%s)", column, strings.Join(quoted, ", "))
}

// buildBaseClauses produces optimized, partition-pruned KQL base filters
func (b *QueryBuilder) buildBaseClauses() string {
	var sb strings.Builder
	sb.WriteString(b.table)
	sb.WriteString("\n")

	// 1. Time filter MUST be first for partition pruning
	if !b.startTime.IsZero() && !b.endTime.IsZero() {
		timeCol := getTimeColumnForTable(b.table)
		sb.WriteString(fmt.Sprintf("| where %s between (datetime('%s') .. datetime('%s'))\n",
			timeCol, FormatTime(b.startTime), FormatTime(b.endTime)))
	}

	// 2. Azure Resource Scope
	if b.target.ResourceID != "" {
		sb.WriteString(fmt.Sprintf("| where _ResourceId =~ '%s'\n", sanitize(b.target.ResourceID)))
	}

	// 3. Microservice / Role Scope (App Insights indexed field)
	if len(b.target.Roles) > 0 && !strings.EqualFold(b.table, "MySqlSlowLogs") {
		sb.WriteString(fmt.Sprintf("| where %s\n", equalityExpr("cloud_RoleName", b.target.Roles)))
	}

	// 4. Pod Scope (cloud_RoleInstance in App Insights)
	if len(b.target.Pods) > 0 {
		sb.WriteString(fmt.Sprintf("| where %s\n", tokenExpr("cloud_RoleInstance", b.target.Pods)))
	}

	// 5. Exclude synthetic availability tests if configured
	if b.target.ExcludesSynthetic() && !strings.EqualFold(b.table, "MySqlSlowLogs") {
		sb.WriteString("| where isempty(operation_SyntheticSource) and isempty(syntheticSource)\n")
	}

	// 6. Exclude health probes (/healthz, /ready, kube-probe) if configured
	if b.target.ExcludesProbes() && !strings.EqualFold(b.table, "MySqlSlowLogs") {
		if b.table == "requests" {
			sb.WriteString("| where tostring(customDimensions['User-Agent']) !has 'kube-probe' and not(name has_any ('/healthz', '/readyz', '/livez', '/health', '/ping', '/actuator/health'))\n")
		} else {
			sb.WriteString("| where tostring(customDimensions['User-Agent']) !has 'kube-probe' and not(operation_Name has_any ('/healthz', '/readyz', '/livez', '/health', '/ping', '/actuator/health'))\n")
		}
	}

	// 7. Kubernetes Namespace scope
	if b.target.Logs.Namespace != "" {
		sb.WriteString(fmt.Sprintf("| where tostring(customDimensions['Kubernetes.Namespace']) =~ '%s' or tostring(customDimensions['namespace']) =~ '%s'\n",
			sanitize(b.target.Logs.Namespace), sanitize(b.target.Logs.Namespace)))
	}

	// 8. Custom Dimensions key-value scoping
	for k, v := range b.target.CustomDimensions {
		if k != "" && v != "" {
			sb.WriteString(fmt.Sprintf("| where tostring(customDimensions['%s']) =~ '%s'\n", sanitize(k), sanitize(v)))
		}
	}

	return sb.String()
}

// BuildRequestsSummary calculates throughput, failure rate, duration percentiles, and HTTP status breakdown
func (b *QueryBuilder) BuildRequestsSummary() TargetQuery {
	base := b.buildBaseClauses()
	q := base + `| summarize 
    TotalCalls = count(),
    FailedCalls = countif(success == false),
    AvgDuration = avg(duration),
    MinDuration = min(duration),
    MaxDuration = max(duration),
    P = percentiles(duration, 50, 75, 90, 95, 99),
    Count2xx = countif(toint(resultCode) >= 200 and toint(resultCode) < 300),
    Count4xx = countif(toint(resultCode) >= 400 and toint(resultCode) < 500),
    Count5xx = countif(toint(resultCode) >= 500 or (isempty(resultCode) and success == false))
| extend 
    P50 = todouble(P.percentile_duration_50),
    P75 = todouble(P.percentile_duration_75),
    P90 = todouble(P.percentile_duration_90),
    P95 = todouble(P.percentile_duration_95),
    P99 = todouble(P.percentile_duration_99),
    ErrorRate = round(100.0 * toreal(FailedCalls) / toreal(TotalCalls), 2)
| project TotalCalls, FailedCalls, AvgDuration, MinDuration, MaxDuration, P50, P75, P90, P95, P99, ErrorRate, Count2xx, Count4xx, Count5xx`
	return TargetQuery{Query: q, Backend: b.backend}
}

// BuildEndpointsSummary generates per-endpoint latency percentiles and error rates
func (b *QueryBuilder) BuildEndpointsSummary() TargetQuery {
	base := b.buildBaseClauses()
	q := base + fmt.Sprintf(`| summarize 
    TotalCalls = count(),
    FailedCalls = countif(success == false),
    AvgDuration = avg(duration),
    MinDuration = min(duration),
    MaxDuration = max(duration),
    P = percentiles(duration, 50, 75, 90, 95, 99)
  by name
| extend 
    P50 = todouble(P.percentile_duration_50),
    P75 = todouble(P.percentile_duration_75),
    P90 = todouble(P.percentile_duration_90),
    P95 = todouble(P.percentile_duration_95),
    P99 = todouble(P.percentile_duration_99),
    ErrorRate = round(100.0 * toreal(FailedCalls) / toreal(TotalCalls), 2)
| project name, TotalCalls, FailedCalls, AvgDuration, MinDuration, MaxDuration, P50, P75, P90, P95, P99, ErrorRate
| order by P95 desc
| take %d`, b.limit)
	return TargetQuery{Query: q, Backend: b.backend}
}

// BuildDependenciesSummary generates slow database / HTTP calls summary
func (b *QueryBuilder) BuildDependenciesSummary(depType string) TargetQuery {
	var sb strings.Builder
	sb.WriteString(b.buildBaseClauses())

	cleanType := strings.ToUpper(strings.TrimSpace(depType))
	switch cleanType {
	case "SQL":
		sb.WriteString("| where type in ('SQL', 'Azure SQL', 'SqlServer', 'PostgreSQL', 'postgres', 'mysql', 'MySQL', 'SQL Server')\n")
	case "HTTP":
		sb.WriteString("| where type in ('HTTP', 'Http (tracked component)', 'gRPC', 'Webservice')\n")
	case "REDIS":
		sb.WriteString("| where type in ('Redis', 'Azure Redis', 'Memcached')\n")
	case "COSMOS", "COSMOSDB":
		sb.WriteString("| where type in ('Azure DocumentDB', 'Cosmos', 'CosmosDB')\n")
	case "", "ALL":
		// all dependency types
	default:
		sb.WriteString(fmt.Sprintf("| where type =~ '%s'\n", sanitize(depType)))
	}

	sb.WriteString(fmt.Sprintf(`| summarize 
    TotalCalls = count(),
    FailedCalls = countif(success == false),
    AvgDuration = avg(duration),
    MinDuration = min(duration),
    MaxDuration = max(duration),
    P = percentiles(duration, 50, 90, 95, 99)
  by type, target, name
| extend 
    P50 = todouble(P.percentile_duration_50),
    P90 = todouble(P.percentile_duration_90),
    P95 = todouble(P.percentile_duration_95),
    P99 = todouble(P.percentile_duration_99),
    ErrorRate = round(100.0 * toreal(FailedCalls) / toreal(TotalCalls), 2)
| project type, target, name, TotalCalls, FailedCalls, AvgDuration, MinDuration, MaxDuration, P50, P90, P95, P99, ErrorRate
| order by P95 desc
| take %d`, b.limit))

	return TargetQuery{Query: sb.String(), Backend: b.backend}
}

// BuildExceptionsSummary groups exceptions by type, normalized message, and affected routes, filtering client/bot noise
func (b *QueryBuilder) BuildExceptionsSummary() TargetQuery {
	base := b.buildBaseClauses()
	q := base + fmt.Sprintf(`| where not(type in ('ActionController::RoutingError', 'NotFoundHttpException', 'Sinatra::NotFound', 'System.OperationCanceledException', 'System.Threading.Tasks.TaskCanceledException', 'Microsoft.AspNetCore.Connections.ConnectionResetException'))
| where not(coalesce(innermostMessage, outerMessage, message, "") has_any ('ClientClosedRequest', 'broken pipe', 'connection reset by peer', 'context canceled', 'request canceled'))
| extend RawMsg = coalesce(innermostMessage, outerMessage, message, "<empty>")
| extend CleanMessage = replace_regex(replace_regex(RawMsg, @"[0-9a-fA-F-]{36}", @"<UUID>"), @"\b\d{2,}\b", @"<ID>")
| summarize 
    Count = count(),
    FirstSeen = min(timestamp),
    LastSeen = max(timestamp),
    AffectedPaths = make_set(operation_Name, 10)
  by type, CleanMessage
| order by Count desc
| take %d`, b.limit)
	return TargetQuery{Query: q, Backend: b.backend}
}

// BuildFanoutSummary measures N+1 and SQL fan-out by crossing requests with dependencies
func (b *QueryBuilder) BuildFanoutSummary() TargetQuery {
	base := b.buildBaseClauses()
	q := base + fmt.Sprintf(`| where success == true
| join kind=inner (
    dependencies
    | where type in ('SQL', 'mysql', 'PostgreSQL', 'Azure SQL', 'SqlServer')
    | summarize SqlCalls = count(), SqlDuration = sum(duration) by operation_Id
) on operation_Id
| summarize 
    TotalRequests = count(),
    AvgSqlCalls = round(avg(SqlCalls), 1),
    MaxSqlCalls = max(SqlCalls),
    AvgSQLDuration = round(avg(SqlDuration), 1),
    AvgEndpointDuration = round(avg(duration), 1)
  by name
| where AvgSqlCalls > 1.0
| project name, TotalRequests, AvgSqlCalls, MaxSqlCalls, AvgSQLDuration, AvgEndpointDuration
| order by AvgSqlCalls desc
| take %d`, b.limit)
	return TargetQuery{Query: q, Backend: b.backend}
}

// BuildLatencyBreakdown breaks down request time across dependencies and app compute
func (b *QueryBuilder) BuildLatencyBreakdown() TargetQuery {
	base := b.buildBaseClauses()
	q := base + fmt.Sprintf(`| join kind=leftouter (
    dependencies
    | summarize 
        SqlTime = sumif(duration, type in ('SQL', 'mysql', 'MySQL', 'PostgreSQL', 'postgres', 'Azure SQL', 'SqlServer')),
        RedisTime = sumif(duration, type has 'redis'),
        HttpExtTime = sumif(duration, type == 'HTTP')
      by operation_Id
) on operation_Id
| extend AppComputeTime = duration - coalesce(SqlTime + RedisTime + HttpExtTime, 0)
| summarize 
    AvgTotalMs = round(avg(duration), 1),
    PctDatabase = round(100.0 * avg(SqlTime) / avg(duration), 1),
    PctExternalApi = round(100.0 * avg(HttpExtTime) / avg(duration), 1),
    PctCache = round(100.0 * avg(RedisTime) / avg(duration), 1),
    PctAppCode = round(100.0 * avg(AppComputeTime) / avg(duration), 1)
  by name
| project name, AvgTotalMs, PctDatabase, PctExternalApi, PctCache, PctAppCode
| order by AvgTotalMs desc
| take %d`, b.limit)
	return TargetQuery{Query: q, Backend: b.backend}
}

// Sanitize removes characters that could alter KQL expression semantics
func Sanitize(input string) string {
	return sanitize(input)
}

func sanitize(input string) string {
	s := strings.TrimSpace(input)
	s = strings.ReplaceAll(s, "\\", "\\\\")
	s = strings.ReplaceAll(s, "'", "\\'")
	s = strings.ReplaceAll(s, "\n", "")
	s = strings.ReplaceAll(s, "\r", "")
	s = strings.ReplaceAll(s, ";", "")
	return s
}

// getTimeColumnForTable returns TimeGenerated for Log Analytics tables, or timestamp for App Insights
func getTimeColumnForTable(table string) string {
	switch strings.ToLower(table) {
	case "mysqlslowlogs":
		return "TimeGenerated"
	default:
		return "timestamp"
	}
}
