package azure

import (
	"context"
	"fmt"
	"os"
	"strings"
	"time"

	"github.com/denjamio/azlens/pkg/config"
	"github.com/denjamio/azlens/pkg/kql"
	"github.com/denjamio/azlens/pkg/model"
)

// MockClient provides deterministic simulated Azure telemetry for testing and
// offline demos. Both windows represent a healthy deployment: `deploy-check --mock`
// compares them and the quality gate exits 0, so the first contact with the CLI
// is a success. This is also the data source for `top` when --mock is set.
type MockClient struct {
	opts ClientOptions
}

func NewMockClient(opts ClientOptions) AzureClient {
	if opts.Profile.Name == "" {
		opts.Profile.Name = "Simulated Service (Demo)"
	}
	return &MockClient{opts: opts}
}

func (m *MockClient) GetProfile() config.Profile {
	return m.opts.Profile
}

func (m *MockClient) logQuery(tq kql.TargetQuery) {
	if m.opts.PrintQuery {
		fmt.Fprintf(os.Stderr, "\n[azlens:query] Backend: %s\n------------------------------------------------------------\n%s\n------------------------------------------------------------\n", tq.Backend, tq.Query)
	}
}

func (m *MockClient) QueryRequestsSummary(ctx context.Context, start, end time.Time) (model.RequestMetric, error) {
	m.logQuery(kql.BuildRequestsSummaryQuery(start, end, m.opts.Profile.Target))
	isBaseline := IsBaseline(ctx) || time.Since(end) > 10*time.Minute

	if isBaseline {
		return model.RequestMetric{
			Name:        m.opts.Profile.Name + " (Baseline)",
			TotalCalls:  45200,
			FailedCalls: 110,
			ErrorRate:   0.24,
			RPS:         12.5,
			HTTP2xx:     45090,
			HTTP4xx:     95,
			HTTP5xx:     15,
			Latency: model.LatencyPercentiles{
				Min: 12.0,
				Avg: 85.4,
				P50: 45.0,
				P75: 78.0,
				P90: 120.0,
				P95: 180.0,
				P99: 340.0,
				Max: 1250.0,
			},
		}, nil
	}

	return model.RequestMetric{
		Name:        m.opts.Profile.Name + " (Post-Deploy)",
		TotalCalls:  48100,
		FailedCalls: 148,
		ErrorRate:   0.31,
		RPS:         13.3,
		HTTP2xx:     47952,
		HTTP4xx:     100,
		HTTP5xx:     48,
		Latency: model.LatencyPercentiles{
			Min: 13.0,
			Avg: 88.6,
			P50: 47.0,
			P75: 81.0,
			P90: 126.0,
			P95: 192.0,
			P99: 352.0,
			Max: 1300.0,
		},
	}, nil
}

