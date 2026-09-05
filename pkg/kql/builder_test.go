package kql

import (
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/denjamio/azlens/pkg/config"
)

func TestQueryBuilderScopeAndPerformance(t *testing.T) {
	start := time.Date(2026, 9, 2, 12, 0, 0, 0, time.UTC)
	end := time.Date(2026, 9, 2, 13, 0, 0, 0, time.UTC)

	b, err := NewBuilder("requests")
	if err != nil {
		t.Fatalf("failed to create builder: %v", err)
	}

	target := config.TargetConfig{
		RoleName: "order-service",
		Pod:      "order-service-7f8d9b",
		Logs:     config.LogsConfig{Database: "ecommerce_db"},
		CustomDimensions: map[string]string{
			"version": "v2.1.0",
		},
	}

	tq := b.WithTimeRange(start, end).
		WithTarget(target).
		WithLimit(15).
		BuildEndpointsSummary()

	if tq.Backend != BackendAppInsights {
		t.Errorf("expected BackendAppInsights, got: %s", tq.Backend)
	}

	query := tq.Query

	// Verify partition pruning (timestamp first)
	if !strings.Contains(query, "where timestamp between") {
		t.Errorf("expected timestamp filter, got: %s", query)
	}

	// Verify cloud_RoleName scoping
	if !strings.Contains(query, "cloud_RoleName =~ 'order-service'") {
		t.Errorf("expected cloud_RoleName filter, got: %s", query)
	}

	// Verify probe exclusion
	if !strings.Contains(query, "kube-probe") || !strings.Contains(query, "has_any") {
		t.Errorf("expected robust probe exclusion filter with kube-probe and has_any, got: %s", query)
	}

	// Verify single-pass percentile aggregation (one histogram scan) and
	// scalar expansion without the illegal dynamic property access
	if !strings.Contains(query, "P = percentiles_array(duration, 50, 75, 90, 95, 99)") {
		t.Errorf("expected single-pass percentiles aggregate, got: %s", query)
	}
	if !strings.Contains(query, "P95 = todouble(P[3])") {
		t.Errorf("expected P95 scalar expansion from the percentiles array, got: %s", query)
	}
	if strings.Contains(query, "P.percentile_duration") {
		t.Errorf("query contains invalid property access P.percentile_duration, got: %s", query)
	}
}

func TestDependenciesTaxonomy(t *testing.T) {
	b, _ := NewBuilder("dependencies")
	tq := b.WithTarget(config.TargetConfig{RoleName: "order-service"}).BuildDependenciesSummary("SQL")

	if tq.Backend != BackendAppInsights {
		t.Errorf("expected BackendAppInsights, got: %s", tq.Backend)
	}

	sqlQuery := tq.Query
	if !strings.Contains(sqlQuery, "'Azure SQL'") || !strings.Contains(sqlQuery, "'PostgreSQL'") {
		t.Errorf("expected SQL taxonomy to include Azure SQL and PostgreSQL, got: %s", sqlQuery)
	}
	if strings.Contains(sqlQuery, "operation_Name") {
		t.Errorf("dependencies table in Azure Application Insights does not have operation_Name column, got: %s", sqlQuery)
	}
	if strings.Contains(sqlQuery, "operation_SyntheticSource") {
		t.Errorf("dependencies table in Azure Application Insights does not have operation_SyntheticSource column, got: %s", sqlQuery)
	}
}

func TestSanitizeNeutralizesKQLInjection(t *testing.T) {
	malicious := "x' or 1==1 //"

	tq := BuildRequestsSummaryQuery(
		time.Date(2026, 9, 2, 12, 0, 0, 0, time.UTC),
		time.Date(2026, 9, 2, 13, 0, 0, 0, time.UTC),
		config.TargetConfig{
			RoleName:         malicious,
			Pod:              `pod\' -x`,
			CustomDimensions: map[string]string{"env": "ns' drop table --"},
		},
	)

	q := tq.Query

	// Raw quote must be escaped so the literal cannot be terminated early
	if strings.Contains(q, "'x' or 1==1") {
		t.Errorf("query contains unescaped injection payload:\n%s", q)
	}
	if strings.Contains(q, "ns' drop") {
		t.Errorf("custom dimension injection not neutralized:\n%s", q)
	}
	if strings.Contains(q, ";") {
		t.Errorf("statement separator ';' must be stripped from sanitized values:\n%s", q)
	}
	if !strings.Contains(q, `x\' or 1==1 //`) {
		t.Errorf("expected escaped payload \\' in query:\n%s", q)
	}
}

