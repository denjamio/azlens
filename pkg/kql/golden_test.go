package kql

import (
	"flag"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/denjamio/azlens/pkg/config"
)

var updateGolden = flag.Bool("update", false, "update golden fixtures")

func TestGoldenQueries(t *testing.T) {
	start := time.Date(2026, 9, 1, 0, 0, 0, 0, time.UTC)
	end := time.Date(2026, 9, 2, 0, 0, 0, 0, time.UTC)
	target := config.TargetConfig{
		RoleName: "order-service",
		Logs:     config.LogsConfig{Database: "ecommerce_db"},
	}

	queries := map[string]TargetQuery{
		"requests_summary.kql":      BuildRequestsSummaryQuery(start, end, target),
		"endpoints_summary.kql":     BuildEndpointsSummaryQuery(start, end, target, 15),
		"dependencies_sql.kql":      BuildSlowDependenciesQuery(start, end, target, "SQL", 15),
		"dependencies_http.kql":     BuildSlowDependenciesQuery(start, end, target, "HTTP", 15),
		"dependencies_redis.kql":    BuildSlowDependenciesQuery(start, end, target, "REDIS", 15),
		"dependencies_all.kql":      BuildSlowDependenciesQuery(start, end, target, "", 15),
		"exceptions_summary.kql":    BuildExceptionsSummaryQuery(start, end, target, 15),
		"fanout_summary.kql":        BuildFanoutSummaryQuery(start, end, target, 15),
		"nplusone_candidate.kql":    BuildNPlusOneCandidateQuery(start, end, target, 15),
		"latency_breakdown.kql":     BuildLatencyBreakdownQuery(start, end, target, 15),
		"deprecations.kql":          BuildDeprecationsQuery(start, end, target, 15),
		"mysql_slow_logs.kql":       BuildMySQLSlowLogsQuery(start, end, "ecommerce_db", 15),
		"mysql_slow_logs_group.kql": BuildMySQLSlowLogsGroupedQuery(start, end, "ecommerce_db", 15),
	}

	goldenDir := filepath.Join("testdata", "golden")

	for name, tq := range queries {
		t.Run(name, func(t *testing.T) {
			if tq.Err != nil {
				t.Fatalf("unexpected error building query %s: %v", name, tq.Err)
			}
			goldenPath := filepath.Join(goldenDir, name)

			if *updateGolden {
				if err := os.WriteFile(goldenPath, []byte(tq.Query), 0644); err != nil {
					t.Fatalf("failed to update golden file %s: %v", name, err)
				}
				return
			}

			expectedBytes, err := os.ReadFile(goldenPath)
			if err != nil {
				t.Fatalf("failed reading golden fixture %s: %v (run with -update to generate)", goldenPath, err)
			}
			expected := string(expectedBytes)

			if tq.Query != expected {
				t.Errorf("query %s drifted from golden fixture %s!\n--- Expected ---\n%s\n--- Actual ---\n%s",
					name, goldenPath, expected, tq.Query)
			}
		})
	}
}
