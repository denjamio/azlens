package config

import (
	"os"
	"testing"

	"gopkg.in/yaml.v3"
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
  service: checkout-api

profiles:
  checkout:
    name: "Checkout Service"
    target:
      insights:
        name: "app-checkout-prod"
      service: "checkout-api"
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
		t.Errorf("expected role 'checkout-api', got %v", prof.Target.Role)
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
  service: order-service

profiles:
  prod:
    name: "Production"
    target:
      insights:
        name: "app-shared-prod"
        subscription_id: "sub-insights-123"
      logs:
        workspace_id: "33333333-hhhh-iiii-jjjj-333333333333"
        subscription_id: "sub-logs-456"
        database: "backend_ror"
      service: "order-service"
      role: "order-service"
      pod: "order-service"
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
	if prof.Target.Insights.SubscriptionID != "sub-insights-123" {
		t.Errorf("expected Target.Insights.SubscriptionID 'sub-insights-123', got '%s'", prof.Target.Insights.SubscriptionID)
	}
	if prof.Target.Role != "order-service" {
		t.Errorf("expected Target.Role 'order-service', got %v", prof.Target.Role)
	}
	if prof.Target.Logs.WorkspaceID != "33333333-hhhh-iiii-jjjj-333333333333" {
		t.Errorf("expected Target.Logs.WorkspaceID '33333333-hhhh-iiii-jjjj-333333333333', got '%s'", prof.Target.Logs.WorkspaceID)
	}
	if prof.Target.Logs.SubscriptionID != "sub-logs-456" {
		t.Errorf("expected Target.Logs.SubscriptionID 'sub-logs-456', got '%s'", prof.Target.Logs.SubscriptionID)
	}
	if prof.Target.Pod != "order-service" {
		t.Errorf("expected Target.Pod 'order-service', got %v", prof.Target.Pod)
	}
	if prof.Target.Logs.Database != "backend_ror" {
		t.Errorf("expected Target.Logs.Database 'backend_ror', got '%s'", prof.Target.Logs.Database)
	}
	if prof.Target.ExcludesSynthetic() != true || prof.Target.ExcludesProbes() != true {
		t.Errorf("expected ExcludeSynthetic and ExcludeProbes true, got %v/%v", prof.Target.ExcludesSynthetic(), prof.Target.ExcludesProbes())
	}
}

func TestSharedTargetInheritance(t *testing.T) {
	tmpFile, err := os.CreateTemp("", "azlens-shared-*.yaml")
	if err != nil {
		t.Fatalf("failed creating temp file: %v", err)
	}
	defer func() { _ = os.Remove(tmpFile.Name()) }()

	yamlContent := `
version: "1.0"
defaults:
  profile: prod
  service: order-service

shared:
  insights:
    directory_id: "dir-insights-shared"
    subscription_id: "sub-insights-shared"
  logs:
    directory_id: "dir-logs-shared"
    subscription_id: "sub-logs-shared"
    database: "backend_ror"
  services:
    order-service:
      role: order-service
      pod: order-service
    billing-service:
      role: billing-service
      pod: billing-service
  service: "order-service"
  role: "order-service"
  pod: "order-service"
  exclude_synthetic: true
  exclude_probes: true
  custom_dimensions:
    team: "platform"
  thresholds:
    p95_latency_warn_pct: 15.0
    p95_latency_crit_pct: 30.0
    min_sample_calls: 5

profiles:
  prod:
    name: "Production"
    target:
      insights:
        name: "app-shared-prod"
      logs:
        workspace_id: "ws-guid-prod"
  staging:
    name: "Staging"
    target:
      insights:
        name: "app-shared-staging"
      logs:
        workspace_id: "ws-guid-staging"
      service: "billing-service"
      role: "billing-service"
      exclude_probes: false
      custom_dimensions:
        team: "staging-oncall"
    thresholds:
      p95_latency_warn_pct: 25.0
`
	if err := os.WriteFile(tmpFile.Name(), []byte(yamlContent), 0644); err != nil {
		t.Fatalf("failed writing temp file: %v", err)
	}

	cfg, err := LoadConfig(tmpFile.Name())
	if err != nil {
		t.Fatalf("failed loading config: %v", err)
	}

	// 1. prod inherits everything shared, only declares names
	prod, err := cfg.GetProfile("prod")
	if err != nil {
		t.Fatalf("failed getting prod profile: %v", err)
	}
	if prod.Target.Insights.Name != "app-shared-prod" || prod.Target.Logs.WorkspaceID != "ws-guid-prod" {
		t.Errorf("expected prod resource names from profile, got: %+v", prod.Target)
	}
	if prod.Target.Insights.SubscriptionID != "sub-insights-shared" {
		t.Errorf("expected shared insights subscription inherited, got: %+v", prod.Target.Insights)
	}
	if prod.Target.Logs.SubscriptionID != "sub-logs-shared" {
		t.Errorf("expected shared logs subscription inherited, got: %+v", prod.Target.Logs)
	}
	if prod.Target.Insights.DirectoryID != "dir-insights-shared" || prod.Target.Logs.DirectoryID != "dir-logs-shared" {
		t.Errorf("expected shared directory IDs inherited, got: %+v / %+v", prod.Target.Insights, prod.Target.Logs)
	}
	if prod.Target.Role != "order-service" || prod.Target.Pod != "order-service" || prod.Target.Logs.Database != "backend_ror" {
		t.Errorf("expected shared filters inherited, got: %+v", prod.Target)
	}
	if !prod.Target.ExcludesSynthetic() || !prod.Target.ExcludesProbes() {
		t.Errorf("expected shared exclusion flags inherited as true")
	}
	if prod.Target.CustomDimensions["team"] != "platform" {
		t.Errorf("expected shared custom_dimensions inherited, got: %v", prod.Target.CustomDimensions)
	}
	if prod.Thresholds.LatencyWarnPct != 15.0 || prod.Thresholds.MinSampleCalls != 5 {
		t.Errorf("expected shared thresholds inherited by prod, got: %+v", prod.Thresholds)
	}

	// 2. staging overrides specific shared fields
	staging, err := cfg.GetProfile("staging")
	if err != nil {
		t.Fatalf("failed getting staging profile: %v", err)
	}
	if staging.Target.Role != "billing-service" {
		t.Errorf("expected profile role override to win over shared, got %v", staging.Target.Role)
	}
	if staging.Target.ExcludesProbes() {
		t.Errorf("expected explicit profile exclude_probes=false to override shared true")
	}
	if staging.Thresholds.LatencyWarnPct != 25.0 || staging.Thresholds.MinSampleCalls != 5 {
		t.Errorf("expected staging threshold override to win with rest inherited, got: %+v", staging.Thresholds)
	}
	if !staging.Target.ExcludesSynthetic() {
		t.Errorf("expected shared exclude_synthetic=true still inherited")
	}
	if staging.Target.Pod != "order-service" || staging.Target.Logs.Database != "backend_ror" {
		t.Errorf("expected other shared filters still inherited, got: %+v", staging.Target)
	}
	if staging.Target.CustomDimensions["team"] != "staging-oncall" {
		t.Errorf("expected profile custom_dimensions key to win, got: %v", staging.Target.CustomDimensions)
	}

	// 3. shared config itself is untouched by merges (no mutation side effects)
	if cfg.Shared.Role != "order-service" {
		t.Errorf("shared target must not be mutated by GetProfile, got role %v", cfg.Shared.Role)
	}
}

