package azure

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/denjamio/azlens/pkg/config"
)

const fakeAzOutput = `{"tables":[{"name":"PrimaryResult","columns":[{"name":"ok","type":"string"}],"rows":[["1"]]}]}`

// fakeAzScript builds a shell script that emulates the `az` CLI: it appends its
// arguments to a capture file, then prints a valid query result so the client
// can parse it.
func fakeAzScript(t *testing.T, capturePath string) (binDir string) {
	t.Helper()
	dir := t.TempDir()
	script := "#!/bin/sh\n" +
		"echo \"$@\" >> " + capturePath + "\n" +
		"echo \"azprofile=$AZURE_CONFIG_DIR\" >> " + capturePath + "\n" +
		"echo '" + fakeAzOutput + "'\n"
	path := filepath.Join(dir, "az")
	if err := os.WriteFile(path, []byte(script), 0755); err != nil {
		t.Fatalf("failed writing fake az script: %v", err)
	}
	return dir
}

// readCapture returns the captured az invocations (args) as a single string
func readCapture(t *testing.T, capturePath string) string {
	t.Helper()
	data, err := os.ReadFile(capturePath)
	if err != nil {
		t.Fatalf("failed reading capture file: %v", err)
	}
	return string(data)
}

// TestExecuteKQLRoutingMultiSubscription verifies that each query is routed to
// the right backend (App Insights vs Log Analytics) with its own subscription
func TestExecuteKQLRoutingMultiSubscription(t *testing.T) {
	prof := config.Profile{
		Name: "Routing Test",
		Target: config.TargetConfig{
			Insights: config.InsightsConfig{
				Name:           "app-shared-pro",
				DirectoryID:    "dir-insights",
				SubscriptionID: "sub-insights",
			},
			Logs: config.LogsConfig{
				WorkspaceID:    "33333333-hhhh-iiii-jjjj-333333333333",
				DirectoryID:    "dir-logs",
				SubscriptionID: "sub-logs",
			},
		},
	}

	capture := filepath.Join(t.TempDir(), "args.txt")
	t.Setenv("PATH", fakeAzScript(t, capture)+string(os.PathListSeparator)+os.Getenv("PATH"))
	t.Setenv("AZLENS_TEST_CAPTURE", capture)

	client := &AzCliClient{opts: ClientOptions{Profile: prof}}
	ctx := context.Background()
	now := time.Now()
	start := now.Add(-1 * time.Hour)
	end := now

	// 1. Log Analytics table query sets context and routes to workspace with logs subscription
	if _, err := client.QueryMySQLSlowLogs(ctx, start, end, "testdb", false, 5); err != nil {
		t.Fatalf("failed querying log analytics: %v", err)
	}
	cap := readCapture(t, capture)
	for _, want := range []string{
		"account set --subscription sub-logs --tenant dir-logs",
		"monitor log-analytics query",
		"--workspace 33333333-hhhh-iiii-jjjj-333333333333",
		"--subscription sub-logs",
	} {
		if !strings.Contains(cap, want) {
			t.Errorf("log analytics query missing %q in captured args:\n%s", want, cap)
		}
	}

	// 2. App Insights query sets context and routes to component with insights subscription
	_ = os.Remove(capture)
	if _, err := client.QueryRequestsSummary(ctx, start, end); err != nil {
		t.Fatalf("failed querying app insights: %v", err)
	}
	cap = readCapture(t, capture)
	for _, want := range []string{
		"account set --subscription sub-insights --tenant dir-insights",
		"monitor app-insights query",
		"--app app-shared-pro",
		"--subscription sub-insights",
	} {
		if !strings.Contains(cap, want) {
			t.Errorf("app insights query missing %q in captured args:\n%s", want, cap)
		}
	}
}

// TestExecuteKQLRoutingFallbacks covers degraded profiles (single backend, no subscriptions)
func TestExecuteKQLRoutingFallbacks(t *testing.T) {
	ctx := context.Background()
	now := time.Now()
	start := now.Add(-1 * time.Hour)
	end := now

	// 1. Profile with only Log Analytics: everything routes to the workspace
	profLogsOnly := config.Profile{
		Name: "Logs Only",
		Target: config.TargetConfig{
			Logs: config.LogsConfig{WorkspaceID: "workspace-guid"},
		},
	}
	capture := filepath.Join(t.TempDir(), "args.txt")
	t.Setenv("PATH", fakeAzScript(t, capture)+string(os.PathListSeparator)+os.Getenv("PATH"))
	t.Setenv("AZLENS_TEST_CAPTURE", capture)

	client := &AzCliClient{opts: ClientOptions{Profile: profLogsOnly}}
	if _, err := client.QueryRequestsSummary(ctx, start, end); err != nil {
		t.Fatalf("failed querying with logs-only profile: %v", err)
	}
	cap := readCapture(t, capture)
	if !strings.Contains(cap, "monitor log-analytics query") || !strings.Contains(cap, "--workspace workspace-guid") {
		t.Errorf("expected logs-only profile to route to log analytics, got:\n%s", cap)
	}
	if strings.Contains(cap, "--subscription") {
		t.Errorf("expected no --subscription flag when none is configured, got:\n%s", cap)
	}

	// 2. Profile with neither backend configured must fail fast
	emptyClient := &AzCliClient{opts: ClientOptions{Profile: config.Profile{Name: "Empty"}}}
	if _, err := emptyClient.QueryRequestsSummary(ctx, start, end); err == nil {
		t.Errorf("expected error when neither insights.name nor logs.workspace_id are configured")
	}
}

func TestExecuteKQLPrintQuery(t *testing.T) {
	prof := config.Profile{
		Target: config.TargetConfig{
			Insights: config.InsightsConfig{Name: "app-shared-pro"},
		},
	}
	capture := filepath.Join(t.TempDir(), "args.txt")
	t.Setenv("PATH", fakeAzScript(t, capture)+string(os.PathListSeparator)+os.Getenv("PATH"))
	t.Setenv("AZLENS_TEST_CAPTURE", capture)

	now := time.Now()
	client := &AzCliClient{opts: ClientOptions{Profile: prof, PrintQuery: true}}
	_, err := client.QueryRequestsSummary(context.Background(), now.Add(-1*time.Hour), now)
	if err != nil {
		t.Fatalf("expected query to succeed with PrintQuery enabled, got: %v", err)
	}
}
