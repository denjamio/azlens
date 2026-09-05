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
	ID      string
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

	// 4. Database Scope (MySqlSlowLogs in Log Analytics)
	if strings.EqualFold(b.table, "MySqlSlowLogs") && strings.TrimSpace(b.target.Logs.Database) != "" {
		sb.WriteString(fmt.Sprintf("| where %s\n", equalityExpr("Db", b.target.Logs.Database)))
	}

	// 5. Exclude synthetic availability test traffic unconditionally (convention over configuration)
	// In Application Insights, only the 'requests' table has the operation_SyntheticSource column.
	if strings.EqualFold(b.table, "requests") {
		sb.WriteString("| where isempty(operation_SyntheticSource)\n")
	}

	// 6. Custom Dimensions key-value scoping
	for k, v := range b.target.CustomDimensions {
		if k != "" && v != "" {
			sb.WriteString(fmt.Sprintf("| where tostring(customDimensions['%s']) =~ '%s'\n", sanitize(k), sanitize(v)))
		}
	}

	return sb.String()
}

// defaultProbeEndpoints lists standard framework health check endpoints
var defaultProbeEndpoints = []string{
	"/healthz", "/readyz", "/livez", "/startupz", "/health", "/healthcheck",
	"/ping", "/status", "/ready", "/live", "/up", "rails/health",
	"HealthController", "/actuator/health", "/actuator/info",
}

// defaultProbeUserAgents lists standard infrastructure probe User-Agents
var defaultProbeUserAgents = []string{
	"kube-probe", "GoogleHC", "ELB-HealthChecker", "ReadyForTraffic", "Consul", "Prometheus",
}

func defaultProbeExclusionClause() string {
	return "| where not(name has_any ('/healthz', '/readyz', '/livez', '/startupz', '/health', '/healthcheck', '/ping', '/status', '/ready', '/live', '/up', 'rails/health', 'HealthController', '/actuator/health', '/actuator/info'))\n| where isempty(customDimensions['User-Agent']) or not(tostring(customDimensions['User-Agent']) has_any ('kube-probe', 'GoogleHC', 'ELB-HealthChecker', 'ReadyForTraffic', 'Consul', 'Prometheus'))\n"
}

func (b *QueryBuilder) probeExclusionClause() string {
	if len(b.target.Endpoints.IgnoredEndpoints) == 0 && len(b.target.Endpoints.IgnoredUserAgents) == 0 && (b.target.Endpoints.IgnoreDefaultProbes == nil || *b.target.Endpoints.IgnoreDefaultProbes) {
		return defaultProbeExclusionClause()
	}

	var endpoints []string
	if b.target.Endpoints.IgnoreDefaultProbes == nil || *b.target.Endpoints.IgnoreDefaultProbes {
		endpoints = append(endpoints, defaultProbeEndpoints...)
	}
	endpoints = append(endpoints, b.target.Endpoints.IgnoredEndpoints...)

	var userAgents []string
	if b.target.Endpoints.IgnoreDefaultProbes == nil || *b.target.Endpoints.IgnoreDefaultProbes {
		userAgents = append(userAgents, defaultProbeUserAgents...)
	}
	userAgents = append(userAgents, b.target.Endpoints.IgnoredUserAgents...)

	var sb strings.Builder
	if len(endpoints) > 0 {
		var formatted []string
		for _, e := range endpoints {
			if trimmed := strings.TrimSpace(e); trimmed != "" {
				formatted = append(formatted, fmt.Sprintf("'%s'", sanitize(trimmed)))
			}
		}
		if len(formatted) > 0 {
			sb.WriteString(fmt.Sprintf("| where not(name has_any (%s))\n", strings.Join(formatted, ", ")))
		}
	}
	if len(userAgents) > 0 {
		var formatted []string
		for _, u := range userAgents {
			if trimmed := strings.TrimSpace(u); trimmed != "" {
				formatted = append(formatted, fmt.Sprintf("'%s'", sanitize(trimmed)))
			}
		}
		if len(formatted) > 0 {
			sb.WriteString(fmt.Sprintf("| where isempty(customDimensions['User-Agent']) or not(tostring(customDimensions['User-Agent']) has_any (%s))\n", strings.Join(formatted, ", ")))
		}
	}
	return sb.String()
}

func (b *QueryBuilder) sqlDependencyPredicate() string {
	return DynamicSQLDependencyPredicate(b.target.Dependencies.SQLTypes)
}