func TestServiceDefUnmarshal(t *testing.T) {
	// Canonical role_name
	yamlDataNew := `
role_name: checkout-svc
pod: checkout-app
`
	var sDefNew ServiceDef
	if err := yaml.Unmarshal([]byte(yamlDataNew), &sDefNew); err != nil {
		t.Fatalf("unexpected unmarshal error: %v", err)
	}
	if sDefNew.GetRoleName() != "checkout-svc" || sDefNew.RoleName != "checkout-svc" || sDefNew.Pod != "checkout-app" {
		t.Errorf("expected ServiceDef{RoleName: checkout-svc, Pod: checkout-app}, got %+v", sDefNew)
	}

	// Legacy role backwards compatibility
	yamlDataLegacy := `
role: legacy-svc
pod: legacy-app
`
	var sDefLegacy ServiceDef
	if err := yaml.Unmarshal([]byte(yamlDataLegacy), &sDefLegacy); err != nil {
		t.Fatalf("unexpected unmarshal error: %v", err)
	}
	if sDefLegacy.GetRoleName() != "legacy-svc" || sDefLegacy.Role != "legacy-svc" || sDefLegacy.Pod != "legacy-app" {
		t.Errorf("expected ServiceDef{Role: legacy-svc, Pod: legacy-app}, got %+v", sDefLegacy)
	}
}

func parseConfigTest(t *testing.T, yamlStr string) *Config {
	t.Helper()
	var cfg Config
	if err := yaml.Unmarshal([]byte(yamlStr), &cfg); err != nil {
		t.Fatalf("failed parsing YAML config: %v", err)
	}
	return &cfg
}

