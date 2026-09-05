// Package kql implements the safe, parameterized KQL query builder for
// Azure Data Explorer (App Insights and Log Analytics) telemetry.
package kql

import (
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/denjamio/azlens/pkg/config"
)

// ErrMissingRole indicates a tenancy violation where an application query was attempted without role/service scoping
var ErrMissingRole = errors.New("tenancy firewall: query generation blocked because role/service filter is empty; set an active service or role to isolate telemetry")

// ErrMissingDatabase indicates a tenancy violation where a database query was attempted without database scoping
var ErrMissingDatabase = errors.New("tenancy firewall: query generation blocked because database filter is empty; set logs.database to isolate database telemetry")

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
	Err     error
}

func (t TargetQuery) String() string {
	if t.Err != nil {
		return fmt.Sprintf("Error: %v", t.Err)
	}
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

// equalityExpr builds a case-insensitive equality KQL expression over a column (=~)
func equalityExpr(column, value string) string {
	return fmt.Sprintf("%s =~ '%s'", column, sanitize(value))
}

// tokenExpr builds a term-indexed KQL match over a column (has)
func tokenExpr(column, value string) string {
	return fmt.Sprintf("%s has '%s'", column, sanitize(value))
}

// checkTenancyFirewall enforces strict multi-tenancy:
//   - Application queries against App Insights tables (requests, dependencies, exceptions, traces)
//     MUST have an active Role filter to prevent unbounded scans across shared resource tenants.
//   - Database queries against Log Analytics (MySqlSlowLogs) MUST have an active Database filter.
func (b *QueryBuilder) checkTenancyFirewall() error {
	if strings.EqualFold(b.table, "MySqlSlowLogs") {
		if strings.TrimSpace(b.target.Logs.Database) == "" {
			return ErrMissingDatabase
		}
		return nil
	}
	if strings.TrimSpace(b.target.RoleName) == "" {
		return ErrMissingRole
	}
	return nil
}

// buildBaseClauses produces the table reference followed by the optimized,
// partition-pruned KQL base filters
func (b *QueryBuilder) buildBaseClauses() string {
	return b.table + "\n" + b.buildBaseFilters()
}

// buildBaseFilters produces the optimized, partition-pruned filter chain for
// the builder's table (time, resource, role, pod, synthetic, probe, custom
// dimensions) without the leading table reference, so table-union queries can
// reuse the same scoping per branch
func (b *QueryBuilder) buildBaseFilters() string {
	var sb strings.Builder

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
	if strings.TrimSpace(b.target.RoleName) != "" && !strings.EqualFold(b.table, "MySqlSlowLogs") {
		sb.WriteString(fmt.Sprintf("| where %s\n", equalityExpr("cloud_RoleName", b.target.RoleName)))
	}

	// 4. Pod Scope (cloud_RoleInstance in App Insights)
	if strings.TrimSpace(b.target.Pod) != "" && !strings.EqualFold(b.table, "MySqlSlowLogs") {
		sb.WriteString(fmt.Sprintf("| where %s\n", tokenExpr("cloud_RoleInstance", b.target.Pod)))
	}

	// 5. Database Scope (MySqlSlowLogs in Log Analytics)
	if strings.EqualFold(b.table, "MySqlSlowLogs") && strings.TrimSpace(b.target.Logs.Database) != "" {
		sb.WriteString(fmt.Sprintf("| where %s\n", equalityExpr("Db", b.target.Logs.Database)))
	}

	// 5. Exclude synthetic availability test traffic unconditionally (convention over configuration)
	if strings.EqualFold(b.table, "requests") || strings.EqualFold(b.table, "dependencies") {
		sb.WriteString("| where isempty(column_ifexists('operation_SyntheticSource', ''))\n")
	}

	// 6. Exclude health check probes unconditionally across all standard stacks and orchestrators
	if strings.EqualFold(b.table, "requests") {
		sb.WriteString("| where not(name has_any ('/healthz', '/readyz', '/livez', '/startupz', '/health', '/healthcheck', '/ping', '/status', '/ready', '/live', '/up', '/actuator/health', '/actuator/info') or tostring(column_ifexists('customDimensions', dynamic(null))['User-Agent']) has_any ('kube-probe', 'GoogleHC', 'ELB-HealthChecker', 'ReadyForTraffic', 'Consul', 'Prometheus'))\n")
	} else if !strings.EqualFold(b.table, "MySqlSlowLogs") {
		sb.WriteString("| where not(operation_Name has_any ('/healthz', '/readyz', '/livez', '/startupz', '/health', '/healthcheck', '/ping', '/status', '/ready', '/live', '/up', '/actuator/health', '/actuator/info') or tostring(column_ifexists('customDimensions', dynamic(null))['User-Agent']) has_any ('kube-probe', 'GoogleHC', 'ELB-HealthChecker', 'ReadyForTraffic', 'Consul', 'Prometheus'))\n")
	}

	// 7. Custom Dimensions key-value scoping
	for k, v := range b.target.CustomDimensions {
		if k != "" && v != "" {
			sb.WriteString(fmt.Sprintf("| where tostring(customDimensions['%s']) =~ '%s'\n", sanitize(k), sanitize(v)))
		}
	}

	return sb.String()
}

// BuildRequestsSummary calculates throughput, failure rate, duration percentiles, and HTTP status breakdown.
// Percentiles are computed in a single percentiles() pass (one histogram scan
// instead of one pass per percentile); the values are identical to individual
// percentile() calls.
func (b *QueryBuilder) BuildRequestsSummary() TargetQuery {
	if err := b.checkTenancyFirewall(); err != nil {
		return TargetQuery{Backend: b.backend, Err: err}
	}
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
| extend P50 = todouble(P[0]), P75 = todouble(P[1]), P90 = todouble(P[2]), P95 = todouble(P[3]), P99 = todouble(P[4]), ErrorRate = round(100.0 * toreal(FailedCalls) / toreal(TotalCalls), 2)
| project TotalCalls, FailedCalls, AvgDuration, MinDuration, MaxDuration, P50, P75, P90, P95, P99, ErrorRate, Count2xx, Count4xx, Count5xx`
	return TargetQuery{Query: q, Backend: b.backend}
}

// BuildEndpointsSummary generates per-endpoint latency percentiles and error rates,
// returning the top N by P95 (top-N heap instead of a full sort)
func (b *QueryBuilder) BuildEndpointsSummary() TargetQuery {
	if err := b.checkTenancyFirewall(); err != nil {
		return TargetQuery{Backend: b.backend, Err: err}
	}
	base := b.buildBaseClauses()
	q := base + fmt.Sprintf(`| summarize 
    TotalCalls = count(),
    FailedCalls = countif(success == false),
    AvgDuration = avg(duration),
    MinDuration = min(duration),
    MaxDuration = max(duration),
    P = percentiles(duration, 50, 75, 90, 95, 99)
  by name
| extend P50 = todouble(P[0]), P75 = todouble(P[1]), P90 = todouble(P[2]), P95 = todouble(P[3]), P99 = todouble(P[4]), ErrorRate = round(100.0 * toreal(FailedCalls) / toreal(TotalCalls), 2)
| project name, TotalCalls, FailedCalls, AvgDuration, MinDuration, MaxDuration, P50, P75, P90, P95, P99, ErrorRate
| top %d by P95 desc`, b.limit)
	return TargetQuery{Query: q, Backend: b.backend}
}

// BuildDependenciesSummary generates slow database / HTTP calls summary
func (b *QueryBuilder) BuildDependenciesSummary(depType string) TargetQuery {
	if err := b.checkTenancyFirewall(); err != nil {
		return TargetQuery{Backend: b.backend, Err: err}
	}
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
| extend P50 = todouble(P[0]), P90 = todouble(P[1]), P95 = todouble(P[2]), P99 = todouble(P[3]), ErrorRate = round(100.0 * toreal(FailedCalls) / toreal(TotalCalls), 2)
| project type, target, name, TotalCalls, FailedCalls, AvgDuration, MinDuration, MaxDuration, P50, P90, P95, P99, ErrorRate
| top %d by P95 desc`, b.limit))

	return TargetQuery{Query: sb.String(), Backend: b.backend}
}

