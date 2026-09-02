package analyzer

import (
	"testing"
	"time"

	"github.com/denjamio/azlens/pkg/model"
)

func TestCompareWindowsRegressionDetection(t *testing.T) {
	now := time.Now()
	baseWin := model.TimeWindow{Start: now.Add(-2 * time.Hour), End: now.Add(-1 * time.Hour)}
	currWin := model.TimeWindow{Start: now.Add(-1 * time.Hour), End: now}

	baseReq := model.RequestMetric{
		Name:        "Overall",
		TotalCalls:  10000,
		FailedCalls: 20,
		ErrorRate:   0.20,
		Latency: model.LatencyPercentiles{
			P50: 50.0,
			P90: 100.0,
			P95: 150.0,
			P99: 250.0,
		},
	}

	// Post deploy with critical P95 latency regression (+100%) and error rate spike
	currReq := model.RequestMetric{
		Name:        "Overall",
		TotalCalls:  11000,
		FailedCalls: 440,
		ErrorRate:   4.00, // +3.80% absolute increase
		Latency: model.LatencyPercentiles{
			P50: 60.0,
			P90: 220.0,
			P95: 310.0, // +106.6% increase
			P99: 600.0,
		},
	}

	thresholds := DefaultThresholds()

	report := Compare(CompareOptions{
		AppName:        "test-app",
		BaselineWindow: baseWin,
		CurrentWindow:  currWin,
		BaseReqOverall: baseReq,
		CurrReqOverall: currReq,
		Thresholds:     thresholds,
	})

	if report.OverallVerdict != model.SeverityCritical {
		t.Fatalf("expected overall verdict to be CRITICAL, got %s", report.OverallVerdict)
	}

	if len(report.SummaryDeltas) == 0 {
		t.Fatalf("expected summary deltas, got none")
	}

	// Verify P95 delta
	p95Delta := report.SummaryDeltas[1]
	if p95Delta.MetricName != "P95 Latency" {
		t.Fatalf("expected metric name 'P95 Latency', got %s", p95Delta.MetricName)
	}
	if p95Delta.Severity != model.SeverityCritical {
		t.Fatalf("expected P95 delta severity to be CRITICAL, got %s", p95Delta.Severity)
	}
}

func TestCompareWindowsImprovement(t *testing.T) {
	now := time.Now()
	baseWin := model.TimeWindow{Start: now.Add(-2 * time.Hour), End: now.Add(-1 * time.Hour)}
	currWin := model.TimeWindow{Start: now.Add(-1 * time.Hour), End: now}

	baseReq := model.RequestMetric{
		Name:        "Overall",
		TotalCalls:  10000,
		FailedCalls: 20,
		ErrorRate:   0.20,
		Latency: model.LatencyPercentiles{
			P50: 100.0,
			P95: 300.0,
		},
	}

	// Post deploy with improved latency (-40%)
	currReq := model.RequestMetric{
		Name:        "Overall",
		TotalCalls:  10000,
		FailedCalls: 10,
		ErrorRate:   0.10,
		Latency: model.LatencyPercentiles{
			P50: 60.0,
			P95: 180.0,
		},
	}

	thresholds := DefaultThresholds()

	report := Compare(CompareOptions{
		AppName:        "test-app",
		BaselineWindow: baseWin,
		CurrentWindow:  currWin,
		BaseReqOverall: baseReq,
		CurrReqOverall: currReq,
		Thresholds:     thresholds,
	})

	if report.OverallVerdict != model.SeverityNone {
		t.Fatalf("expected overall verdict to be OK/None, got %s", report.OverallVerdict)
	}

	p95Delta := report.SummaryDeltas[1]
	if p95Delta.Severity != model.SeverityImprove {
		t.Fatalf("expected P95 delta severity to be IMPROVED, got %s", p95Delta.Severity)
	}
}

