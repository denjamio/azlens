package kql

import (
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/denjamio/azlens/pkg/config"
)

// TestValidateQueriesWithKqlGuard runs offline static analysis using Microsoft's
// official kql-guard AST parser (if installed). It validates syntax (KQL001)
// and ensures zero financially dangerous or unwindowed query shapes (cost score 0).
func TestValidateQueriesWithKqlGuard(t *testing.T) {
	kqlGuardPath, err := exec.LookPath("kql-guard")
	if err != nil {
		t.Skip("kql-guard not found in PATH; skipping offline KQL AST validation (run via docker compose or install kql-guard to enable)")
	}

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
		"latency_breakdown.kql":     BuildLatencyBreakdownQuery(start, end, target, 15),
		"deprecations.kql":          BuildDeprecationsQuery(start, end, target, 15),
		"mysql_slow_logs.kql":       BuildMySQLSlowLogsQuery(start, end, "ecommerce_db", 15),
		"mysql_slow_logs_group.kql": BuildMySQLSlowLogsGroupedQuery(start, end, "ecommerce_db", 15),
	}

	tempDir := t.TempDir()

	for name, tq := range queries {
		t.Run(name, func(t *testing.T) {
			if tq.Err != nil {
				t.Fatalf("failed to build %s query: %v", name, tq.Err)
			}
			filePath := filepath.Join(tempDir, name)
			if err := os.WriteFile(filePath, []byte(tq.Query), 0600); err != nil {
				t.Fatalf("failed to write %s: %v", name, err)
			}

			// Run kql-guard with strict mode (exit 1 on any error or cost warning)
			cmd := exec.Command(kqlGuardPath, filePath, "--strict")
			out, err := cmd.CombinedOutput()
			outStr := string(out)

			if err != nil {
				t.Fatalf("kql-guard flagged query %s:\n%s\nQuery:\n%s", name, outStr, tq.Query)
			}

			if !strings.Contains(outStr, "cost score 0") {
				t.Errorf("expected cost score 0 for %s, got output:\n%s", name, outStr)
			}
		})
	}
}
