package analysis_test

import (
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/denjamio/azlens/pkg/analysis"
	"github.com/denjamio/azlens/pkg/analysis/detectors"
	"github.com/denjamio/azlens/pkg/domain"
	"github.com/denjamio/azlens/pkg/model"
)

func newTestSnapshot(role string) *domain.Snapshot {
	now := time.Now()
	return domain.NewSnapshot(
		domain.ProfileContext{Name: "prod", DisplayName: "Production"},
		domain.Scope{Role: role},
		domain.WindowContext{
			Label:    "last 60m",
			Duration: "60m",
			Start:    now.Add(-60 * time.Minute),
			End:      now,
		},
	)
}

// Scenario A - Healthy environment
func TestScenarioA_HealthyEnvironment(t *testing.T) {
	snap := newTestSnapshot("checkout")
	now := time.Now()
	snap.Freshness.RequestsLastSeen = &now
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

	engine := analysis.NewEngine(detectors.DefaultConfig())
	res := engine.Analyze(snap)

	if res.State != domain.HealthStateHealthy {
		t.Fatalf("expected state %q, got %q", domain.HealthStateHealthy, res.State)
	}
	if len(res.Problems) != 0 {
		t.Fatalf("expected 0 problems, got %d", len(res.Problems))
	}
	if res.StatusMessage != "Everything looks normal." {
		t.Errorf("expected 'Everything looks normal.', got %q", res.StatusMessage)
	}
}

// Scenario B - Dependency causes endpoint regression
func TestScenarioB_DependencyCausesEndpointRegression(t *testing.T) {
	snap := newTestSnapshot("checkout")
	now := time.Now()
	snap.Freshness.RequestsLastSeen = &now

	// Checkout endpoint degraded
	snap.BaselineEndpoints = []model.RequestMetric{
		{Name: "POST /checkout", TotalCalls: 1000, ErrorRate: 0.4, Latency: model.LatencyPercentiles{P95: 380.0}},
	}
	snap.CurrentEndpoints = []model.RequestMetric{
		{Name: "POST /checkout", TotalCalls: 1000, ErrorRate: 3.7, Latency: model.LatencyPercentiles{P95: 1200.0}},
	}
	snap.CurrentOverall = model.RequestMetric{TotalCalls: 4000} // 25% traffic

	// Dependency payments-api regressed, SQL/Redis stable
	snap.BaselineDependencies = []model.DependencyMetric{
		{Type: "HTTP", Target: "payments-api", Name: "POST /pay", TotalCalls: 900, Latency: model.LatencyPercentiles{P95: 120.0}},
		{Type: "SQL", Target: "checkout-db", Name: "SELECT *", TotalCalls: 2000, Latency: model.LatencyPercentiles{P95: 15.0}},
	}
	snap.CurrentDependencies = []model.DependencyMetric{
		{Type: "HTTP", Target: "payments-api", Name: "POST /pay", TotalCalls: 910, Latency: model.LatencyPercentiles{P95: 910.0}},
		{Type: "SQL", Target: "checkout-db", Name: "SELECT *", TotalCalls: 2000, Latency: model.LatencyPercentiles{P95: 15.0}},
	}

	engine := analysis.NewEngine(detectors.DefaultConfig())
	res := engine.Analyze(snap)

	if res.State != domain.HealthStateDegraded {
		t.Fatalf("expected state degraded, got %s", res.State)
	}
	// Exactly one main problem! (Forbidden: separate top-level problems for checkout and payments-api)
	if len(res.Problems) != 1 {
		t.Fatalf("expected exactly 1 problem, got %d", len(res.Problems))
	}

	prob := res.Problems[0]
	if prob.Cause == nil {
		t.Fatalf("expected problem to have likely cause")
	}
	if prob.Cause.Summary != "payments-api" {
		t.Errorf("expected cause summary 'payments-api', got %q", prob.Cause.Summary)
	}
	if prob.Cause.Strength != domain.EvidenceStrengthStrong && prob.Cause.Strength != domain.EvidenceStrengthModerate {
		t.Errorf("expected strong or moderate cause strength, got %q", prob.Cause.Strength)
	}
	if prob.Action == nil {
		t.Errorf("expected recommended action")
	}
}

