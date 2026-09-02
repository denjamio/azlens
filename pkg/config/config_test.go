package config

import (
	"os"
	"testing"
)

func TestConfigLoadAndGetProfile(t *testing.T) {
	tmpFile, err := os.CreateTemp("", "azlens-*.yaml")
	if err != nil {
		t.Fatalf("failed creating temp file: %v", err)
	}
	defer func() { _ = os.Remove(tmpFile.Name()) }()

	yamlContent := `
version: "1.0"
defaults:
  profile: checkout

profiles:
  checkout:
    name: "Checkout Service"
    target:
      insights:
        name: "app-checkout-prod"
      role: "checkout-api"
    thresholds:
      p95_latency_warn_pct: 18.0
      p95_latency_crit_pct: 35.0
`
	if err := os.WriteFile(tmpFile.Name(), []byte(yamlContent), 0644); err != nil {
		t.Fatalf("failed writing temp file: %v", err)
	}

	cfg, err := LoadConfig(tmpFile.Name())
	if err != nil {
		t.Fatalf("failed loading config: %v", err)
	}

	if cfg.GetDefaultProfile() != "checkout" {
		t.Errorf("expected default profile 'checkout', got '%s'", cfg.GetDefaultProfile())
	}

	prof, err := cfg.GetProfile("")
	if err != nil {
		t.Fatalf("failed getting default profile: %v", err)
	}

	if prof.Target.Role != "checkout-api" {
		t.Errorf("expected role 'checkout-api', got '%s'", prof.Target.Role)
	}
	if prof.Thresholds.LatencyCritPct != 35.0 {
		t.Errorf("expected 35.0 crit threshold, got %f", prof.Thresholds.LatencyCritPct)
	}
}

func TestConfigEffectiveDefaults(t *testing.T) {
	cfg := &Config{
		Defaults: Defaults{
			Profile: "prod",
			Window:  "2h",
			Since:   "1h",
			Limit:   20,
			Output:  "markdown",
		},
		Profiles: map[string]Profile{
			"prod": {
				Name: "Prod",
			},
			"staging": {
				Name: "Staging",
				Defaults: Defaults{
					Window: "30m",
					Since:  "15m",
					Limit:  10,
				},
			},
		},
	}

	// 1. Prod inherits global operational defaults
	prodDefs := cfg.EffectiveDefaults("prod")
	if prodDefs.Window != "2h" || prodDefs.Limit != 20 || prodDefs.Output != "markdown" {
		t.Errorf("prod defaults mismatch: %+v", prodDefs)
	}

	// 2. Staging overrides window, since, limit, but inherits output
	stagDefs := cfg.EffectiveDefaults("staging")
	if stagDefs.Window != "30m" || stagDefs.Since != "15m" || stagDefs.Limit != 10 {
		t.Errorf("staging overrides mismatch: %+v", stagDefs)
	}
	if stagDefs.Output != "markdown" {
		t.Errorf("staging inherited defaults mismatch: %+v", stagDefs)
	}

	// 3. Unknown profile gets global + system defaults
	unknownDefs := cfg.EffectiveDefaults("unknown")
	if unknownDefs.Window != "2h" {
		t.Errorf("unknown profile defaults mismatch: %+v", unknownDefs)
	}
}

func TestConfigTargetExplicit(t *testing.T) {
	tmpFile, err := os.CreateTemp("", "azlens-target-*.yaml")
	if err != nil {
		t.Fatalf("failed creating temp file: %v", err)
	}
	defer func() { _ = os.Remove(tmpFile.Name()) }()

	yamlContent := `
version: "1.0"
defaults:
  profile: prod

profiles:
  prod:
    name: "Production"
    target:
      insights:
        name: "app-shared-prod"
        subscription: "sub-insights-123"
      logs:
        workspace_id: "33333333-hhhh-iiii-jjjj-333333333333"
        subscription: "sub-logs-456"
      role: "order-service"
      pod: "order-service"
      namespace: "ecommerce"
      database: "backend_ror"
      exclude_synthetic: true
      exclude_probes: true
`
	if err := os.WriteFile(tmpFile.Name(), []byte(yamlContent), 0644); err != nil {
		t.Fatalf("failed writing temp file: %v", err)
	}

	cfg, err := LoadConfig(tmpFile.Name())
	if err != nil {
		t.Fatalf("failed loading config: %v", err)
	}

	prof, err := cfg.GetProfile("prod")
	if err != nil {
		t.Fatalf("failed getting profile: %v", err)
	}

	if prof.Target.Insights.Name != "app-shared-prod" {
		t.Errorf("expected Target.Insights.Name 'app-shared-prod', got '%s'", prof.Target.Insights.Name)
	}
	if prof.Target.Insights.Subscription != "sub-insights-123" {
		t.Errorf("expected Target.Insights.Subscription 'sub-insights-123', got '%s'", prof.Target.Insights.Subscription)
	}
	if prof.Target.Role != "order-service" {
		t.Errorf("expected Target.Role 'order-service', got '%s'", prof.Target.Role)
	}
	if prof.Target.Logs.WorkspaceID != "33333333-hhhh-iiii-jjjj-333333333333" {
		t.Errorf("expected Target.Logs.WorkspaceID '33333333-hhhh-iiii-jjjj-333333333333', got '%s'", prof.Target.Logs.WorkspaceID)
	}
	if prof.Target.Logs.Subscription != "sub-logs-456" {
		t.Errorf("expected Target.Logs.Subscription 'sub-logs-456', got '%s'", prof.Target.Logs.Subscription)
	}
	if prof.Target.Namespace != "ecommerce" {
		t.Errorf("expected Target.Namespace 'ecommerce', got '%s'", prof.Target.Namespace)
	}
	if prof.Target.Pod != "order-service" {
		t.Errorf("expected Target.Pod 'order-service', got '%s'", prof.Target.Pod)
	}
	if prof.Target.Database != "backend_ror" {
		t.Errorf("expected Target.Database 'backend_ror', got '%s'", prof.Target.Database)
	}
	if !prof.Target.ExcludeSynthetic || !prof.Target.ExcludeProbes {
		t.Errorf("expected ExcludeSynthetic and ExcludeProbes true")
	}
}

func TestAvailableProfilesIsSorted(t *testing.T) {
	cfg := &Config{
		Profiles: map[string]Profile{
			"prod":    {Name: "Production"},
			"staging": {Name: "Staging"},
			"dev":     {Name: "Development"},
		},
	}

	got := cfg.AvailableProfiles()
	want := []string{"dev", "prod", "staging"}

	if len(got) != len(want) {
		t.Fatalf("expected %d profiles, got %d (%v)", len(want), len(got), got)
	}
	for i := range want {
		if got[i] != want[i] {
			t.Errorf("expected sorted profiles %v, got %v", want, got)
			break
		}
	}
}