// BuildExceptionsSummary groups exceptions and HTTP 5xx requests by type,
// normalized message, and affected routes, filtering client/bot noise. HTTP 5xx
// responses that never produced exception telemetry are synthesized from the
// requests table as "HTTP <code>" signatures, so services that fail without
// throwing tracked exceptions still surface in 'top errors'.
func (b *QueryBuilder) BuildExceptionsSummary() TargetQuery {
	if err := b.checkTenancyFirewall(); err != nil {
		return TargetQuery{Backend: b.backend, Err: err}
	}
	exceptionsBase := b.buildBaseFilters()
	reqBuilder := mustBuilder("requests").WithTimeRange(b.startTime, b.endTime).WithTarget(b.target)
	requestsBase := reqBuilder.buildBaseFilters()

	q := fmt.Sprintf(`union isfuzzy=true
(
    exceptions
%s
    | where not(type in ('ActionController::RoutingError', 'NotFoundHttpException', 'Sinatra::NotFound', 'System.OperationCanceledException', 'System.Threading.Tasks.TaskCanceledException', 'Microsoft.AspNetCore.Connections.ConnectionResetException'))
    | extend RawMsg = iff(isnotempty(column_ifexists('outerMessage', '')), column_ifexists('outerMessage', ''),
                      iff(isnotempty(column_ifexists('message', '')), column_ifexists('message', ''),
                      iff(isnotempty(column_ifexists('innermostMessage', '')), column_ifexists('innermostMessage', ''),
                      '<empty>')))
    | project timestamp, type, RawMsg, operation_Name
),
(
    requests
%s
    | where success == false and (toint(resultCode) >= 500 or isempty(resultCode))
    | extend type = iff(isempty(resultCode), 'HTTP 5xx', strcat('HTTP ', resultCode))
    | extend RawMsg = name
    | project timestamp, type, RawMsg, operation_Name
)
| where not(RawMsg has_any ('ClientClosedRequest', 'broken pipe', 'connection reset by peer', 'context canceled', 'request canceled'))
| extend CleanMessage = replace_regex(replace_regex(RawMsg, @"[0-9a-fA-F-]{36}", @"<UUID>"), @"\b\d{2,}\b", @"<ID>")
| summarize 
    Count = count(),
    FirstSeen = min(timestamp),
    LastSeen = max(timestamp),
    AffectedPaths = make_set(operation_Name, 10)
  by type, CleanMessage
| top %d by Count desc`, exceptionsBase, requestsBase, b.limit)
	return TargetQuery{Query: q, Backend: b.backend}
}

