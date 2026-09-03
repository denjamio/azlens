package azure

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/denjamio/azlens/pkg/config"
	"github.com/denjamio/azlens/pkg/kql"
)

// fakeAzBatchScript builds an az replacement that captures its invocation and
// returns a multi-table JSON payload (one table per batched statement)
func fakeAzBatchScript(t *testing.T, capturePath string) string {
	t.Helper()
	dir := t.TempDir()
	payload := `{"tables":[` +
		`{"name":"PrimaryResult","columns":[{"name":"c0","type":"real"}],"rows":[[100,2,50,40,200,60,80,120,180,300,1.2,98,1,1]]},` +
		`{"name":"PrimaryResult","columns":[{"name":"c0","type":"string"}],"rows":[["GET /x",1000,5,40,20,150,50,70,90,120,250,0.5]]},` +
		`{"name":"PrimaryResult","columns":[{"name":"c0","type":"string"}],"rows":[["SQL","db.host","SELECT 1",500,1,120,60,400,80,150,200,300,0.2]]},` +
		`{"name":"PrimaryResult","columns":[{"name":"c0","type":"string"}],"rows":[["System.Err","boom",7,"2026-09-03T10:00:00Z","2026-09-03T11:00:00Z"]]},` +
		`{"name":"PrimaryResult","columns":[{"name":"c0","type":"real"}],"rows":[["GET /x",1000,42.5,128,185.0,320.0]]}` +
		`]}`
	script := "#!/bin/sh\n" +
		"echo \"=== INVOCATION ===\" >> " + capturePath + "\n" +
		"echo \"$@\" >> " + capturePath + "\n" +
		"echo '" + payload + "'\n"
	path := filepath.Join(dir, "az")
	if err := os.WriteFile(path, []byte(script), 0755); err != nil {
		t.Fatalf("failed writing fake az script: %v", err)
	}
	return dir
}

// TestQueryWindowMetricsBatched verifies that the full window telemetry is fetched
// with a SINGLE az invocation (5 statements batched) and parsed into the right metrics
func TestQueryWindowMetricsBatched(t *testing.T) {
	prof := config.Profile{
		Name: "Batch Test",
		Target: config.TargetConfig{
			Insights: config.InsightsConfig{
				Name:         "app-shared-pro",
				Subscription: "sub-insights",
			},
			Logs: config.LogsConfig{WorkspaceID: "workspace-guid"},
		},
	}

	capture := filepath.Join(t.TempDir(), "args.txt")
	t.Setenv("PATH", fakeAzBatchScript(t, capture)+string(os.PathListSeparator)+os.Getenv("PATH"))
	t.Setenv("AZLENS_TEST_CAPTURE", capture)

	client := &AzCliClient{opts: ClientOptions{Profile: prof}}
	now := time.Now()

	wm, err := client.QueryWindowMetrics(context.Background(), now.Add(-time.Hour), now, 30)
	if err != nil {
		t.Fatalf("failed querying window metrics: %v", err)
	}

	// 1. Overall summary parsed from table 1
	if wm.Overall.TotalCalls != 100 || wm.Overall.Latency.P95 != 180 || wm.Overall.ErrorRate != 1.2 {
		t.Errorf("overall summary mismatch: %+v", wm.Overall)
	}
	// 2. Endpoints parsed from table 2
	if len(wm.Endpoints) != 1 || wm.Endpoints[0].Name != "GET /x" || wm.Endpoints[0].Latency.P95 != 120 {
		t.Errorf("endpoints mismatch: %+v", wm.Endpoints)
	}
	// 3. Dependencies parsed from table 3
	if len(wm.Deps) != 1 || wm.Deps[0].Type != "SQL" || wm.Deps[0].TotalCalls != 500 {
		t.Errorf("dependencies mismatch: %+v", wm.Deps)
	}
	// 4. Exceptions parsed from table 4
	if len(wm.Errors) != 1 || wm.Errors[0].Count != 7 {
		t.Errorf("exceptions mismatch: %+v", wm.Errors)
	}
	// 5. Fan-out parsed from table 5
	if len(wm.Fanout) != 1 || wm.Fanout[0].AvgSQLCalls != 42.5 || wm.Fanout[0].MaxSQLCalls != 128 {
		t.Errorf("fanout mismatch: %+v", wm.Fanout)
	}

	// 6. Single az invocation with semicolon-separated statements and correct routing
	cap := readCapture(t, capture)
	if invocations := strings.Count(cap, "=== INVOCATION ==="); invocations != 1 {
		t.Errorf("expected exactly one az invocation, got %d:\n%s", invocations, cap)
	}
	if !strings.Contains(cap, ";") {
		t.Errorf("expected semicolon-separated batch query, got:\n%s", cap)
	}
	for _, want := range []string{
		"monitor app-insights query",
		"--app app-shared-pro",
		"--subscription sub-insights",
	} {
		if !strings.Contains(cap, want) {
			t.Errorf("batch query missing %q in captured args:\n%s", want, cap)
		}
	}
}

// TestExecuteKQLBatchMixedBackendRejected verifies batches never cross backends
func TestExecuteKQLBatchMixedBackendRejected(t *testing.T) {
	prof := config.Profile{
		Target: config.TargetConfig{
			Insights: config.InsightsConfig{Name: "app-shared-pro"},
			Logs:     config.LogsConfig{WorkspaceID: "workspace-guid"},
		},
	}

	capture := filepath.Join(t.TempDir(), "args.txt")
	t.Setenv("PATH", fakeAzBatchScript(t, capture)+string(os.PathListSeparator)+os.Getenv("PATH"))
	t.Setenv("AZLENS_TEST_CAPTURE", capture)

	client := &AzCliClient{opts: ClientOptions{Profile: prof}}
	_, err := client.executeKQLBatch(context.Background(), []kql.TargetQuery{
		{Query: "requests | take 5", Backend: kql.BackendAppInsights},
		{Query: "MySqlSlowLogs | take 5", Backend: kql.BackendLogAnalytics},
	})
	if err == nil || !strings.Contains(err.Error(), "mixed backend") {
		t.Fatalf("expected mixed backend rejection error, got: %v", err)
	}
	if _, statErr := os.Stat(capture); statErr == nil {
		t.Errorf("no az invocation should happen for a rejected batch")
	}
}