func TestSanitizeRemovesNewlines(t *testing.T) {
	s := Sanitize("a\nb\rc;d\\e'f")
	if strings.ContainsAny(s, "\n\r;") {
		t.Errorf("sanitize must strip newlines and semicolons, got: %q", s)
	}
	if !strings.Contains(s, `\\e`) || !strings.Contains(s, `\'f`) {
		t.Errorf("sanitize must escape backslash and quote, got: %q", s)
	}
}

func TestNewBuilderRejectsUnknownTable(t *testing.T) {
	if _, err := NewBuilder("AngularHttp"); err == nil {
		t.Errorf("expected error for non-whitelisted table")
	}
	if _, err := NewBuilder("requests"); err != nil {
		t.Errorf("whitelisted table must be accepted: %v", err)
	}
}

func TestCrossCorrelationsQueries(t *testing.T) {
	start := time.Now().Add(-1 * time.Hour)
	end := time.Now()
	target := config.TargetConfig{RoleName: "order-service"}

	fanoutTQ := BuildFanoutSummaryQuery(start, end, target, 10)
	fanoutQ := fanoutTQ.Query
	if !strings.Contains(fanoutQ, "join kind=inner") || !strings.Contains(fanoutQ, "dependencies") {
		t.Errorf("expected inner join with dependencies in fanout query, got: %s", fanoutQ)
	}
	if !strings.Contains(fanoutQ, "AvgSqlCalls") {
		t.Errorf("expected AvgSqlCalls in fanout query, got: %s", fanoutQ)
	}
	if strings.Contains(fanoutQ, "dependencies\n    | where cloud_RoleName") {
		t.Errorf("dependencies subquery should not filter cloud_RoleName (scoped via operation_Id join to preserve dependencies without role tag), got: %s", fanoutQ)
	}
	if !strings.Contains(fanoutQ, "postgres") {
		t.Errorf("expected postgresql support in fanout query, got: %s", fanoutQ)
	}

	attrTQ := BuildLatencyBreakdownQuery(start, end, target, 10)
	attrQ := attrTQ.Query
	if !strings.Contains(attrQ, "join kind=leftouter") || !strings.Contains(attrQ, "PctDatabase") {
		t.Errorf("expected leftouter join and PctDatabase in breakdown query, got: %s", attrQ)
	}
	if !strings.Contains(attrQ, "postgres") {
		t.Errorf("expected postgresql support in breakdown query, got: %s", attrQ)
	}
}

func TestSingularRoleFilter(t *testing.T) {
	start := time.Now().Add(-1 * time.Hour)
	end := time.Now()

	b, err := NewBuilder("requests")
	if err != nil {
		t.Fatalf("failed to create builder: %v", err)
	}
	tq := b.WithTimeRange(start, end).
		WithTarget(config.TargetConfig{
			RoleName: "order-service",
		}).BuildEndpointsSummary()

	q := tq.Query

	if !strings.Contains(q, "cloud_RoleName =~ 'order-service'") {
		t.Errorf("expected singular role =~ filter, got: %s", q)
	}
	if strings.Contains(q, "cloud_RoleInstance") {
		t.Errorf("cloud_RoleInstance should not be filtered by default, got: %s", q)
	}
}

func TestTenancyFirewallBlocksUnscopedQueries(t *testing.T) {
	start := time.Now().Add(-1 * time.Hour)
	end := time.Now()

	b, err := NewBuilder("requests")
	if err != nil {
		t.Fatalf("failed to create builder: %v", err)
	}
	tq := b.WithTimeRange(start, end).
		WithTarget(config.TargetConfig{RoleName: ""}).
		BuildEndpointsSummary()

	if tq.Err == nil {
		t.Fatalf("expected ErrMissingRole from tenancy firewall, got nil")
	}
	if !strings.Contains(tq.Err.Error(), "tenancy firewall") {
		t.Errorf("expected tenancy firewall error, got: %v", tq.Err)
	}
}