func (b *QueryBuilder) httpDependencyPredicate() string {
	return DynamicHTTPDependencyPredicate(b.target.Dependencies.HTTPTypes)
}

func (b *QueryBuilder) redisDependencyPredicate() string {
	return DynamicRedisDependencyPredicate(b.target.Dependencies.CacheTypes)
}

func (b *QueryBuilder) dependencyCategoryFilter(depType string) string {
	return DynamicDependencyCategoryFilter(depType, b.target.Dependencies.SQLTypes, b.target.Dependencies.HTTPTypes, b.target.Dependencies.CacheTypes)
}

// BuildRequestsSummary calculates throughput, failure rate, duration percentiles, and HTTP status breakdown.
// Percentiles are computed in a single percentiles() pass (one histogram scan
// instead of one pass per percentile); the values are identical to individual
// percentile() calls.
func (b *QueryBuilder) BuildRequestsSummary() TargetQuery {
	if err := b.checkTenancyFirewall(); err != nil {
		return TargetQuery{ID: QueryIDRequestsSummary, Backend: b.backend, Err: err}
	}
	base := b.buildBaseClauses()
	q := base + b.probeExclusionClause() + `| summarize 
    TotalCalls = count(),
    FailedCalls = countif(success == false),
    AvgDuration = avg(duration),
    MinDuration = min(duration),
    MaxDuration = max(duration),
    P = percentiles_array(duration, 50, 75, 90, 95, 99),
    Count2xx = countif(toint(resultCode) >= 200 and toint(resultCode) < 300),
    Count4xx = countif(toint(resultCode) >= 400 and toint(resultCode) < 500),
    Count5xx = countif(toint(resultCode) >= 500 or (isempty(resultCode) and success == false)),
    LastSeen = max(timestamp)
| extend 
    P50 = todouble(P[0]),
    P75 = todouble(P[1]),
    P90 = todouble(P[2]),
    P95 = todouble(P[3]),
    P99 = todouble(P[4]),
    ErrorRate = iff(TotalCalls > 0, round(100.0 * toreal(FailedCalls) / toreal(TotalCalls), 2), 0.0)
| project TotalCalls, FailedCalls, AvgDuration, MinDuration, MaxDuration, P50, P75, P90, P95, P99, ErrorRate, Count2xx, Count4xx, Count5xx, LastSeen`
	return TargetQuery{ID: QueryIDRequestsSummary, Query: q, Backend: b.backend}
}

// BuildEndpointsSummary generates per-endpoint latency percentiles and error rates,
// returning the top N by P95 (top-N heap instead of a full sort)
func (b *QueryBuilder) BuildEndpointsSummary() TargetQuery {
	if err := b.checkTenancyFirewall(); err != nil {
		return TargetQuery{ID: QueryIDRequestsEndpoints, Backend: b.backend, Err: err}
	}
	base := b.buildBaseClauses()
	q := base + b.probeExclusionClause() + fmt.Sprintf(`| summarize 
    TotalCalls = count(),
    FailedCalls = countif(success == false),
    AvgDuration = avg(duration),
    MinDuration = min(duration),
    MaxDuration = max(duration),
    P = percentiles_array(duration, 50, 75, 90, 95, 99)
  by Endpoint = name
| extend P50 = todouble(P[0]), P75 = todouble(P[1]), P90 = todouble(P[2]), P95 = todouble(P[3]), P99 = todouble(P[4]), ErrorRate = iff(TotalCalls > 0, round(100.0 * toreal(FailedCalls) / toreal(TotalCalls), 2), 0.0)
| project Endpoint, TotalCalls, FailedCalls, AvgDuration, MinDuration, MaxDuration, P50, P75, P90, P95, P99, ErrorRate
| top %d by P95 desc`, b.limit)
	return TargetQuery{ID: QueryIDRequestsEndpoints, Query: q, Backend: b.backend}
}

