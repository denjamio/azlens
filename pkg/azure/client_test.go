package azure

import (
	"context"
	"testing"
	"time"

	"github.com/denjamio/azlens/pkg/config"
)

func TestMockClientOperations(t *testing.T) {
	ctx := context.Background()
	prof := config.Profile{
		Name: "Test Profile",
	}

	client := NewClient(ClientOptions{
		Profile: prof,
		IsMock:  true,
	})

	now := time.Now()
	start := now.Add(-1 * time.Hour)
	end := now

	// 1. Window Metrics
	wm, err := client.QueryWindowMetrics(ctx, start, end, 10)
	if err != nil {
		t.Fatalf("failed querying window metrics: %v", err)
	}
	if wm.Overall.TotalCalls == 0 {
		t.Errorf("expected non-zero total calls")
	}

	// 2. Endpoints
	endpoints, err := client.QueryEndpoints(ctx, start, end, 10)
	if err != nil {
		t.Fatalf("failed querying endpoints: %v", err)
	}
	if len(endpoints) == 0 {
		t.Errorf("expected endpoints, got none")
	}

	// 3. Dependencies
	deps, err := client.QuerySlowDependencies(ctx, start, end, "SQL", 10)
	if err != nil {
		t.Fatalf("failed querying dependencies: %v", err)
	}
	if len(deps) == 0 {
		t.Errorf("expected dependencies, got none")
	}

	// 4. Exceptions
	errs, err := client.QueryExceptions(ctx, start, end, 10)
	if err != nil {
		t.Fatalf("failed querying exceptions: %v", err)
	}
	if len(errs) == 0 {
		t.Errorf("expected exceptions, got none")
	}

	// 5. MySQL Slow Logs
	mysqlLogs, err := client.QueryMySQLSlowLogs(ctx, start, end, "backend_ror", false, 10)
	if err != nil {
		t.Fatalf("failed querying mysql slow logs: %v", err)
	}
	if len(mysqlLogs.Columns) == 0 || len(mysqlLogs.Rows) == 0 {
		t.Errorf("expected mysql slow logs rows and columns")
	}
}