func TestBuildMySqlSlowLogsQuery(t *testing.T) {
	start := time.Date(2026, 9, 2, 12, 0, 0, 0, time.UTC)
	end := time.Date(2026, 9, 2, 13, 0, 0, 0, time.UTC)

	tq := BuildMySQLSlowLogsQuery(start, end, "backend_ror", 15)
	if tq.Backend != BackendLogAnalytics {
		t.Errorf("expected BackendLogAnalytics, got: %s", tq.Backend)
	}
	q := tq.Query
	if !strings.Contains(q, "where Db =~ 'backend_ror'") {
		t.Errorf("expected exact Db filter in query, got: %s", q)
	}
	if strings.Contains(q, "DatabaseName_s") {
		t.Errorf("query should not contain non-existent DatabaseName_s, got: %s", q)
	}
	if !strings.Contains(q, "top 15 by QueryDurationMs desc") {
		t.Errorf("expected top-N by query duration, got: %s", q)
	}
	if !strings.Contains(q, "QueryDurationMs") {
		t.Errorf("expected QueryDurationMs column, got: %s", q)
	}
	if !strings.Contains(q, "SqlText") {
		t.Errorf("expected SqlText column, got: %s", q)
	}
	if strings.Contains(q, "string(null)") {
		t.Errorf("query contains invalid KQL syntax string(null), got: %s", q)
	}
}

func TestBuildMySqlSlowLogsGroupedQuery(t *testing.T) {
	start := time.Date(2026, 9, 2, 12, 0, 0, 0, time.UTC)
	end := time.Date(2026, 9, 2, 13, 0, 0, 0, time.UTC)

	tq := BuildMySQLSlowLogsGroupedQuery(start, end, "backend_ror", 15)
	if tq.Backend != BackendLogAnalytics {
		t.Errorf("expected BackendLogAnalytics, got: %s", tq.Backend)
	}
	q := tq.Query
	if !strings.Contains(q, "where Db =~ 'backend_ror'") {
		t.Errorf("expected exact Db filter in query, got: %s", q)
	}
	if !strings.Contains(q, "summarize") || !strings.Contains(q, "Executions = count()") {
		t.Errorf("expected per-fingerprint aggregation, got: %s", q)
	}
	for _, metric := range []string{"AvgMs = round(avg(QueryDurationMs), 1)", "MaxMs = max(QueryDurationMs)", "TotalMs = round(sum(QueryDurationMs), 1)", "AvgRowsExamined"} {
		if !strings.Contains(q, metric) {
			t.Errorf("expected %q aggregation, got: %s", metric, q)
		}
	}
	if !strings.Contains(q, "top 15 by TotalMs desc") {
		t.Errorf("expected top-N by total accumulated duration, got: %s", q)
	}
	if strings.Contains(q, "string(null)") {
		t.Errorf("query contains invalid KQL syntax string(null), got: %s", q)
	}
}

func TestBuildMySqlSlowLogsTenancyFirewall(t *testing.T) {
	start := time.Date(2026, 9, 2, 12, 0, 0, 0, time.UTC)
	end := time.Date(2026, 9, 2, 13, 0, 0, 0, time.UTC)

	// 1. BuildMySQLSlowLogsQuery with empty db must return ErrMissingDatabase
	tqRaw := BuildMySQLSlowLogsQuery(start, end, "", 15)
	if !errors.Is(tqRaw.Err, ErrMissingDatabase) {
		t.Errorf("expected ErrMissingDatabase for empty dbName, got: %v", tqRaw.Err)
	}

	// 2. BuildMySQLSlowLogsGroupedQuery with empty db must return ErrMissingDatabase
	tqGrouped := BuildMySQLSlowLogsGroupedQuery(start, end, "  ", 15)
	if !errors.Is(tqGrouped.Err, ErrMissingDatabase) {
		t.Errorf("expected ErrMissingDatabase for whitespace dbName, got: %v", tqGrouped.Err)
	}

	// 3. QueryBuilder against MySqlSlowLogs must enforce checkTenancyFirewall
	b, err := NewBuilder("MySqlSlowLogs")
	if err != nil {
		t.Fatalf("failed to create builder: %v", err)
	}
	b.WithTimeRange(start, end).WithTarget(config.TargetConfig{
		Logs: config.LogsConfig{Database: ""},
	})
	if err := b.checkTenancyFirewall(); !errors.Is(err, ErrMissingDatabase) {
		t.Errorf("expected checkTenancyFirewall to return ErrMissingDatabase, got: %v", err)
	}

	b.WithTarget(config.TargetConfig{
		Logs: config.LogsConfig{Database: "ecommerce_db"},
	})
	if err := b.checkTenancyFirewall(); err != nil {
		t.Errorf("expected checkTenancyFirewall to succeed with valid database, got: %v", err)
	}
}