func (m *MockClient) QueryEndpoints(ctx context.Context, start, end time.Time, topN int) ([]model.RequestMetric, error) {
	m.logQuery(kql.BuildEndpointsSummaryQuery(start, end, m.opts.Profile.Target, topN))
	isBaseline := IsBaseline(ctx) || time.Since(end) > 10*time.Minute

	if isBaseline {
		return []model.RequestMetric{
			{
				Name:        "GET /api/v1/users/{id}",
				TotalCalls:  18500,
				FailedCalls: 12,
				ErrorRate:   0.06,
				Latency:     model.LatencyPercentiles{Avg: 42.0, P50: 25.0, P90: 55.0, P95: 75.0, P99: 140.0},
			},
			{
				Name:        "POST /api/v1/orders/checkout",
				TotalCalls:  8200,
				FailedCalls: 45,
				ErrorRate:   0.55,
				Latency:     model.LatencyPercentiles{Avg: 190.0, P50: 120.0, P90: 310.0, P95: 410.0, P99: 750.0},
			},
			{
				Name:        "GET /api/v1/catalog/search",
				TotalCalls:  12400,
				FailedCalls: 20,
				ErrorRate:   0.16,
				Latency:     model.LatencyPercentiles{Avg: 95.0, P50: 60.0, P90: 140.0, P95: 185.0, P99: 310.0},
			},
			{
				Name:        "GET /healthz",
				TotalCalls:  6100,
				FailedCalls: 0,
				ErrorRate:   0.0,
				Latency:     model.LatencyPercentiles{Avg: 4.0, P50: 2.0, P90: 5.0, P95: 8.0, P99: 15.0},
			},
		}, nil
	}

	return []model.RequestMetric{
		{
			Name:        "GET /api/v1/users/{id}",
			TotalCalls:  19200,
			FailedCalls: 14,
			ErrorRate:   0.07,
			Latency:     model.LatencyPercentiles{Avg: 41.0, P50: 24.0, P90: 54.0, P95: 76.0, P99: 138.0},
		},
		{
			Name:        "POST /api/v1/orders/checkout",
			TotalCalls:  9100,
			FailedCalls: 50,
			ErrorRate:   0.55,
			Latency:     model.LatencyPercentiles{Avg: 195.0, P50: 122.0, P90: 315.0, P95: 425.0, P99: 760.0},
		},
		{
			Name:        "GET /api/v1/catalog/search",
			TotalCalls:  13500,
			FailedCalls: 21,
			ErrorRate:   0.16,
			Latency:     model.LatencyPercentiles{Avg: 96.0, P50: 61.0, P90: 142.0, P95: 189.0, P99: 315.0},
		},
		{
			Name:        "GET /healthz",
			TotalCalls:  6300,
			FailedCalls: 0,
			ErrorRate:   0.0,
			Latency:     model.LatencyPercentiles{Avg: 4.0, P50: 2.0, P90: 5.0, P95: 8.0, P99: 15.0},
		},
	}, nil
}

func (m *MockClient) QuerySlowDependencies(ctx context.Context, start, end time.Time, depType string, topN int) ([]model.DependencyMetric, error) {
	m.logQuery(kql.BuildSlowDependenciesQuery(start, end, m.opts.Profile.Target, depType, topN))
	isBaseline := IsBaseline(ctx) || time.Since(end) > 10*time.Minute

	if isBaseline {
		return []model.DependencyMetric{
			{
				Type:        "SQL",
				Target:      "sqldb-orders-prod.database.windows.net",
				Name:        "SELECT * FROM Orders o JOIN Items i ON o.Id = i.OrderId WHERE o.CustomerId = @id",
				TotalCalls:  13800,
				FailedCalls: 4,
				ErrorRate:   0.03,
				Latency:     model.LatencyPercentiles{Avg: 75.0, P50: 45.0, P90: 95.0, P95: 120.0, P99: 240.0},
			},
			{
				Type:        "HTTP",
				Target:      "api.stripe.com",
				Name:        "POST /v1/payment_intents",
				TotalCalls:  7900,
				FailedCalls: 5,
				ErrorRate:   0.06,
				Latency:     model.LatencyPercentiles{Avg: 120.0, P50: 90.0, P90: 180.0, P95: 220.0, P99: 380.0},
			},
			{
				Type:        "Redis",
				Target:      "redis-cache-prod.redis.cache.windows.net:6380",
				Name:        "MGET session:*",
				TotalCalls:  82000,
				FailedCalls: 1,
				ErrorRate:   0.001,
				Latency:     model.LatencyPercentiles{Avg: 3.0, P50: 1.8, P90: 4.2, P95: 6.8, P99: 17.0},
			},
		}, nil
	}

	return []model.DependencyMetric{
		{
			Type:        "SQL",
			Target:      "sqldb-orders-prod.database.windows.net",
			Name:        "SELECT * FROM Orders o JOIN Items i ON o.Id = i.OrderId WHERE o.CustomerId = @id",
			TotalCalls:  14200,
			FailedCalls: 5,
			ErrorRate:   0.035,
			Latency:     model.LatencyPercentiles{Avg: 80.0, P50: 47.0, P90: 99.0, P95: 126.0, P99: 250.0},
		},
		{
			Type:        "HTTP",
			Target:      "api.stripe.com",
			Name:        "POST /v1/payment_intents",
			TotalCalls:  8100,
			FailedCalls: 6,
			ErrorRate:   0.074,
			Latency:     model.LatencyPercentiles{Avg: 124.0, P50: 93.0, P90: 186.0, P95: 229.0, P99: 392.0},
		},
		{
			Type:        "Redis",
			Target:      "redis-cache-prod.redis.cache.windows.net:6380",
			Name:        "MGET session:*",
			TotalCalls:  84000,
			FailedCalls: 1,
			ErrorRate:   0.001,
			Latency:     model.LatencyPercentiles{Avg: 3.1, P50: 1.8, P90: 4.3, P95: 7.0, P99: 17.5},
		},
	}, nil
}