// BuildDependenciesSummary generates slow database / HTTP calls summary
func (b *QueryBuilder) BuildDependenciesSummary(depType string) TargetQuery {
	qid := QueryIDDependenciesAll
	switch strings.ToUpper(strings.TrimSpace(depType)) {
	case CategorySQL:
		qid = QueryIDDependenciesSQL
	case CategoryHTTP:
		qid = QueryIDDependenciesHTTP
	case CategoryRedis:
		qid = QueryIDDependenciesRedis
	}
	if err := b.checkTenancyFirewall(); err != nil {
		return TargetQuery{ID: qid, Backend: b.backend, Err: err}
	}
	var sb strings.Builder
	sb.WriteString(b.buildBaseClauses())
	sb.WriteString(b.dependencyCategoryFilter(depType))
	if len(b.target.Dependencies.IgnoredTargets) > 0 {
		var cleanTargets []string
		for _, t := range b.target.Dependencies.IgnoredTargets {
			if trimmed := strings.TrimSpace(t); trimmed != "" {
				cleanTargets = append(cleanTargets, fmt.Sprintf("'%s'", sanitize(trimmed)))
			}
		}
		if len(cleanTargets) > 0 {
			sb.WriteString(fmt.Sprintf("| where not(target in~ (%s))\n", strings.Join(cleanTargets, ", ")))
		}
	}

	sb.WriteString(fmt.Sprintf(`| summarize 
    TotalCalls = count(),
    FailedCalls = countif(success == false),
    AvgDuration = avg(duration),
    MinDuration = min(duration),
    MaxDuration = max(duration),
    P = percentiles_array(duration, 50, 90, 95, 99)
  by Type = type, Target = target, Dependency = name
| extend P50 = todouble(P[0]), P90 = todouble(P[1]), P95 = todouble(P[2]), P99 = todouble(P[3]), ErrorRate = iff(TotalCalls > 0, round(100.0 * toreal(FailedCalls) / toreal(TotalCalls), 2), 0.0)
| project Type, Target, Dependency, TotalCalls, FailedCalls, AvgDuration, MinDuration, MaxDuration, P50, P90, P95, P99, ErrorRate
| top %d by P95 desc`, b.limit))

	return TargetQuery{ID: qid, Query: sb.String(), Backend: b.backend}
}

// BuildExceptionsSummary groups exceptions and HTTP 5xx requests by structured type,
// returning real sample messages, source origin (exception vs request_5xx), and affected routes.
func (b *QueryBuilder) BuildExceptionsSummary() TargetQuery {
	if err := b.checkTenancyFirewall(); err != nil {
		return TargetQuery{ID: QueryIDExceptionsSummary, Backend: b.backend, Err: err}
	}
	exceptionsBase := b.buildBaseFilters()
	if len(b.target.Exceptions.IgnoredTypes) > 0 {
		var cleanTypes []string
		for _, t := range b.target.Exceptions.IgnoredTypes {
			if trimmed := strings.TrimSpace(t); trimmed != "" {
				cleanTypes = append(cleanTypes, fmt.Sprintf("'%s'", sanitize(trimmed)))
			}
		}
		if len(cleanTypes) > 0 {
			exceptionsBase += fmt.Sprintf("| where not(type in~ (%s))\n", strings.Join(cleanTypes, ", "))
		}
	}
	if len(b.target.Exceptions.IgnoredMessages) > 0 {
		var cleanMsgs []string
		for _, m := range b.target.Exceptions.IgnoredMessages {
			if trimmed := strings.TrimSpace(m); trimmed != "" {
				cleanMsgs = append(cleanMsgs, fmt.Sprintf("'%s'", sanitize(trimmed)))
			}
		}
		if len(cleanMsgs) > 0 {
			exceptionsBase += fmt.Sprintf("| where not(coalesce(iff(isnotempty(innermostMessage), innermostMessage, outerMessage), message, \"<empty>\") has_any (%s))\n", strings.Join(cleanMsgs, ", "))
		}
	}

	reqBuilder := mustBuilder("requests").WithTimeRange(b.startTime, b.endTime).WithTarget(b.target)
	requestsBase := reqBuilder.buildBaseFilters()

	q := fmt.Sprintf(`union isfuzzy=true
(
    exceptions
%s    | extend Source = 'exception'
    | extend RawMsg = coalesce(iff(isnotempty(innermostMessage), innermostMessage, outerMessage), message, type, "<empty>")
    | project timestamp, Source, type, RawMsg, operation_Name
),
(
    requests
%s    | where success == false and (toint(resultCode) >= 500 or isempty(resultCode))
    | extend Source = 'request_5xx'
    | extend type = iff(isempty(resultCode), 'HTTP 5xx', strcat('HTTP ', resultCode))
    | extend RawMsg = coalesce(name, "<empty>")
    | project timestamp, Source, type, RawMsg, operation_Name = name
)
| summarize 
    Count = count(),
    SampleMessage = any(RawMsg),
    Source = any(Source),
    FirstSeen = min(timestamp),
    LastSeen = max(timestamp),
    AffectedPaths = make_set(operation_Name, 10)
  by Type = type
| project Type, Source, Message = SampleMessage, Count, FirstSeen, LastSeen, AffectedPaths
| top %d by Count desc`, exceptionsBase, requestsBase, b.limit)
	return TargetQuery{ID: QueryIDExceptionsSummary, Query: q, Backend: b.backend}
}

