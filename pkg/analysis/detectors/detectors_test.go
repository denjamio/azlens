package detectors_test

import (
	"testing"
	"time"

	"github.com/denjamio/azlens/pkg/analysis/detectors"
	"github.com/denjamio/azlens/pkg/domain"
	"github.com/denjamio/azlens/pkg/model"
)

func newTestSnapshot() *domain.Snapshot {
	now := time.Now()
	return domain.NewSnapshot(
		domain.ProfileContext{Name: "prod", DisplayName: "Production"},
		domain.Scope{Role: "checkout"},
		domain.WindowContext{
			Label:    "last 60m",
			Duration: "60m",
			Start:    now.Add(-60 * time.Minute),
			End:      now,
		},
	)
}

func TestHealthySnapshotEmitsNoFindings(t *testing.T) {
	snap := newTestSnapshot()
	snap.BaselineOverall = &model.RequestMetric{
		TotalCalls: 1000,
		ErrorRate:  0.2,
		Latency:    model.LatencyPercentiles{P95: 150.0},
	}
	snap.CurrentOverall = model.RequestMetric{
		TotalCalls: 1050,
		ErrorRate:  0.21,
		Latency:    model.LatencyPercentiles{P95: 152.0},
	}
	snap.BaselineEndpoints = []model.RequestMetric{
		{Name: "POST /checkout", TotalCalls: 500, ErrorRate: 0.1, Latency: model.LatencyPercentiles{P95: 200.0}},
	}
	snap.CurrentEndpoints = []model.RequestMetric{
		{Name: "POST /checkout", TotalCalls: 520, ErrorRate: 0.12, Latency: model.LatencyPercentiles{P95: 205.0}},
	}
	now := time.Now()
	snap.Freshness.RequestsLastSeen = &now

	registry := detectors.NewDefaultRegistry(detectors.DefaultConfig())
	findings := registry.Run(snap)

	if len(findings) != 0 {
		t.Fatalf("expected 0 findings for healthy snapshot, got %d: %+v", len(findings), findings)
	}
}

func TestRequestLatencyAndErrorRegression(t *testing.T) {
	snap := newTestSnapshot()
	snap.BaselineEndpoints = []model.RequestMetric{
		{Name: "POST /checkout", TotalCalls: 100, ErrorRate: 0.4, Latency: model.LatencyPercentiles{P95: 380.0}},
	}
	snap.CurrentEndpoints = []model.RequestMetric{
		{Name: "POST /checkout", TotalCalls: 100, ErrorRate: 3.7, Latency: model.LatencyPercentiles{P95: 1200.0}},
	}

	registry := detectors.NewDefaultRegistry(detectors.DefaultConfig())
	findings := registry.Run(snap)

	var hasLatency, hasError bool
	for _, f := range findings {
		if f.Kind == domain.FindingRequestLatencyRegression {
			hasLatency = true
		}
		if f.Kind == domain.FindingRequestErrorRegression {
			hasError = true
		}
	}

	if !hasLatency {
		t.Errorf("expected FindingRequestLatencyRegression")
	}
	if !hasError {
		t.Errorf("expected FindingRequestErrorRegression")
	}
}

func TestNewExceptionNoisePolicy(t *testing.T) {
	snap := newTestSnapshot()
	snap.CurrentOverall = model.RequestMetric{TotalCalls: 40000} // 40,000 total requests
	// Low-impact new exception: 12 occurrences (0.03% of total requests)
	snap.CurrentExceptions = []model.ErrorSummary{
		{
			Type:          "NoMethodError",
			Count:         12,
			AffectedPaths: []string{"POST /checkout"},
		},
	}

	registry := detectors.NewDefaultRegistry(detectors.DefaultConfig())
	findings := registry.Run(snap)

	if len(findings) != 1 {
		t.Fatalf("expected 1 finding, got %d", len(findings))
	}
	if findings[0].Severity != "LOW" {
		t.Errorf("expected LOW severity for 0.03%% traffic exception, got %s", findings[0].Severity)
	}
}

func TestAvailabilityFailureDetector(t *testing.T) {
	snap := newTestSnapshot()
	snap.Availability = []domain.AvailabilityMetric{
		{
			TestName:    "ping-check",
			TotalTests:  100,
			FailedTests: 15,
			SuccessRate: 85.0,
		},
	}

	registry := detectors.NewDefaultRegistry(detectors.DefaultConfig())
	findings := registry.Run(snap)

	var hasAvail bool
	for _, f := range findings {
		if f.Kind == domain.FindingAvailabilityFailure {
			hasAvail = true
		}
	}

	if !hasAvail {
		t.Errorf("expected FindingAvailabilityFailure")
	}
}

func TestTelemetryStaleDetector(t *testing.T) {
	snap := newTestSnapshot()
	staleTime := time.Now().Add(-25 * time.Minute)
	snap.Freshness.RequestsLastSeen = &staleTime

	detector := detectors.NewTelemetryStaleDetector(detectors.DefaultConfig())
	findings := detector.Detect(snap)

	if len(findings) != 1 {
		t.Fatalf("expected 1 finding for stale telemetry, got %d", len(findings))
	}
	if findings[0].Kind != domain.FindingTelemetryStale {
		t.Errorf("expected FindingTelemetryStale, got %v", findings[0].Kind)
	}
}

func TestMinimumSampleCallsNoisePolicy(t *testing.T) {
	snap := newTestSnapshot()
	// Only 2 calls in baseline and current
	snap.BaselineEndpoints = []model.RequestMetric{
		{Name: "GET /rare", TotalCalls: 2, ErrorRate: 0.0, Latency: model.LatencyPercentiles{P95: 50.0}},
	}
	snap.CurrentEndpoints = []model.RequestMetric{
		{Name: "GET /rare", TotalCalls: 2, ErrorRate: 50.0, Latency: model.LatencyPercentiles{P95: 500.0}},
	}

	cfg := detectors.DefaultConfig()
	cfg.MinSampleCalls = 5
	registry := detectors.NewDefaultRegistry(cfg)
	findings := registry.Run(snap)

	if len(findings) != 0 {
		t.Fatalf("expected 0 findings due to sample size < 5, got %d: %+v", len(findings), findings)
	}
}