func (m *MockClient) QueryExceptions(ctx context.Context, start, end time.Time, topN int) ([]model.ErrorSummary, error) {
	m.logQuery(kql.BuildExceptionsSummaryQuery(start, end, m.opts.Profile.Target, topN))
	now := time.Now()
	isBaseline := IsBaseline(ctx) || time.Since(end) > 10*time.Minute

	if isBaseline {
		return []model.ErrorSummary{
			{
				Type:          "Npgsql.NpgsqlException",
				Message:       "Connection pool timeout: no available connections in pool",
				Count:         45,
				FirstSeen:     now.Add(-6 * time.Hour),
				LastSeen:      now.Add(-1 * time.Hour),
				AffectedPaths: []string{"GET /api/v1/catalog/search"},
			},
		}, nil
	}

	return []model.ErrorSummary{
		{
			Type:          "Npgsql.NpgsqlException",
			Message:       "Connection pool timeout: no available connections in pool",
			Count:         52,
			FirstSeen:     now.Add(-3 * time.Hour),
			LastSeen:      now.Add(-8 * time.Minute),
			AffectedPaths: []string{"GET /api/v1/catalog/search"},
		},
	}, nil
}

func (m *MockClient) QueryFanout(ctx context.Context, start, end time.Time, topN int) ([]model.FanoutMetric, error) {
	m.logQuery(kql.BuildFanoutSummaryQuery(start, end, m.opts.Profile.Target, topN))
	isBaseline := IsBaseline(ctx) || time.Since(end) > 10*time.Minute

	if isBaseline {
		return []model.FanoutMetric{
			{
				Endpoint:              "GET /api/v1/orders",
				TotalRequests:         13800,
				AvgSQLCalls:           3.2,
				MaxSQLCalls:           6,
				AvgSQLDurationMs:      15.0,
				AvgEndpointDurationMs: 45.0,
			},
			{
				Endpoint:              "GET /api/v1/products/recommendations",
				TotalRequests:         28000,
				AvgSQLCalls:           17.8,
				MaxSQLCalls:           42,
				AvgSQLDurationMs:      92.0,
				AvgEndpointDurationMs: 135.0,
			},
			{
				Endpoint:              "POST /api/v1/checkout",
				TotalRequests:         7900,
				AvgSQLCalls:           7.9,
				MaxSQLCalls:           14,
				AvgSQLDurationMs:      42.0,
				AvgEndpointDurationMs: 460.0,
			},
			{
				Endpoint:              "GET /api/v1/users/profile",
				TotalRequests:         34500,
				AvgSQLCalls:           2.0,
				MaxSQLCalls:           4,
				AvgSQLDurationMs:      8.0,
				AvgEndpointDurationMs: 24.0,
			},
		}, nil
	}

	return []model.FanoutMetric{
		{
			Endpoint:              "GET /api/v1/orders",
			TotalRequests:         14200,
			AvgSQLCalls:           3.4,
			MaxSQLCalls:           7,
			AvgSQLDurationMs:      15.5,
			AvgEndpointDurationMs: 46.0,
		},
		{
			Endpoint:              "GET /api/v1/products/recommendations",
			TotalRequests:         28500,
			AvgSQLCalls:           18.0,
			MaxSQLCalls:           43,
			AvgSQLDurationMs:      93.0,
			AvgEndpointDurationMs: 137.0,
		},
		{
			Endpoint:              "POST /api/v1/checkout",
			TotalRequests:         8100,
			AvgSQLCalls:           8.1,
			MaxSQLCalls:           15,
			AvgSQLDurationMs:      43.0,
			AvgEndpointDurationMs: 465.0,
		},
		{
			Endpoint:              "GET /api/v1/users/profile",
			TotalRequests:         35000,
			AvgSQLCalls:           2.0,
			MaxSQLCalls:           4,
			AvgSQLDurationMs:      8.0,
			AvgEndpointDurationMs: 24.0,
		},
	}, nil
}

