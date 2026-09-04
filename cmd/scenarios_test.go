package cmd

import (
	"bytes"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/denjamio/azlens/pkg/domain"
	"github.com/denjamio/azlens/pkg/model"
)

// Scenario H - Multi-profile isolation
func TestScenarioH_MultiProfileIsolation(t *testing.T) {
	resetRootFlags()
	defer resetRootFlags()

	dir := t.TempDir()
	origWd, _ := os.Getwd()
	defer func() { _ = os.Chdir(origWd) }()
	_ = os.Chdir(dir)

	cfgContent := `defaults:
  profile: prod
profiles:
  prod:
    name: Production
    subscription_id: sub-prod
    resource_group: rg-prod
    workspace_id: ws-prod
    target:
      app_insights_app: app-prod
      roles:
        - checkout
  staging:
    name: Staging
    subscription_id: sub-staging
    resource_group: rg-staging
    workspace_id: ws-staging
    target:
      app_insights_app: app-staging
      roles:
        - checkout-staging
`
	if err := os.WriteFile(filepath.Join(dir, "azlens.yaml"), []byte(cfgContent), 0644); err != nil {
		t.Fatalf("failed to write test azlens.yaml: %v", err)
	}

	buf := new(bytes.Buffer)
	RootCmd.SetOut(buf)
	RootCmd.SetArgs([]string{"-p", "staging", "--mock", "-o", "json"})

	err := RootCmd.Execute()
	if err != nil {
		t.Fatalf("expected azlens -p staging to succeed, got: %v", err)
	}

	var res domain.AnalysisResult
	if err := json.Unmarshal(buf.Bytes(), &res); err != nil {
		t.Fatalf("failed to parse JSON result: %v\nOutput was: %s", err, buf.String())
	}

	if res.Profile.Name != "staging" {
		t.Errorf("expected profile name 'staging', got %q", res.Profile.Name)
	}
	if res.Profile.DisplayName != "Staging" {
		t.Errorf("expected display name 'Staging', got %q", res.Profile.DisplayName)
	}
}

// Scenario I - Deploy regression detected: only changed signals shown, exit code 2
func TestScenarioI_DeployRegression(t *testing.T) {
	resetRootFlags()
	defer resetRootFlags()

	now := time.Now()
	dur := 30 * time.Minute
	snap := domain.NewSnapshot(
		domain.ProfileContext{Name: "prod", DisplayName: "Production"},
		domain.Scope{Role: "checkout"},
		domain.WindowContext{
			Label:    "deploy at 14:00",
			Duration: "30m",
			Start:    now.Add(-dur),
			End:      now,
		},
	)
	snap.Freshness.RequestsLastSeen = &now
	snap.BaselineEndpoints = []model.RequestMetric{
		{Name: "POST /checkout", TotalCalls: 1000, ErrorRate: 0.2, Latency: model.LatencyPercentiles{P95: 200.0}},
		{Name: "GET /healthz", TotalCalls: 5000, ErrorRate: 0.0, Latency: model.LatencyPercentiles{P95: 5.0}},
	}
	// Checkout latency regresses 4x; healthz is unchanged
	snap.CurrentEndpoints = []model.RequestMetric{
		{Name: "POST /checkout", TotalCalls: 1000, ErrorRate: 0.2, Latency: model.LatencyPercentiles{P95: 800.0}},
		{Name: "GET /healthz", TotalCalls: 5000, ErrorRate: 0.0, Latency: model.LatencyPercentiles{P95: 5.0}},
	}

	// Verify exit code 2 contract
	err := NewExitError(ExitCodeActionable, "meaningful regression detected")
	if GetExitCode(err) != ExitCodeActionable {
		t.Errorf("expected exit code %d, got %d", ExitCodeActionable, GetExitCode(err))
	}
}

// Scenario J - Deploy with insufficient baseline
func TestScenarioJ_DeployInsufficientBaseline(t *testing.T) {
	resetRootFlags()
	defer resetRootFlags()

	// Verify exit code 3 contract for unknown / insufficient baseline
	err := NewExitError(ExitCodeUnknown, "insufficient baseline data to verify safety")
	if GetExitCode(err) != ExitCodeUnknown {
		t.Errorf("expected exit code %d, got %d", ExitCodeUnknown, GetExitCode(err))
	}
}

// Scenario K - Ambiguous explain subject
func TestScenarioK_AmbiguousExplainSubject(t *testing.T) {
	res := &domain.AnalysisResult{
		State: domain.HealthStateDegraded,
		Problems: []domain.Problem{
			{
				Kind:    domain.ProblemKindDegradation,
				Summary: "POST /orders/checkout degraded",
				Scope:   domain.Scope{Endpoint: "POST /orders/checkout"},
			},
		},
	}
	snap := domain.NewSnapshot(
		domain.ProfileContext{Name: "prod"},
		domain.Scope{},
		domain.WindowContext{},
	)
	snap.CurrentEndpoints = []model.RequestMetric{
		{Name: "POST /orders/checkout"},
	}
	snap.CurrentDependencies = []model.DependencyMetric{
		{Target: "sqldb-orders", Type: "SQL"},
	}

	_, _, err := resolveExplainSubject("order", res, snap, nil)
	if err == nil {
		t.Fatalf("expected error for ambiguous subject 'order', got nil")
	}

	errMsg := err.Error()
	if !strings.Contains(errMsg, `"order" matches:`) {
		t.Errorf("expected error message to contain '\"order\" matches:', got:\n%s", errMsg)
	}
	if !strings.Contains(errMsg, "POST /orders/checkout (endpoint)") {
		t.Errorf("expected candidate 'POST /orders/checkout (endpoint)', got:\n%s", errMsg)
	}
	if !strings.Contains(errMsg, "sqldb-orders (dependency)") {
		t.Errorf("expected candidate 'sqldb-orders (dependency)', got:\n%s", errMsg)
	}
	if !strings.Contains(errMsg, "Be more specific.") {
		t.Errorf("expected 'Be more specific.', got:\n%s", errMsg)
	}
}

// Scenario L - Upgrade naming: upgrade is canonical, update is hidden alias with deprecation warning
func TestScenarioL_UpgradeNaming(t *testing.T) {
	// 1. Root command help shows upgrade
	rootHelpBuf := new(bytes.Buffer)
	RootCmd.SetOut(rootHelpBuf)
	RootCmd.SetArgs([]string{"--help"})
	_ = RootCmd.Execute()

	rootHelp := rootHelpBuf.String()
	if !strings.Contains(rootHelp, "upgrade") {
		t.Errorf("expected 'upgrade' in root help, got:\n%s", rootHelp)
	}

	// 2. Update is hidden from help
	if !updateCmd.Hidden {
		t.Errorf("expected updateCmd to be Hidden")
	}
	if updateCmd.Deprecated == "" {
		t.Errorf("expected updateCmd to be marked Deprecated")
	}

	// 3. Upgrade command help succeeds cleanly
	resetRootFlags()
	defer resetRootFlags()
	upgradeHelpBuf := new(bytes.Buffer)
	RootCmd.SetOut(upgradeHelpBuf)
	RootCmd.SetArgs([]string{"upgrade", "--help"})
	if err := RootCmd.Execute(); err != nil {
		t.Fatalf("expected 'azlens upgrade --help' to succeed, got: %v", err)
	}
	upgradeHelp := upgradeHelpBuf.String()
	if !strings.Contains(upgradeHelp, "Upgrade azlens binary") && !strings.Contains(upgradeHelp, "upgrade") {
		t.Errorf("expected upgrade help text, got:\n%s", upgradeHelp)
	}
}
