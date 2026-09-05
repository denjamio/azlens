package telemetry_test

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/denjamio/azlens/pkg/azure"
	"github.com/denjamio/azlens/pkg/config"
	"github.com/denjamio/azlens/pkg/domain"
	"github.com/denjamio/azlens/pkg/model"
	"github.com/denjamio/azlens/pkg/telemetry"
)

type mockClient struct {
	azure.AzureClient
	windowMetricsFn func(ctx context.Context, start, end time.Time, topN int) (model.WindowMetrics, error)
	slowLogsGroupFn func(ctx context.Context, start, end time.Time, dbName string, topN int) ([]model.SlowLogGroup, error)
}

func (m *mockClient) QueryWindowMetrics(ctx context.Context, start, end time.Time, topN int) (model.WindowMetrics, error) {
	if m.windowMetricsFn != nil {
		return m.windowMetricsFn(ctx, start, end, topN)
	}
	return model.WindowMetrics{}, nil
}

func (m *mockClient) QueryMySQLSlowLogsGrouped(ctx context.Context, start, end time.Time, dbName string, topN int) ([]model.SlowLogGroup, error) {
	if m.slowLogsGroupFn != nil {
		return m.slowLogsGroupFn(ctx, start, end, dbName, topN)
	}
	return nil, nil
}

func (m *mockClient) GetProfile() config.Profile {
	return config.Profile{}
}

func TestPopulateSnapshotMetrics(t *testing.T) {
	// Test nil safety
	telemetry.PopulateSnapshotMetrics(nil, nil, nil)

	snap := domain.NewSnapshot(
		domain.ProfileContext{Name: "test", DisplayName: "Test"},
		domain.Scope{Service: "svc"},
		domain.WindowContext{Label: "1h", Duration: "1h", Start: time.Now().Add(-time.Hour), End: time.Now()},
	)

	baseWM := &model.WindowMetrics{
		Overall:   model.RequestMetric{Name: "overall", TotalCalls: 100},
		Endpoints: []model.RequestMetric{{Name: "GET /api", TotalCalls: 80}},
		Deps:      []model.DependencyMetric{{Name: "db-query", TotalCalls: 120}},
		Errors:    []model.ErrorSummary{{Type: "TimeoutException", Count: 2}},
		Fanout:    []model.FanoutMetric{{Endpoint: "GET /api", AvgSQLCalls: 5.0}},
	}
	currWM := &model.WindowMetrics{
		Overall:   model.RequestMetric{Name: "overall", TotalCalls: 150},
		Endpoints: []model.RequestMetric{{Name: "GET /api", TotalCalls: 120}},
		Deps:      []model.DependencyMetric{{Name: "db-query", TotalCalls: 180}},
		Errors:    []model.ErrorSummary{{Type: "TimeoutException", Count: 5}},
		Fanout:    []model.FanoutMetric{{Endpoint: "GET /api", AvgSQLCalls: 8.0}},
	}

	telemetry.PopulateSnapshotMetrics(snap, baseWM, currWM)

	if snap.BaselineOverall == nil || snap.BaselineOverall.TotalCalls != 100 {
		t.Errorf("expected baseline overall total calls 100, got %+v", snap.BaselineOverall)
	}
	if snap.CurrentOverall.TotalCalls != 150 {
		t.Errorf("expected current overall total calls 150, got %d", snap.CurrentOverall.TotalCalls)
	}
	if len(snap.BaselineEndpoints) != 1 || len(snap.CurrentEndpoints) != 1 {
		t.Errorf("expected 1 endpoint each, got base=%d, curr=%d", len(snap.BaselineEndpoints), len(snap.CurrentEndpoints))
	}
	if len(snap.BaselineDependencies) != 1 || len(snap.CurrentDependencies) != 1 {
		t.Errorf("expected 1 dependency each, got base=%d, curr=%d", len(snap.BaselineDependencies), len(snap.CurrentDependencies))
	}
	if len(snap.BaselineExceptions) != 1 || len(snap.CurrentExceptions) != 1 {
		t.Errorf("expected 1 exception each, got base=%d, curr=%d", len(snap.BaselineExceptions), len(snap.CurrentExceptions))
	}
	if len(snap.BaselineFanout) != 1 || len(snap.CurrentFanout) != 1 {
		t.Errorf("expected 1 fanout metric each, got base=%d, curr=%d", len(snap.BaselineFanout), len(snap.CurrentFanout))
	}
}