func (m *MockClient) QueryLatencyBreakdown(ctx context.Context, start, end time.Time, topN int) ([]model.LatencyBreakdown, error) {
	m.logQuery(kql.BuildLatencyBreakdownQuery(start, end, m.opts.Profile.Target, topN))
	return []model.LatencyBreakdown{
		{
			Endpoint:       "POST /api/v1/checkout",
			AvgDurationMs:  480.0,
			PctDatabase:    12.5,
			PctExternalAPI: 78.5,
			PctCache:       2.0,
			PctAppCode:     7.0,
		},
		{
			Endpoint:       "GET /api/v1/orders",
			AvgDurationMs:  320.0,
			PctDatabase:    82.0,
			PctExternalAPI: 0.0,
			PctCache:       4.5,
			PctAppCode:     13.5,
		},
		{
			Endpoint:       "GET /api/v1/catalog/search",
			AvgDurationMs:  85.0,
			PctDatabase:    25.0,
			PctExternalAPI: 0.0,
			PctCache:       65.0,
			PctAppCode:     10.0,
		},
	}, nil
}

func (m *MockClient) QueryDeprecations(ctx context.Context, start, end time.Time, topN int) ([]model.DeprecationSummary, error) {
	m.logQuery(kql.BuildDeprecationsQuery(start, end, m.opts.Profile.Target, topN))
	now := time.Now()
	return []model.DeprecationSummary{
		{
			Message:           "DEPRECATION WARNING: Calling `update_attributes` is deprecated and will be removed in Rails 7.2. Please use `update` instead.",
			Count:             1420,
			FirstSeen:         now.Add(-2 * time.Hour),
			LastSeen:          now.Add(-5 * time.Minute),
			AffectedEndpoints: []string{"POST /api/v1/orders", "PUT /api/v1/users/:id"},
		},
		{
			Message:           "DeprecationWarning: datetime.datetime.utcnow() is deprecated and scheduled for removal in Python 3.14. Use timezone-aware datetime.now(datetime.UTC).",
			Count:             890,
			FirstSeen:         now.Add(-3 * time.Hour),
			LastSeen:          now.Add(-2 * time.Minute),
			AffectedEndpoints: []string{"GET /api/v1/auth/token", "POST /api/v1/analytics"},
		},
		{
			Message:           "[DEP0040] DeprecationWarning: The `punycode` module is deprecated. Please use a userland alternative instead.",
			Count:             310,
			FirstSeen:         now.Add(-5 * time.Hour),
			LastSeen:          now.Add(-12 * time.Minute),
			AffectedEndpoints: []string{"POST /webhook/stripe"},
		},
	}, nil
}

