package reporter

import (
	"bytes"
	"strings"
	"testing"
	"time"

	"github.com/denjamio/azlens/pkg/model"
)

func TestPrintRequestsTable(t *testing.T) {
	reqs := []model.RequestMetric{
		{
			Name:        "GET /orders",
			TotalCalls:  1200,
			FailedCalls: 12,
			ErrorRate:   1.0,
			Latency: model.LatencyPercentiles{
				Avg: 45.0,
				P50: 30.0,
				P90: 80.0,
				P95: 120.0,
				P99: 250.0,
			},
		},
	}
	var buf bytes.Buffer
	PrintRequestsTable(&buf, reqs)
	out := buf.String()
	if !strings.Contains(out, "GET /orders") {
		t.Errorf("expected endpoint name in output, got: %s", out)
	}
	if !strings.Contains(out, "1,200") {
		t.Errorf("expected formatted count, got: %s", out)
	}
	if !strings.Contains(out, "120ms") {
		t.Errorf("expected formatted p95, got: %s", out)
	}
}

func TestPrintDependenciesTable(t *testing.T) {
	deps := []model.DependencyMetric{
		{
			Type:        "SQL",
			Target:      "order-db.postgres.database.azure.com",
			Name:        "SELECT * FROM orders",
			TotalCalls:  5000,
			FailedCalls: 5,
			ErrorRate:   0.1,
			Latency: model.LatencyPercentiles{
				Avg: 12.0,
				P50: 8.0,
				P90: 25.0,
				P95: 45.0,
				P99: 110.0,
			},
		},
	}
	var buf bytes.Buffer
	PrintDependenciesTable(&buf, deps)
	out := buf.String()
	if !strings.Contains(out, "SQL") || !strings.Contains(out, "order-db") {
		t.Errorf("expected SQL target in output, got: %s", out)
	}
	if !strings.Contains(out, "5,000") {
		t.Errorf("expected formatted count, got: %s", out)
	}
}

func TestPrintErrorsTable(t *testing.T) {
	errors := []model.ErrorSummary{
		{
			Type:          "SqlException",
			Message:       "connection timeout",
			Count:         42,
			FirstSeen:     time.Now().Add(-time.Hour),
			LastSeen:      time.Now(),
			AffectedPaths: []string{"POST /checkout", "GET /cart"},
		},
	}
	var buf bytes.Buffer
	PrintErrorsTable(&buf, errors)
	out := buf.String()
	if !strings.Contains(out, "SqlException") || !strings.Contains(out, "connection timeout") {
		t.Errorf("expected error summary in output, got: %s", out)
	}
	if !strings.Contains(out, "42") {
		t.Errorf("expected error count in output, got: %s", out)
	}
}

func TestPrintFanoutTable(t *testing.T) {
	fanout := []model.FanoutMetric{
		{
			Endpoint:              "GET /api/catalog",
			TotalRequests:         350,
			AvgSQLCalls:           12.4,
			MaxSQLCalls:           45,
			P50Calls:              10.0,
			P90Calls:              18.0,
			P95Calls:              24.0,
			P99Calls:              40.0,
			AvgSQLDurationMs:      120.0,
			AvgEndpointDurationMs: 340.0,
		},
	}
	var buf bytes.Buffer
	PrintFanoutTable(&buf, fanout)
	out := buf.String()
	if !strings.Contains(out, "GET /api/catalog") {
		t.Errorf("expected endpoint in fanout output, got: %s", out)
	}
	if !strings.Contains(out, "12.4") {
		t.Errorf("expected avg SQL calls in output, got: %s", out)
	}
}

func TestPrintLatencyBreakdownTable(t *testing.T) {
	hasOverlap := true
	breakdown := []model.LatencyBreakdown{
		{
			Endpoint:       "POST /checkout",
			AvgDurationMs:  450.0,
			PctDatabase:    65.0,
			PctExternalAPI: 15.0,
			PctCache:       5.0,
			PctResidual:    15.0,
			HasOverlap:     hasOverlap,
		},
	}
	var buf bytes.Buffer
	PrintLatencyBreakdownTable(&buf, breakdown)
	out := buf.String()
	if !strings.Contains(out, "POST /checkout") {
		t.Errorf("expected endpoint in breakdown output, got: %s", out)
	}
	if !strings.Contains(out, "65.0%") {
		t.Errorf("expected percentage in output, got: %s", out)
	}
}