func TestCompareWindowsCorrelationAndNewErrors(t *testing.T) {
	now := time.Now()
	baseWin := model.TimeWindow{Start: now.Add(-2 * time.Hour), End: now.Add(-1 * time.Hour)}
	currWin := model.TimeWindow{Start: now.Add(-1 * time.Hour), End: now}

	baseReq := model.RequestMetric{Name: "Overall", TotalCalls: 1000, Latency: model.LatencyPercentiles{P95: 50.0}}
	currReq := model.RequestMetric{Name: "Overall", TotalCalls: 1000, Latency: model.LatencyPercentiles{P95: 120.0}}

	baseEndpoints := []model.RequestMetric{
		{Name: "POST /checkout", TotalCalls: 500, Latency: model.LatencyPercentiles{P95: 100.0}},
	}
	currEndpoints := []model.RequestMetric{
		{Name: "POST /checkout", TotalCalls: 500, Latency: model.LatencyPercentiles{P95: 350.0}},
	}

	baseDeps := []model.DependencyMetric{
		{Type: "SQL", Target: "sqldb", Name: "SELECT * FROM orders", Latency: model.LatencyPercentiles{P95: 50.0}},
	}
	currDeps := []model.DependencyMetric{
		{Type: "SQL", Target: "sqldb", Name: "SELECT * FROM orders", Latency: model.LatencyPercentiles{P95: 220.0}},
	}

	baseErrors := []model.ErrorSummary{}
	currErrors := []model.ErrorSummary{
		{Type: "PaymentGatewayTimeout", Message: "Timeout connecting to gateway", Count: 15, AffectedPaths: []string{"POST /checkout"}},
	}

	thresholds := DefaultThresholds()
	report := Compare(CompareOptions{
		AppName:        "shop",
		BaselineWindow: baseWin,
		CurrentWindow:  currWin,
		BaseReqOverall: baseReq,
		CurrReqOverall: currReq,
		BaseEndpoints:  baseEndpoints,
		CurrEndpoints:  currEndpoints,
		BaseDeps:       baseDeps,
		CurrDeps:       currDeps,
		BaseErrors:     baseErrors,
		CurrErrors:     currErrors,
		Thresholds:     thresholds,
	})

	if len(report.NewErrors) != 1 {
		t.Fatalf("expected 1 new error, got %d", len(report.NewErrors))
	}
	if len(report.RegressedDeps) != 1 {
		t.Fatalf("expected 1 regressed dependency, got %d", len(report.RegressedDeps))
	}
	if len(report.RootCauseHints) == 0 {
		t.Fatalf("expected root cause hints, got none")
	}
}

func TestCompareWithFanoutAndNewQueries(t *testing.T) {
	now := time.Now()
	baseWin := model.TimeWindow{Start: now.Add(-2 * time.Hour), End: now.Add(-1 * time.Hour)}
	currWin := model.TimeWindow{Start: now.Add(-1 * time.Hour), End: now}

	baseFanout := []model.FanoutMetric{
		{Endpoint: "GET /orders", AvgSQLCalls: 2.5, MaxSQLCalls: 4},
	}
	currFanout := []model.FanoutMetric{
		{Endpoint: "GET /orders", AvgSQLCalls: 38.0, MaxSQLCalls: 120},
	}

	baseDeps := []model.DependencyMetric{
		{Type: "SQL", Target: "db", Name: "SELECT * FROM users", Latency: model.LatencyPercentiles{P95: 20.0}},
	}
	currDeps := []model.DependencyMetric{
		{Type: "SQL", Target: "db", Name: "SELECT * FROM users", Latency: model.LatencyPercentiles{P95: 20.0}},
		{Type: "SQL", Target: "db", Name: "SELECT * FROM orders WHERE status = 'x'", Latency: model.LatencyPercentiles{P95: 850.0}, TotalCalls: 250},
	}

	opts := CompareOptions{
		AppName:        "test-app",
		BaselineWindow: baseWin,
		CurrentWindow:  currWin,
		BaseFanout:     baseFanout,
		CurrFanout:     currFanout,
		BaseDeps:       baseDeps,
		CurrDeps:       currDeps,
		Thresholds:     DefaultThresholds(),
	}

	report := Compare(opts)

	if len(report.FanoutDeltas) != 1 {
		t.Fatalf("expected 1 fanout delta, got %d", len(report.FanoutDeltas))
	}
	if report.FanoutDeltas[0].Severity != model.SeverityCritical {
		t.Fatalf("expected fanout severity CRITICAL, got %s", report.FanoutDeltas[0].Severity)
	}
	if len(report.NewDependencies) != 1 {
		t.Fatalf("expected 1 new dependency, got %d", len(report.NewDependencies))
	}
	if len(report.RootCauseHints) < 2 {
		t.Fatalf("expected at least 2 root cause hints, got %d", len(report.RootCauseHints))
	}
}
