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

profiles:
  checkout:
    name: "Checkout Service"
    target:
      insights:
        name: "app-checkout-prod"
      roles: "checkout-api"
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

	if len(prof.Target.Roles) != 1 || prof.Target.Roles[0] != "checkout-api" {
		t.Errorf("expected role 'checkout-api', got %v", prof.Target.Roles)
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
        subscription_id: "sub-insights-123"
      logs:
        workspace_id: "33333333-hhhh-iiii-jjjj-333333333333"
        subscription_id: "sub-logs-456"
        namespace: "ecommerce"
        database: "backend_ror"
      roles: "order-service"
      pods: "order-service"
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
	if len(prof.Target.Roles) != 1 || prof.Target.Roles[0] != "order-service" {
		t.Errorf("expected Target.Roles 'order-service', got %v", prof.Target.Roles)
	}
	if prof.Target.Logs.WorkspaceID != "33333333-hhhh-iiii-jjjj-333333333333" {
		t.Errorf("expected Target.Logs.WorkspaceID '33333333-hhhh-iiii-jjjj-333333333333', got '%s'", prof.Target.Logs.WorkspaceID)
	}
	if prof.Target.Logs.SubscriptionID != "sub-logs-456" {
		t.Errorf("expected Target.Logs.SubscriptionID 'sub-logs-456', got '%s'", prof.Target.Logs.SubscriptionID)
	}
	if prof.Target.Logs.Namespace != "ecommerce" {
		t.Errorf("expected Target.Logs.Namespace 'ecommerce', got '%s'", prof.Target.Logs.Namespace)
	}
	if len(prof.Target.Pods) != 1 || prof.Target.Pods[0] != "order-service" {
		t.Errorf("expected Target.Pods 'order-service', got %v", prof.Target.Pods)
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

	// Realistic topology: App Insights and Log Analytics live in different
	// Entra ID directories; the 3 environments share subscriptions, filters,
	// and flags — only insights.name and logs.workspace_id differ per env.
	yamlContent := `
version: "1.0"
defaults:
  profile: prod

shared:
  insights:
    directory_id: "dir-insights-shared"
    subscription_id: "sub-insights-shared"
  logs:
    directory_id: "dir-logs-shared"
    subscription_id: "sub-logs-shared"
    namespace: "ecommerce"
    database: "backend_ror"
  roles: "order-service"
  pods: "order-service"
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
      roles: "billing-service"          # per-env override of a shared field
      exclude_probes: false            # explicit false overrides shared true
      custom_dimensions:
        team: "staging-oncall"         # map keys merge, profile wins
    thresholds:
      p95_latency_warn_pct: 25.0       # per-env override of shared policy
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
	if len(prod.Target.Roles) != 1 || prod.Target.Roles[0] != "order-service" || len(prod.Target.Pods) != 1 || prod.Target.Pods[0] != "order-service" || prod.Target.Logs.Namespace != "ecommerce" || prod.Target.Logs.Database != "backend_ror" {
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
	if len(staging.Target.Roles) != 1 || staging.Target.Roles[0] != "billing-service" {
		t.Errorf("expected profile role override to win over shared, got %v", staging.Target.Roles)
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
	if len(staging.Target.Pods) != 1 || staging.Target.Pods[0] != "order-service" || staging.Target.Logs.Namespace != "ecommerce" {
		t.Errorf("expected other shared filters still inherited, got: %+v", staging.Target)
	}
	if staging.Target.CustomDimensions["team"] != "staging-oncall" {
		t.Errorf("expected profile custom_dimensions key to win, got: %v", staging.Target.CustomDimensions)
	}

	// 3. shared config itself is untouched by merges (no mutation side effects)
	if len(cfg.Shared.Roles) != 1 || cfg.Shared.Roles[0] != "order-service" {
		t.Errorf("shared target must not be mutated by GetProfile, got roles %v", cfg.Shared.Roles)
	}
}

func TestStringListUnmarshal(t *testing.T) {
	cases := []struct {
		name    string
		yaml    string
		want    StringList
		wantErr bool
	}{
		{name: "scalar", yaml: `roles: order-service`, want: StringList{"order-service"}},
		{name: "list", yaml: `roles: [order-service, billing-service]`, want: StringList{"order-service", "billing-service"}},
		{name: "block list", yaml: "roles:\n  - order-service\n  - billing-service\n", want: StringList{"order-service", "billing-service"}},
		{name: "empty scalar means unset", yaml: `roles: ""`, want: nil},
		{name: "empty list means unset", yaml: `roles: []`, want: nil},
		{name: "map is invalid", yaml: "roles:\n  a: b\n", wantErr: true},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			var out struct {
				Roles StringList `yaml:"roles"`
			}
			if err := yaml.Unmarshal([]byte(tc.yaml), &out); err != nil {
				if !tc.wantErr {
					t.Fatalf("unexpected unmarshal error: %v", err)
				}
				return
			}
			if tc.wantErr {
				t.Fatalf("expected error, got none (%v)", out.Roles)
			}
			if len(out.Roles) != len(tc.want) {
				t.Fatalf("expected %v, got %v", tc.want, out.Roles)
			}
			for i := range tc.want {
				if out.Roles[i] != tc.want[i] {
					t.Fatalf("expected %v, got %v", tc.want, out.Roles)
				}
			}
		})
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