func TestBuildSnapshotSuccess(t *testing.T) {
	mock := &mockClient{
		windowMetricsFn: func(ctx context.Context, start, end time.Time, topN int) (model.WindowMetrics, error) {
			return model.WindowMetrics{
				Overall: model.RequestMetric{TotalCalls: 200, FailedCalls: 2},
			}, nil
		},
		slowLogsGroupFn: func(ctx context.Context, start, end time.Time, dbName string, topN int) ([]model.SlowLogGroup, error) {
			return []model.SlowLogGroup{
				{Fingerprint: "SELECT 1", Executions: 10},
			}, nil
		},
	}

	b := telemetry.NewSnapshotBuilder(mock)
	prof := config.Profile{
		Name: "prod",
		Target: config.TargetConfig{
			Service: "order-service",
			Insights: config.InsightsConfig{
				Name: "ai-order",
			},
			Logs: config.LogsConfig{
				Database: "orders_db",
			},
		},
	}

	now := time.Now()
	start := now.Add(-time.Hour)
	end := now

	snap, err := b.BuildSnapshot(context.Background(), "prod", prof, start, end, "last 1h")
	if err != nil {
		t.Fatalf("expected BuildSnapshot to succeed, got: %v", err)
	}
	if snap == nil {
		t.Fatalf("expected non-nil snapshot")
	}
	if snap.CurrentOverall.TotalCalls != 200 {
		t.Errorf("expected 200 calls, got %d", snap.CurrentOverall.TotalCalls)
	}
	if len(snap.SlowLogs) != 1 {
		t.Errorf("expected 1 slow log group, got %d", len(snap.SlowLogs))
	}
	if !snap.ConfiguredCapabilities[domain.CapabilityRequests] {
		t.Errorf("expected CapabilityRequests configured")
	}
	if !snap.ConfiguredCapabilities[domain.CapabilityDatabaseSlowLogs] {
		t.Errorf("expected CapabilityDatabaseSlowLogs configured")
	}
}

func TestBuildSnapshotCurrentQueryError(t *testing.T) {
	mock := &mockClient{
		windowMetricsFn: func(ctx context.Context, start, end time.Time, topN int) (model.WindowMetrics, error) {
			if azure.IsBaseline(ctx) {
				return model.WindowMetrics{}, nil
			}
			return model.WindowMetrics{}, errors.New("kusto timeout")
		},
	}

	b := telemetry.NewSnapshotBuilder(mock)
	prof := config.Profile{}
	now := time.Now()

	snap, err := b.BuildSnapshot(context.Background(), "prod", prof, now.Add(-time.Hour), now, "1h")
	if err == nil {
		t.Fatalf("expected BuildSnapshot to fail on current query error, got nil")
	}
	if snap == nil {
		t.Fatalf("expected non-nil snapshot even with error")
	}
	if snap.QueryErrors[domain.CapabilityRequests] == nil {
		t.Errorf("expected QueryErrors to record CapabilityRequests error")
	}
}

func TestBuildSnapshot_RealFreshness(t *testing.T) {
	now := time.Now()
	start := now.Add(-time.Hour)
	end := now
	realLastSeen := now.Add(-12 * time.Minute)

	mock := &mockClient{
		windowMetricsFn: func(ctx context.Context, s, e time.Time, topN int) (model.WindowMetrics, error) {
			if azure.IsBaseline(ctx) {
				return model.WindowMetrics{}, nil
			}
			return model.WindowMetrics{
				Overall: model.RequestMetric{
					TotalCalls: 100,
					LastSeen:   realLastSeen,
				},
			}, nil
		},
	}

	b := telemetry.NewSnapshotBuilder(mock)
	prof := config.Profile{Target: config.TargetConfig{Service: "test-svc"}}

	snap, err := b.BuildSnapshot(context.Background(), "prod", prof, start, end, "1h")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if snap.Freshness.RequestsLastSeen == nil {
		t.Fatalf("expected RequestsLastSeen to be set")
	}
	if !snap.Freshness.RequestsLastSeen.Equal(realLastSeen) {
		t.Errorf("expected RequestsLastSeen %v, got %v (should NOT be window end %v)", realLastSeen, *snap.Freshness.RequestsLastSeen, end)
	}
}

