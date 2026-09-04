package kql

import (
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
		Roles:            config.StringList{"order-service"},
		Pods:             config.StringList{"order-service-7f8d9b"},
		Logs:             config.LogsConfig{Namespace: "ecommerce-prod"},
		ExcludeSynthetic: config.BoolPtr(true),
		ExcludeProbes:    config.BoolPtr(true),
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

	// Verify pod instance scoping
	if !strings.Contains(query, "cloud_RoleInstance has 'order-service-7f8d9b'") {
		t.Errorf("expected cloud_RoleInstance filter, got: %s", query)
	}

	// Verify probe exclusion
	if !strings.Contains(query, "kube-probe") || !strings.Contains(query, "has_any") {
		t.Errorf("expected robust probe exclusion filter with kube-probe and has_any, got: %s", query)
	}

	// Verify direct percentile calculation
	if !strings.Contains(query, "P95 = percentile(duration, 95)") {
		t.Errorf("expected direct percentile aggregate, got: %s", query)
	}
	if strings.Contains(query, "P.percentile_duration") {
		t.Errorf("query contains invalid property access P.percentile_duration, got: %s", query)
	}
}

func TestDependenciesTaxonomy(t *testing.T) {
	b, _ := NewBuilder("dependencies")
	tq := b.BuildDependenciesSummary("SQL")

	if tq.Backend != BackendAppInsights {
		t.Errorf("expected BackendAppInsights, got: %s", tq.Backend)
	}

	sqlQuery := tq.Query
	if !strings.Contains(sqlQuery, "'Azure SQL'") || !strings.Contains(sqlQuery, "'PostgreSQL'") {
		t.Errorf("expected SQL taxonomy to include Azure SQL and PostgreSQL, got: %s", sqlQuery)
	}
}

func TestSanitizeNeutralizesKQLInjection(t *testing.T) {
	malicious := "x' or 1==1 //"

	tq := BuildRequestsSummaryQuery(
		time.Date(2026, 9, 2, 12, 0, 0, 0, time.UTC),
		time.Date(2026, 9, 2, 13, 0, 0, 0, time.UTC),
		config.TargetConfig{
			Roles: config.StringList{malicious},
			Pods:  config.StringList{`pod\' -x`},
			Logs:  config.LogsConfig{Namespace: "ns' drop table --"},
		},
	)

	q := tq.Query

	// Raw quote must be escaped so the literal cannot be terminated early
	if strings.Contains(q, "'x' or 1==1") {
		t.Errorf("query contains unescaped injection payload:\n%s", q)
	}
	if strings.Contains(q, "ns' drop") {
		t.Errorf("namespace injection not neutralized:\n%s", q)
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
	target := config.TargetConfig{Roles: config.StringList{"order-service"}}

	fanoutTQ := BuildFanoutSummaryQuery(start, end, target, 10)
	fanoutQ := fanoutTQ.Query
	if !strings.Contains(fanoutQ, "join kind=inner") || !strings.Contains(fanoutQ, "dependencies") {
		t.Errorf("expected inner join with dependencies in fanout query, got: %s", fanoutQ)
	}
	if !strings.Contains(fanoutQ, "AvgSqlCalls") {
		t.Errorf("expected AvgSqlCalls in fanout query, got: %s", fanoutQ)
	}

	attrTQ := BuildLatencyBreakdownQuery(start, end, target, 10)
	attrQ := attrTQ.Query
	if !strings.Contains(attrQ, "join kind=leftouter") || !strings.Contains(attrQ, "PctDatabase") {
		t.Errorf("expected leftouter join and PctDatabase in breakdown query, got: %s", attrQ)
	}
}

func TestMultiRoleAndPodFilters(t *testing.T) {
	start := time.Now().Add(-1 * time.Hour)
	end := time.Now()

	b, err := NewBuilder("requests")
	if err != nil {
		t.Fatalf("failed to create builder: %v", err)
	}
	tq := b.WithTimeRange(start, end).
		WithTarget(config.TargetConfig{
			Roles: config.StringList{"order-service", "billing-service", "returns-service"},
			Pods:  config.StringList{"order-service-7c9d", "order-service-8f2e"},
		}).BuildEndpointsSummary()

	q := tq.Query

	// Multi-role uses case-insensitive in~ (not a single =~ equality)
	if !strings.Contains(q, "cloud_RoleName in~ ('order-service', 'billing-service', 'returns-service')") {
		t.Errorf("expected multi-role in~ filter, got: %s", q)
	}
	if strings.Contains(q, "cloud_RoleName =~") {
		t.Errorf("single-value =~ must not be used with multiple roles, got: %s", q)
	}

	// Multi-pod uses term-indexed has_any
	if !strings.Contains(q, "cloud_RoleInstance has_any ('order-service-7c9d', 'order-service-8f2e')") {
		t.Errorf("expected multi-pod has_any filter, got: %s", q)
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
	if !strings.Contains(q, "order by QueryDurationMs desc") {
		t.Errorf("expected slowest queries ordering, got: %s", q)
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

func TestBuildDeprecationsQuery(t *testing.T) {
	start := time.Date(2026, 9, 2, 12, 0, 0, 0, time.UTC)
	end := time.Date(2026, 9, 2, 13, 0, 0, 0, time.UTC)
	target := config.TargetConfig{Roles: config.StringList{"order-service"}, ExcludeProbes: config.BoolPtr(true)}

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
	if !strings.Contains(q, "message has \"deprecated\"") {
		t.Errorf("expected deprecated filter in deprecations query, got: %s", q)
	}
	if !strings.Contains(q, "message startswith \"SELECT\"") {
		t.Errorf("expected SQL noise filter in deprecations query, got: %s", q)
	}
	if !strings.Contains(q, ":<line>") {
		t.Errorf("expected line number normalization in deprecations query, got: %s", q)
	}
}

func TestBuildExceptionsSummaryNoiseFiltering(t *testing.T) {
	b, _ := NewBuilder("exceptions")
	start := time.Now().Add(-1 * time.Hour)
	end := time.Now()
	target := config.TargetConfig{ExcludeProbes: config.BoolPtr(true), ExcludeSynthetic: config.BoolPtr(true)}

	tq := b.WithTimeRange(start, end).WithTarget(target).BuildExceptionsSummary()
	q := tq.Query
	if !strings.Contains(q, "ActionController::RoutingError") {
		t.Errorf("expected bot 404 routing filter in exceptions query, got: %s", q)
	}
	if !strings.Contains(q, "ClientClosedRequest") {
		t.Errorf("expected client drop filter in exceptions query, got: %s", q)
	}
	if !strings.Contains(q, "<UUID>") || !strings.Contains(q, "<ID>") {
		t.Errorf("expected dynamic ID normalization in exceptions query, got: %s", q)
	}
	if !strings.Contains(q, "operation_Name has_any") {
		t.Errorf("expected probe exclusion on operation_Name, got: %s", q)
	}
}