// Scenario C - New exception with negligible impact
func TestScenarioC_NewExceptionWithNegligibleImpact(t *testing.T) {
	snap := newTestSnapshot("checkout")
	now := time.Now()
	snap.Freshness.RequestsLastSeen = &now
	snap.CurrentOverall = model.RequestMetric{TotalCalls: 40000} // 40k total requests

	// New exception: 12 occurrences (0.03% of total requests)
	snap.CurrentExceptions = []model.ErrorSummary{
		{Type: "NoMethodError", Count: 12, AffectedPaths: []string{"POST /checkout"}},
	}

	engine := analysis.NewEngine(detectors.DefaultConfig())
	res := engine.Analyze(snap)

	// State remains healthy, exit code 0
	if res.State != domain.HealthStateHealthy {
		t.Fatalf("expected state healthy for negligible exception, got %s", res.State)
	}
	if len(res.Problems) != 0 {
		t.Fatalf("expected 0 problems in needs attention, got %d", len(res.Problems))
	}
	if len(res.Watching) != 1 {
		t.Fatalf("expected 1 item in worth watching, got %d", len(res.Watching))
	}
}

// Scenario D - New exception causes user failures
func TestScenarioD_NewExceptionCausesUserFailures(t *testing.T) {
	snap := newTestSnapshot("checkout")
	now := time.Now()
	snap.Freshness.RequestsLastSeen = &now

	snap.BaselineEndpoints = []model.RequestMetric{
		{Name: "POST /checkout", TotalCalls: 1000, ErrorRate: 0.5, Latency: model.LatencyPercentiles{P95: 200.0}},
	}
	snap.CurrentEndpoints = []model.RequestMetric{
		{Name: "POST /checkout", TotalCalls: 1000, ErrorRate: 7.5, Latency: model.LatencyPercentiles{P95: 210.0}},
	}
	snap.CurrentOverall = model.RequestMetric{TotalCalls: 2000}

	snap.CurrentExceptions = []model.ErrorSummary{
		{Type: "NoMethodError", Count: 80, AffectedPaths: []string{"POST /checkout"}},
	}

	engine := analysis.NewEngine(detectors.DefaultConfig())
	res := engine.Analyze(snap)

	if res.State != domain.HealthStateDegraded {
		t.Fatalf("expected degraded state, got %s", res.State)
	}
	if len(res.Problems) != 1 {
		t.Fatalf("expected 1 correlated problem, got %d", len(res.Problems))
	}
	prob := res.Problems[0]
	if prob.Cause == nil || prob.Cause.Summary != "NoMethodError" {
		t.Errorf("expected cause 'NoMethodError', got %+v", prob.Cause)
	}
}

// Scenario E - Availability failure (synthetic test degradation or 503 spike)
func TestScenarioE_AvailabilityFailure(t *testing.T) {
	snap := newTestSnapshot("backend")
	now := time.Now()
	snap.Freshness.RequestsLastSeen = &now
	snap.CurrentOverall.TotalCalls = 100
	snap.CurrentOverall.HTTP5xx = 20

	engine := analysis.NewEngine(detectors.DefaultConfig())
	res := engine.Analyze(snap)

	if res.State != domain.HealthStateDegraded {
		t.Fatalf("expected degraded state, got %s", res.State)
	}
	if len(res.Problems) != 1 {
		t.Fatalf("expected exactly 1 correlated availability problem, got %d", len(res.Problems))
	}
	prob := res.Problems[0]
	if prob.Kind != domain.ProblemKindAvailability {
		t.Errorf("expected ProblemKindAvailability, got %s", prob.Kind)
	}
}

// Scenario F - Telemetry disappears
func TestScenarioF_TelemetryDisappears(t *testing.T) {
	snap := newTestSnapshot("checkout")
	staleTime := time.Now().Add(-23 * time.Minute)
	snap.Freshness.RequestsLastSeen = &staleTime

	engine := analysis.NewEngine(detectors.DefaultConfig())
	res := engine.Analyze(snap)

	if res.State != domain.HealthStateUnknown {
		t.Fatalf("expected state unknown when telemetry stops, got %s", res.State)
	}

	// Check capability state is stale
	var reqStale bool
	for _, c := range res.Coverage {
		if c.Capability == domain.CapabilityRequests && c.State == domain.CapabilityStateStale {
			reqStale = true
		}
	}
	if !reqStale {
		t.Errorf("expected requests capability to be stale")
	}
}