// BuildFanoutSummary measures N+1 and SQL fan-out by crossing requests with dependencies
func (b *QueryBuilder) BuildFanoutSummary() TargetQuery {
	if err := b.checkTenancyFirewall(); err != nil {
		return TargetQuery{Backend: b.backend, Err: err}
	}
	base := b.buildBaseClauses()
	depTimeFilter := ""
	if !b.startTime.IsZero() && !b.endTime.IsZero() {
		depTimeFilter = fmt.Sprintf("\n    | where timestamp between (datetime('%s') .. datetime('%s'))",
			FormatTime(b.startTime), FormatTime(b.endTime))
	}
	q := base + fmt.Sprintf(`| where success == true
| join kind=inner (
    dependencies%s
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
| top %d by AvgSqlCalls desc`, depTimeFilter, b.limit)
	return TargetQuery{Query: q, Backend: b.backend}
}

// BuildLatencyBreakdown breaks down request time across dependencies and app compute
func (b *QueryBuilder) BuildLatencyBreakdown() TargetQuery {
	if err := b.checkTenancyFirewall(); err != nil {
		return TargetQuery{Backend: b.backend, Err: err}
	}
	base := b.buildBaseClauses()
	depTimeFilter := ""
	if !b.startTime.IsZero() && !b.endTime.IsZero() {
		depTimeFilter = fmt.Sprintf("\n    | where timestamp between (datetime('%s') .. datetime('%s'))",
			FormatTime(b.startTime), FormatTime(b.endTime))
	}
	q := base + fmt.Sprintf(`| join kind=leftouter (
    dependencies%s
    | summarize 
        SqlTime = sumif(duration, type in ('SQL', 'mysql', 'MySQL', 'PostgreSQL', 'postgres', 'Azure SQL', 'SqlServer')),
        RedisTime = sumif(duration, type has 'redis'),
        HttpExtTime = sumif(duration, type == 'HTTP')
      by operation_Id
) on operation_Id
| extend AppComputeTime = duration - (coalesce(SqlTime, 0.0) + coalesce(RedisTime, 0.0) + coalesce(HttpExtTime, 0.0))
| summarize 
    AvgTotal = avg(duration),
    AvgSql = avg(coalesce(SqlTime, 0.0)),
    AvgHttp = avg(coalesce(HttpExtTime, 0.0)),
    AvgRedis = avg(coalesce(RedisTime, 0.0)),
    AvgApp = avg(coalesce(AppComputeTime, 0.0))
  by name
| extend 
    AvgTotalMs = round(AvgTotal, 1),
    PctDatabase = iff(AvgTotal > 0, round(100.0 * AvgSql / AvgTotal, 1), 0.0),
    PctExternalApi = iff(AvgTotal > 0, round(100.0 * AvgHttp / AvgTotal, 1), 0.0),
    PctCache = iff(AvgTotal > 0, round(100.0 * AvgRedis / AvgTotal, 1), 0.0),
    PctAppCode = iff(AvgTotal > 0, round(100.0 * AvgApp / AvgTotal, 1), 0.0)
| project name, AvgTotalMs, PctDatabase, PctExternalApi, PctCache, PctAppCode
| top %d by AvgTotalMs desc`, depTimeFilter, b.limit)
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