func TestInsightsNameAndRoleName(t *testing.T) {
	// Test canonical insights_name and role_name
	canonicalYAML := `
version: "1.0"
defaults:
  profile: prod
  service: checkout
shared:
  services:
    checkout:
      role_name: checkout-service
      pod: checkout-pod
profiles:
  prod:
    name: "Production"
    target:
      insights_name: "app-insights-canonical"
      role_name: "checkout-service"
`
	cfg := parseConfigTest(t, canonicalYAML)
	prof, err := cfg.GetProfile("prod")
	if err != nil {
		t.Fatalf("failed getting prod profile: %v", err)
	}
	if prof.Target.GetInsightsName() != "app-insights-canonical" {
		t.Errorf("expected GetInsightsName 'app-insights-canonical', got %q", prof.Target.GetInsightsName())
	}
	if prof.Target.InsightsName != "app-insights-canonical" {
		t.Errorf("expected InsightsName 'app-insights-canonical', got %q", prof.Target.InsightsName)
	}
	if prof.Target.Insights.Name != "app-insights-canonical" {
		t.Errorf("expected synchronized Insights.Name 'app-insights-canonical', got %q", prof.Target.Insights.Name)
	}
	if prof.Target.GetRoleName() != "checkout-service" {
		t.Errorf("expected GetRoleName 'checkout-service', got %q", prof.Target.GetRoleName())
	}
	if prof.Target.RoleName != "checkout-service" {
		t.Errorf("expected RoleName 'checkout-service', got %q", prof.Target.RoleName)
	}
	if prof.Target.Role != "checkout-service" {
		t.Errorf("expected synchronized Role 'checkout-service', got %q", prof.Target.Role)
	}

	// Test legacy insights.name and role
	legacyYAML := `
version: "1.0"
profiles:
  legacy:
    name: "Legacy"
    target:
      insights:
        name: "app-insights-legacy"
      role: "legacy-service"
`
	cfgLegacy := parseConfigTest(t, legacyYAML)
	profLegacy, err := cfgLegacy.GetProfile("legacy")
	if err != nil {
		t.Fatalf("failed getting legacy profile: %v", err)
	}
	if profLegacy.Target.GetInsightsName() != "app-insights-legacy" {
		t.Errorf("expected GetInsightsName 'app-insights-legacy', got %q", profLegacy.Target.GetInsightsName())
	}
	if profLegacy.Target.InsightsName != "app-insights-legacy" {
		t.Errorf("expected synchronized InsightsName 'app-insights-legacy', got %q", profLegacy.Target.InsightsName)
	}
	if profLegacy.Target.Insights.Name != "app-insights-legacy" {
		t.Errorf("expected Insights.Name 'app-insights-legacy', got %q", profLegacy.Target.Insights.Name)
	}
	if profLegacy.Target.GetRoleName() != "legacy-service" {
		t.Errorf("expected GetRoleName 'legacy-service', got %q", profLegacy.Target.GetRoleName())
	}
	if profLegacy.Target.RoleName != "legacy-service" {
		t.Errorf("expected synchronized RoleName 'legacy-service', got %q", profLegacy.Target.RoleName)
	}
	if profLegacy.Target.Role != "legacy-service" {
		t.Errorf("expected Role 'legacy-service', got %q", profLegacy.Target.Role)
	}
}

func TestDefaultConfigSharedFilters(t *testing.T) {
	cfg := DefaultConfig()
	prof, err := cfg.GetProfile("prod")
	if err != nil {
		t.Fatalf("failed getting default prod profile: %v", err)
	}
	if !prof.Target.ExcludesSynthetic() || !prof.Target.ExcludesProbes() {
		t.Errorf("expected default shared exclusions true on effective prod profile")
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

func TestResolveProfilePrecedence(t *testing.T) {
	// Setup test config with multiple profiles and a default
	cfgMulti := &Config{
		Defaults: Defaults{Profile: "prod"},
		Profiles: map[string]Profile{
			"prod":    {Name: "Production"},
			"staging": {Name: "Staging"},
			"dev":     {Name: "Development"},
		},
	}

	// 1. --profile / -p flag always wins
	t.Setenv("AZLENS_PROFILE", "staging")

	got, err := cfgMulti.ResolveProfile("dev")
	if err != nil || got != "dev" {
		t.Fatalf("expected CLI flag 'dev' to win over env and config, got %q (err: %v)", got, err)
	}

	// 2. AZLENS_PROFILE wins when flag is empty
	got, err = cfgMulti.ResolveProfile("")
	if err != nil || got != "staging" {
		t.Fatalf("expected AZLENS_PROFILE 'staging' to win over config default, got %q (err: %v)", got, err)
	}

	// 3. defaults.profile wins when neither CLI nor env is set
	t.Setenv("AZLENS_PROFILE", "")
	got, err = cfgMulti.ResolveProfile("")
	if err != nil || got != "prod" {
		t.Fatalf("expected defaults.profile 'prod' to win, got %q (err: %v)", got, err)
	}

	// 4. The only configured profile is selected when defaults.profile is absent
	cfgSingle := &Config{
		Profiles: map[string]Profile{
			"only-one": {Name: "Only One"},
		},
	}
	got, err = cfgSingle.ResolveProfile("")
	if err != nil || got != "only-one" {
		t.Fatalf("expected the single profile 'only-one' to be selected, got %q (err: %v)", got, err)
	}

	// 5. Error when multiple profiles exist and none can be selected
	cfgNoDefault := &Config{
		Profiles: map[string]Profile{
			"prod":    {Name: "Production"},
			"staging": {Name: "Staging"},
		},
	}
	_, err = cfgNoDefault.ResolveProfile("")
	if err == nil {
		t.Fatalf("expected error when multiple profiles exist without selection, got nil")
	}
}
