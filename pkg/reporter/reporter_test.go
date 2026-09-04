package reporter

import (
	"bytes"
	"strings"
	"testing"
	"time"

	"github.com/denjamio/azlens/pkg/model"
)

func TestReporterOutputs(t *testing.T) {
	now := time.Now()
	report := model.DiffReport{
		AppName:        "test-service",
		BaselineWindow: model.TimeWindow{Start: now.Add(-2 * time.Hour), End: now.Add(-1 * time.Hour)},
		CurrentWindow:  model.TimeWindow{Start: now.Add(-1 * time.Hour), End: now},
		OverallVerdict: model.SeverityCritical,
		SummaryDeltas: []model.MetricDelta{
			{MetricName: "P95 Latency", Baseline: 100.0, Current: 250.0, Delta: 150.0, Percentage: 150.0, Unit: "ms", Severity: model.SeverityCritical},
		},
		EndpointDeltas: []model.EndpointDiff{
			{Name: "POST /checkout", Baseline: model.RequestMetric{Latency: model.LatencyPercentiles{P95: 100.0}}, Current: model.RequestMetric{Latency: model.LatencyPercentiles{P95: 250.0}}, P95DeltaPct: 150.0, Severity: model.SeverityCritical},
		},
		RootCauseHints: []string{"Degraded endpoint POST /checkout correlated with regressed SQL"},
	}

	// 1. Terminal Output
	var termBuf bytes.Buffer
	PrintDiffTerminal(&termBuf, report)
	termOut := termBuf.String()
	if !strings.Contains(termOut, "AZLENS DEPLOY REGRESSION REPORT") {
		t.Errorf("expected header in terminal output, got: %s", termOut)
	}
	if !strings.Contains(termOut, "Root-Cause & Correlation Insights") {
		t.Errorf("expected root cause in terminal output, got: %s", termOut)
	}

	// 2. Markdown Output
	var mdBuf bytes.Buffer
	PrintDiffMarkdown(&mdBuf, report)
	mdOut := mdBuf.String()
	if !strings.Contains(mdOut, "## 🚀 AzLens Deployment Regression Report") {
		t.Errorf("expected markdown title, got: %s", mdOut)
	}
	if !strings.Contains(mdOut, "Root-Cause & Correlation Insights") {
		t.Errorf("expected root cause in markdown output, got: %s", mdOut)
	}

	// 3. JSON Output
	var jsonBuf bytes.Buffer
	if err := PrintJSON(&jsonBuf, report); err != nil {
		t.Fatalf("failed rendering JSON: %v", err)
	}
	if !strings.Contains(jsonBuf.String(), `"app_name": "test-service"`) {
		t.Errorf("expected app_name in JSON, got: %s", jsonBuf.String())
	}
}

func TestGenericTableAndMarkdown(t *testing.T) {
	res := model.GenericQueryResult{
		Columns: []string{"ColA", "ColB"},
		Rows: [][]interface{}{
			{"Val1", 123},
		},
	}

	var termBuf bytes.Buffer
	PrintGenericTable(&termBuf, res)
	if !strings.Contains(termBuf.String(), "COLA") || !strings.Contains(termBuf.String(), "Val1") {
		t.Errorf("expected generic table output, got: %s", termBuf.String())
	}

	var mdBuf bytes.Buffer
	PrintGenericMarkdown(&mdBuf, res)
	if !strings.Contains(mdBuf.String(), "| ColA | ColB |") {
		t.Errorf("expected generic markdown output, got: %s", mdBuf.String())
	}
}

func TestSnapshotReporters(t *testing.T) {
	// Endpoints
	reqs := []model.RequestMetric{
		{Name: "GET /api/items", TotalCalls: 100, ErrorRate: 0.5, Latency: model.LatencyPercentiles{Avg: 20, P50: 10, P90: 30, P95: 45, P99: 80}},
	}
	var reqMd bytes.Buffer
	PrintRequestsMarkdown(&reqMd, reqs)
	if !strings.Contains(reqMd.String(), "GET /api/items") {
		t.Errorf("expected endpoint in markdown, got: %s", reqMd.String())
	}

	// Dependencies
	deps := []model.DependencyMetric{
		{Type: "SQL", Target: "mydb", Name: "SELECT 1", TotalCalls: 50, Latency: model.LatencyPercentiles{Avg: 10, P95: 25, P99: 50}},
	}
	var depMd bytes.Buffer
	PrintDependenciesMarkdown(&depMd, deps)
	if !strings.Contains(depMd.String(), "SELECT 1") {
		t.Errorf("expected dependency in markdown, got: %s", depMd.String())
	}

	// Errors
	errs := []model.ErrorSummary{
		{Type: "NullReferenceException", Message: "null pointer", Count: 10},
	}
	var errMd bytes.Buffer
	PrintErrorsMarkdown(&errMd, errs)
	if !strings.Contains(errMd.String(), "NullReferenceException") {
		t.Errorf("expected error in markdown, got: %s", errMd.String())
	}

	// Deprecations
	deprecations := []model.DeprecationSummary{
		{
			Message:           "DEPRECATION WARNING: update_attributes is deprecated",
			Count:             42,
			LastSeen:          time.Now(),
			AffectedEndpoints: []string{"POST /orders"},
		},
	}
	var deprecationMd bytes.Buffer
	PrintDeprecationsMarkdown(&deprecationMd, deprecations)
	if !strings.Contains(deprecationMd.String(), "update_attributes is deprecated") {
		t.Errorf("expected deprecation in markdown, got: %s", deprecationMd.String())
	}

	var deprecationTable bytes.Buffer
	PrintDeprecationsTable(&deprecationTable, deprecations)
	if !strings.Contains(deprecationTable.String(), "update_attributes") {
		t.Errorf("expected deprecation in table, got: %s", deprecationTable.String())
	}
}

func TestSlowLogsReporters(t *testing.T) {
	ts, _ := time.Parse(time.RFC3339, "2026-09-04T10:15:30Z")
	logs := []model.SlowLogEntry{
		{
			Timestamp:    ts,
			DurationSec:  14.52,
			DurationMs:   14520.0,
			RowsExamined: 1250000,
			RowsSent:     10,
			SQLText:      "SELECT * FROM large_table WHERE unindexed = 1",
		},
	}

	var tableBuf bytes.Buffer
	PrintSlowLogsTable(&tableBuf, logs)
	outTable := tableBuf.String()
	if !strings.Contains(outTable, "14.52s") || !strings.Contains(outTable, "1,250,000") || !strings.Contains(outTable, "large_table") {
		t.Errorf("expected formatted table output, got:\n%s", outTable)
	}

	var mdBuf bytes.Buffer
	PrintSlowLogsMarkdown(&mdBuf, logs)
	outMd := mdBuf.String()
	if !strings.Contains(outMd, "14.52s") || !strings.Contains(outMd, "1,250,000") || !strings.Contains(outMd, "large_table") {
		t.Errorf("expected formatted markdown output, got:\n%s", outMd)
	}
}