// Scenario G - Slow logs capability coverage
func TestScenarioG_SlowLogsCoverage(t *testing.T) {
	snap := newTestSnapshot("checkout")
	now := time.Now()
	snap.Freshness.RequestsLastSeen = &now
	snap.CurrentOverall = model.RequestMetric{TotalCalls: 500, ErrorRate: 0.1, Latency: model.LatencyPercentiles{P95: 100.0}}

	snap.SlowLogs = []model.SlowLogGroup{
		{Fingerprint: "SELECT * FROM orders", Executions: 42},
	}

	engine := analysis.NewEngine(detectors.DefaultConfig())
	res := engine.Analyze(snap)

	if res.State != domain.HealthStateHealthy {
		t.Errorf("expected healthy state for application requests, got %s", res.State)
	}

	var slowLogAvailable bool
	for _, c := range res.Coverage {
		if c.Capability == domain.CapabilityDatabaseSlowLogs && c.State == domain.CapabilityStateAvailable {
			slowLogAvailable = true
		}
	}
	if !slowLogAvailable {
		t.Errorf("expected database_slow_logs to be marked available")
	}
}

// Scenario M - Query failure propagates reason in status message
func TestScenarioM_QueryFailurePropagatesReason(t *testing.T) {
	snap := newTestSnapshot("checkout")
	queryErr := errors.New("azure cli command failed: resource not found")
	snap.QueryErrors[domain.CapabilityRequests] = queryErr

	engine := analysis.NewEngine(detectors.DefaultConfig())
	res := engine.Analyze(snap)

	if res.State != domain.HealthStateUnknown {
		t.Fatalf("expected state unknown for query failure, got %s", res.State)
	}

	if !strings.Contains(res.StatusMessage, "azure cli command failed: resource not found") {
		t.Errorf("expected status message to contain the underlying query error, got: %q", res.StatusMessage)
	}
}

// Scenario N - Cross-service causal isolation: A regressed dependency on service B
// must NOT be attributed as the cause of an endpoint degradation on service A.
func TestCrossServiceCausalIsolation(t *testing.T) {
	snap := newTestSnapshot("order-service")
	now := time.Now()
	snap.Freshness.RequestsLastSeen = &now

	// Service A (order-service) endpoint degrades
	snap.BaselineEndpoints = []model.RequestMetric{
		{Name: "POST /orders", TotalCalls: 500, ErrorRate: 0.2, Latency: model.LatencyPercentiles{P95: 150.0}},
	}
	snap.CurrentEndpoints = []model.RequestMetric{
		{Name: "POST /orders", TotalCalls: 500, ErrorRate: 0.2, Latency: model.LatencyPercentiles{P95: 500.0}},
	}
	snap.CurrentOverall = model.RequestMetric{TotalCalls: 1000}

	// Service B has a regressed dependency
	correlator := analysis.NewCorrelator()
	findings := []domain.Finding{
		{
			Kind:        domain.FindingRequestLatencyRegression,
			Scope:       domain.Scope{Role: "order-service", Endpoint: "POST /orders"},
			Summary:     "POST /orders p95 latency increased by 233%",
			SampleCount: 500,
			Evidence: []domain.Evidence{
				{
					Signal:   "p95 latency",
					Current:  domain.Value{Val: 500.0, Unit: "ms", Text: "500ms"},
					Baseline: &domain.Value{Val: 150.0, Unit: "ms", Text: "150ms"},
					Change:   &domain.Change{Delta: 350.0, Pct: 233.0},
					Scope:    domain.Scope{Role: "order-service", Endpoint: "POST /orders"},
				},
			},
		},
		{
			Kind:        domain.FindingDependencyLatencyRegression,
			Scope:       domain.Scope{Role: "inventory-service", Target: "inventory-db"},
			Summary:     "SQL dependency 'inventory-db' p95 increased by 300%",
			SampleCount: 800,
			Evidence: []domain.Evidence{
				{
					Signal:   "dependency p95 latency",
					Current:  domain.Value{Val: 1200.0, Unit: "ms", Text: "1200ms"},
					Baseline: &domain.Value{Val: 300.0, Unit: "ms", Text: "300ms"},
					Change:   &domain.Change{Delta: 900.0, Pct: 300.0},
					Scope:    domain.Scope{Role: "inventory-service", Target: "inventory-db"},
				},
			},
		},
	}

	problems, _ := correlator.Correlate(snap, findings)

	// We expect 2 separate problems: order-service degradation and inventory-service dependency degradation
	if len(problems) != 2 {
		t.Fatalf("expected 2 isolated problems, got %d", len(problems))
	}

	// Verify order-service problem does NOT have inventory-db as Cause
	var orderProb *domain.Problem
	var depProb *domain.Problem
	for i := range problems {
		if problems[i].Scope.Endpoint == "POST /orders" {
			orderProb = &problems[i]
		} else if problems[i].Scope.Target == "inventory-db" {
			depProb = &problems[i]
		}
	}

	if orderProb == nil {
		t.Fatalf("missing order-service problem")
	}
	if orderProb.Cause != nil {
		t.Errorf("expected order-service problem to have nil Cause (cross-service isolation), but got: %+v", orderProb.Cause)
	}

	if depProb == nil {
		t.Fatalf("missing independent inventory-service dependency problem")
	}
	if depProb.Scope.Role != "inventory-service" {
		t.Errorf("expected inventory-service role, got %s", depProb.Scope.Role)
	}
}