// QueryWindowMetrics bundles the simulated telemetry for a full window
func (m *MockClient) QueryWindowMetrics(ctx context.Context, start, end time.Time, topN int) (model.WindowMetrics, error) {
	if m.opts.PrintQuery {
		batch := []kql.TargetQuery{
			kql.BuildRequestsSummaryQuery(start, end, m.opts.Profile.Target),
			kql.BuildEndpointsSummaryQuery(start, end, m.opts.Profile.Target, topN),
			kql.BuildSlowDependenciesQuery(start, end, m.opts.Profile.Target, "", topN),
			kql.BuildExceptionsSummaryQuery(start, end, m.opts.Profile.Target, topN),
			kql.BuildFanoutSummaryQuery(start, end, m.opts.Profile.Target, topN),
		}
		var sb strings.Builder
		for i, q := range batch {
			if i > 0 {
				sb.WriteString(";\n\n")
			}
			sb.WriteString(q.Query)
		}
		fmt.Fprintf(os.Stderr, "\n[azlens:batch-query] Backend: %s (%d statements)\n------------------------------------------------------------\n%s\n------------------------------------------------------------\n", batch[0].Backend, len(batch), sb.String())
	}

	overall, err := m.QueryRequestsSummary(ctx, start, end)
	if err != nil {
		return model.WindowMetrics{}, err
	}
	endpoints, err := m.QueryEndpoints(ctx, start, end, topN)
	if err != nil {
		return model.WindowMetrics{}, err
	}
	deps, err := m.QuerySlowDependencies(ctx, start, end, "", topN)
	if err != nil {
		return model.WindowMetrics{}, err
	}
	errs, err := m.QueryExceptions(ctx, start, end, topN)
	if err != nil {
		return model.WindowMetrics{}, err
	}
	fanout, err := m.QueryFanout(ctx, start, end, topN)
	if err != nil {
		return model.WindowMetrics{}, err
	}

	return model.WindowMetrics{
		Overall:   overall,
		Endpoints: endpoints,
		Deps:      deps,
		Errors:    errs,
		Fanout:    fanout,
	}, nil
}

func (m *MockClient) QueryMySQLSlowLogs(ctx context.Context, start, end time.Time, dbName string, slowest bool, topN int) (model.GenericQueryResult, error) {
	q := kql.BuildMySQLSlowLogsQuery(start, end, dbName, slowest, topN)
	m.logQuery(q)
	if slowest {
		return model.GenericQueryResult{
			Columns: []string{"TimeGenerated", "QueryDurationMs", "SqlText"},
			Rows: [][]interface{}{
				{"2026-09-02T20:15:22Z", 3840.5, "SELECT * FROM orders o JOIN order_items i ON o.id = i.order_id WHERE o.status = 'pending' FOR UPDATE"},
				{"2026-09-02T20:12:05Z", 2150.0, "SELECT count(*) FROM audit_logs WHERE created_at < NOW() - INTERVAL 90 DAY"},
				{"2026-09-02T20:08:44Z", 1890.2, "UPDATE inventory SET reserved_qty = reserved_qty + 1 WHERE sku = 'PROD-9981'"},
				{"2026-09-02T20:01:10Z", 1420.8, "SELECT * FROM users u LEFT JOIN payment_methods p ON u.id = p.user_id WHERE u.email = 'customer@test.com'"},
			},
		}, nil
	}
	return model.GenericQueryResult{
		Columns: []string{"SqlText", "Executions", "TotalDurationMs", "AvgDurationMs", "MaxDurationMs"},
		Rows: [][]interface{}{
			{"SELECT * FROM orders o JOIN order_items i ON o.id = i.order_id WHERE o.status = 'pending'", 1420, 245800.0, 173.1, 3840.5},
			{"SELECT count(*) FROM audit_logs WHERE created_at < NOW() - INTERVAL 90 DAY", 48, 103200.0, 2150.0, 2900.0},
			{"SELECT * FROM users u LEFT JOIN payment_methods p ON u.id = p.user_id WHERE u.id = ?", 18450, 94250.0, 5.1, 840.0},
			{"UPDATE inventory SET reserved_qty = reserved_qty + 1 WHERE sku = ?", 3100, 48200.0, 15.5, 1890.2},
		},
	}, nil
}