// BuildFanoutSummary measures SQL fan-out distribution across endpoints
func (b *QueryBuilder) BuildFanoutSummary() TargetQuery {
	if err := b.checkTenancyFirewall(); err != nil {
		return TargetQuery{ID: QueryIDFanoutSQL, Backend: b.backend, Err: err}
	}
	base := b.buildBaseClauses()
	depFilters := ""
	if !b.startTime.IsZero() && !b.endTime.IsZero() {
		depFilters += fmt.Sprintf("\n    | where timestamp between (datetime('%s') .. datetime('%s'))",
			FormatTime(b.startTime), FormatTime(b.endTime))
	}
	if strings.TrimSpace(b.target.RoleName) != "" {
		depFilters += fmt.Sprintf("\n    | where %s", equalityExpr("cloud_RoleName", b.target.RoleName))
	}
	q := base + fmt.Sprintf(`| where success == true
| project operation_Id, name, duration
| join kind=inner (
    dependencies%s
    | where %s
    | summarize SqlCalls = count(), SqlDuration = sum(duration) by operation_Id
) on operation_Id
| summarize 
    TotalRequests = count(),
    AvgSqlCalls = round(avg(SqlCalls), 1),
    P_Calls = percentiles_array(SqlCalls, 50, 75, 90, 95, 99),
    MaxSqlCalls = max(SqlCalls),
    AvgSQLDuration = round(avg(SqlDuration), 1),
    AvgEndpointDuration = round(avg(duration), 1)
  by Endpoint = name
| extend 
    P50_Calls = todouble(P_Calls[0]),
    P75_Calls = todouble(P_Calls[1]),
    P90_Calls = todouble(P_Calls[2]),
    P95_Calls = todouble(P_Calls[3]),
    P99_Calls = todouble(P_Calls[4])
| where P95_Calls > 1.0 or MaxSqlCalls >= 5 or AvgSqlCalls > 1.0
| project Endpoint, TotalRequests, AvgSqlCalls, P50_Calls, P75_Calls, P90_Calls, P95_Calls, P99_Calls, MaxSqlCalls, AvgSQLDuration, AvgEndpointDuration
| top %d by P95_Calls desc`, depFilters, b.sqlDependencyPredicate(), b.limit)
	return TargetQuery{ID: QueryIDFanoutSQL, Query: q, Backend: b.backend}
}

// BuildNPlusOneCandidateSummary detects deterministic N+1 candidates by requiring evidence
// of repeated SQL query shapes within single requests (Section 6.2).
func (b *QueryBuilder) BuildNPlusOneCandidateSummary() TargetQuery {
	if err := b.checkTenancyFirewall(); err != nil {
		return TargetQuery{ID: QueryIDNPlusOneCandidate, Backend: b.backend, Err: err}
	}
	depBase := b.buildBaseClauses()
	reqFilters := ""
	if !b.startTime.IsZero() && !b.endTime.IsZero() {
		reqFilters += fmt.Sprintf("\n    | where timestamp between (datetime('%s') .. datetime('%s'))",
			FormatTime(b.startTime), FormatTime(b.endTime))
	}
	if strings.TrimSpace(b.target.RoleName) != "" {
		reqFilters += fmt.Sprintf("\n    | where %s", equalityExpr("cloud_RoleName", b.target.RoleName))
	}

	q := depBase + fmt.Sprintf(`| where %s
| summarize 
    ShapeCalls = count(), 
    ShapeDuration = sum(duration) 
  by operation_Id, QueryShape = strcat(target, ': ', name)
| summarize 
    TotalSqlCalls = sum(ShapeCalls),
    DistinctShapes = count(),
    RepeatedShapeCalls = sumif(ShapeCalls, ShapeCalls > 1),
    MaxRepeatedShapeCalls = max(ShapeCalls),
    RepeatedShapeDuration = sumif(ShapeDuration, ShapeCalls > 1),
    TopRepeatedShape = anyif(QueryShape, ShapeCalls > 1)
  by operation_Id
| extend RepeatedShapeRatio = round(100.0 * toreal(RepeatedShapeCalls) / toreal(TotalSqlCalls), 1)
| where TotalSqlCalls >= 5 and MaxRepeatedShapeCalls >= 3
| join kind=inner (
    requests%s
    | where success == true
    | project operation_Id, Endpoint = name, duration
) on operation_Id
| summarize 
    TotalRequests = count(),
    AvgSqlCalls = round(avg(TotalSqlCalls), 1),
    MaxSqlCalls = max(TotalSqlCalls),
    AvgRepeatedCalls = round(avg(RepeatedShapeCalls), 1),
    MaxRepeatedShape = max(MaxRepeatedShapeCalls),
    AvgRepeatedRatio = round(avg(RepeatedShapeRatio), 1),
    SampleRepeatedShape = any(TopRepeatedShape),
    AvgRepeatedDuration = round(avg(RepeatedShapeDuration), 1),
    AvgEndpointDuration = round(avg(duration), 1)
  by Endpoint
| where AvgRepeatedRatio >= 40.0 and MaxRepeatedShape >= 5
| project Endpoint, TotalRequests, AvgSqlCalls, MaxSqlCalls, AvgRepeatedCalls, MaxRepeatedShape, AvgRepeatedRatio, SampleRepeatedShape, AvgRepeatedDuration, AvgEndpointDuration
| top %d by MaxRepeatedShape desc`, b.sqlDependencyPredicate(), reqFilters, b.limit)

	return TargetQuery{ID: QueryIDNPlusOneCandidate, Query: q, Backend: b.backend}
}