func TestBuildDeprecationsQuery(t *testing.T) {
	start := time.Date(2026, 9, 2, 12, 0, 0, 0, time.UTC)
	end := time.Date(2026, 9, 2, 13, 0, 0, 0, time.UTC)
	target := config.TargetConfig{RoleName: "order-service"}

	tq := BuildDeprecationsQuery(start, end, target, 15)
	if tq.Backend != BackendAppInsights {
		t.Errorf("expected BackendAppInsights, got: %s", tq.Backend)
	}
	q := tq.Query
	if !strings.Contains(q, "union isfuzzy=true") {
		t.Errorf("expected union isfuzzy=true in deprecations query, got: %s", q)
	}
	if !strings.Contains(q, "cloud_RoleName =~ 'order-service'") {
		t.Errorf("expected role filter in deprecations query, got: %s", q)
	}
	if !strings.Contains(q, "message has_any (\"deprecated\"") {
		t.Errorf("expected deprecated filter in deprecations query, got: %s", q)
	}
	if !strings.Contains(q, "message startswith \"SELECT\"") {
		t.Errorf("expected SQL noise filter in deprecations query, got: %s", q)
	}
	if strings.Contains(q, "replace_regex") {
		t.Errorf("deprecations query should not contain regex normalization, got: %s", q)
	}
	if !strings.Contains(q, "substring(message, 0, 200)") {
		t.Errorf("expected substring grouping in deprecations query, got: %s", q)
	}
	if strings.Contains(q, "operation_SyntheticSource") {
		t.Errorf("traces and exceptions tables do not have operation_SyntheticSource column, got: %s", q)
	}
}

func TestBuildExceptionsSummaryNoiseFiltering(t *testing.T) {
	b, _ := NewBuilder("exceptions")
	start := time.Now().Add(-1 * time.Hour)
	end := time.Now()
	target := config.TargetConfig{RoleName: "order-service"}

	tq := b.WithTimeRange(start, end).WithTarget(target).BuildExceptionsSummary()
	q := tq.Query
	if !strings.Contains(q, "union isfuzzy=true") {
		t.Errorf("expected exceptions + requests union, got: %s", q)
	}
	if strings.Contains(q, "ActionController::RoutingError") {
		t.Errorf("exceptions query should be stack-agnostic and not contain ActionController::RoutingError, got: %s", q)
	}
	if !strings.Contains(q, "toint(resultCode) >= 500") || !strings.Contains(q, "strcat('HTTP ', resultCode)") {
		t.Errorf("expected HTTP 5xx synthesis from requests, got: %s", q)
	}
	if strings.Contains(q, "ClientClosedRequest") {
		t.Errorf("exceptions query should be stack-agnostic and not contain ClientClosedRequest, got: %s", q)
	}
	if !strings.Contains(q, "coalesce(iff(isnotempty(innermostMessage)") {
		t.Errorf("expected coalesce for RawMsg in exceptions query, got: %s", q)
	}
	if !strings.Contains(q, "operation_Name = name") {
		t.Errorf("expected requests branch to project operation_Name = name, got: %s", q)
	}
	if strings.Contains(q, "replace_regex") {
		t.Errorf("exceptions query should not contain regex normalization, got: %s", q)
	}
	if !strings.Contains(q, "SampleMessage = any(RawMsg)") {
		t.Errorf("expected native sample message extraction in exceptions query, got: %s", q)
	}
	if !strings.Contains(q, "by Type = type") {
		t.Errorf("expected grouping by structured Type, got: %s", q)
	}
	// Both union branches must be partition-pruned by the same time window
	if got := strings.Count(q, "timestamp between (datetime("); got != 2 {
		t.Errorf("expected time filter in both union branches, got %d, in: %s", got, q)
	}
}