func TestCorrelator_NoPanicOnDependencyWithoutBaselineChange(t *testing.T) {
	snap := newTestSnapshot("order-service")
	correlator := analysis.NewCorrelator()

	findings := []domain.Finding{
		{
			Kind:        domain.FindingRequestLatencyRegression,
			Scope:       domain.Scope{Role: "order-service", Endpoint: "POST /orders"},
			Severity:    "CRITICAL",
			SampleCount: 1000,
			Evidence: []domain.Evidence{
				{
					Signal:   "p95 latency",
					Current:  domain.Value{Val: 1500.0, Text: "1500ms"},
					Baseline: &domain.Value{Val: 200.0, Text: "200ms"},
					Change:   &domain.Change{Delta: 1300.0, Pct: 650.0},
					Scope:    domain.Scope{Role: "order-service", Endpoint: "POST /orders"},
				},
			},
		},
		{
			Kind:        domain.FindingDependencyErrorRegression,
			Scope:       domain.Scope{Role: "order-service", Target: "payment-gw"},
			Severity:    "CRITICAL",
			SampleCount: 500,
			Evidence: []domain.Evidence{
				{
					Signal:  "dependency error rate",
					Current: domain.Value{Val: 25.0, Text: "25.0%"},
					// Deliberately nil Change (no baseline)
					Change: nil,
					Scope:  domain.Scope{Role: "order-service", Target: "payment-gw"},
				},
			},
		},
	}

	// Must not panic!
	problems, _ := correlator.Correlate(snap, findings)
	if len(problems) != 1 {
		t.Fatalf("expected 1 correlated problem, got %d", len(problems))
	}
	if problems[0].Cause == nil {
		t.Fatalf("expected correlated cause, got nil")
	}
	if problems[0].Cause.Summary != "payment-gw" {
		t.Errorf("expected cause 'payment-gw', got %q", problems[0].Cause.Summary)
	}
}

func TestCorrelator_ExceptionRegressionMappedToWatching(t *testing.T) {
	snap := newTestSnapshot("order-service")
	correlator := analysis.NewCorrelator()

	findings := []domain.Finding{
		{
			Kind:        domain.FindingExceptionRegression,
			Scope:       domain.Scope{Role: "order-service", Endpoint: "GET /health"},
			Summary:     "Exception TimeoutException increased +150% (20 -> 50)",
			Severity:    "WARNING",
			SampleCount: 50,
		},
	}

	problems, watching := correlator.Correlate(snap, findings)
	if len(problems) != 0 {
		t.Errorf("expected 0 problems for orphan exception regression, got %d", len(problems))
	}
	if len(watching) != 1 {
		t.Fatalf("expected 1 watching item, got %d", len(watching))
	}
	if watching[0].Summary != "Exception TimeoutException increased +150% (20 -> 50)" {
		t.Errorf("unexpected watching summary: %s", watching[0].Summary)
	}
}