// BuildLatencyBreakdown breaks down request time across dependencies and residual compute,
// explicitly flagging concurrent dependency overlap (Section 7).
func (b *QueryBuilder) BuildLatencyBreakdown() TargetQuery {
	if err := b.checkTenancyFirewall(); err != nil {
		return TargetQuery{ID: QueryIDLatencyBreakdown, Backend: b.backend, Err: err}
	}
	base := b.buildBaseClauses()
	depFilters := ""
	if !b.startTime.IsZero() && !b.endTime.IsZero() {
		depFilters += fmt.Sprintf("\n    | where timestamp between (datetime('%s') .. datetime('%s'))",
			FormatTime(b.startTime), FormatTime(b.endTime))
	}
	if strings.TrimSpace(b.target.RoleName) != "" {
		depFilters += fmt.Sprintf("\n    | where %s", equalityExpr("cloud_RoleName", b.target.RoleName))
	}
	q := base + fmt.Sprintf(`| project operation_Id, name, duration
| join kind=leftouter (
    dependencies%s
    | summarize 
        SqlTime = sumif(duration, %s),
        RedisTime = sumif(duration, %s),
        HttpExtTime = sumif(duration, %s)
      by operation_Id
) on operation_Id
| extend TotalDepTime = coalesce(SqlTime, 0.0) + coalesce(RedisTime, 0.0) + coalesce(HttpExtTime, 0.0)
| extend ResidualRequestTime = max_of(0.0, duration - TotalDepTime)
| extend OverlapDetected = iff(TotalDepTime > duration, true, false)
| summarize 
    AvgTotal = avg(duration),
    AvgSql = avg(coalesce(SqlTime, 0.0)),
    AvgHttp = avg(coalesce(HttpExtTime, 0.0)),
    AvgRedis = avg(coalesce(RedisTime, 0.0)),
    AvgResidual = avg(ResidualRequestTime),
    OverlapCount = countif(OverlapDetected == true)
  by Endpoint = name
| extend 
    AvgTotalMs = round(AvgTotal, 1),
    PctDatabase = iff(AvgTotal > 0, round(100.0 * AvgSql / AvgTotal, 1), 0.0),
    PctExternalApi = iff(AvgTotal > 0, round(100.0 * AvgHttp / AvgTotal, 1), 0.0),
    PctCache = iff(AvgTotal > 0, round(100.0 * AvgRedis / AvgTotal, 1), 0.0),
    PctResidual = iff(AvgTotal > 0, round(100.0 * AvgResidual / AvgTotal, 1), 0.0),
    HasOverlap = iff(OverlapCount > 0, true, false)
| project Endpoint, AvgTotalMs, PctDatabase, PctExternalApi, PctCache, PctResidual, HasOverlap
| top %d by AvgTotalMs desc`, depFilters, b.sqlDependencyPredicate(), b.redisDependencyPredicate(), b.httpDependencyPredicate(), b.limit)
	return TargetQuery{ID: QueryIDLatencyBreakdown, Query: q, Backend: b.backend}
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
