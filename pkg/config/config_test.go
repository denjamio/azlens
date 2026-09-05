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
    insights:
      name: "app-checkout-prod"
    service: "checkout-api"
    role_name: "checkout-api"
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

	if prof.Target.Insights.Name != "app-checkout-prod" {
		t.Errorf("expected Insights.Name 'app-checkout-prod', got %v", prof.Target.Insights.Name)
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
    insights:
      name: "app-shared-prod"
      subscription_id: "sub-insights-123"
    logs:
      workspace_id: "33333333-hhhh-iiii-jjjj-333333333333"
      subscription_id: "sub-logs-456"
      database: "backend_ror"
    service: "order-service"
    role_name: "order-service"
    pod: "order-service"
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
	if prof.Target.RoleName != "order-service" {
		t.Errorf("expected Target.RoleName 'order-service', got %v", prof.Target.RoleName)
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
      role_name: order-service
      pod: order-service
    billing-service:
      role_name: billing-service
      pod: billing-service
  service: "order-service"
  role_name: "order-service"
  pod: "order-service"
  custom_dimensions:
    team: "platform"
  thresholds:
    p95_latency_warn_pct: 15.0
    p95_latency_crit_pct: 30.0
    min_sample_calls: 10

profiles:
  prod:
    name: "Production"
    insights:
      name: "app-shared-prod"
    logs:
      workspace_id: "ws-guid-prod"
  staging:
    name: "Staging"
    insights:
      name: "app-shared-staging"
    logs:
      workspace_id: "ws-guid-staging"
    service: "billing-service"
    role_name: "billing-service"
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
	if prod.Target.RoleName != "order-service" || prod.Target.Pod != "order-service" || prod.Target.Logs.Database != "backend_ror" {
		t.Errorf("expected shared filters inherited, got: %+v", prod.Target)
	}
	if prod.Target.CustomDimensions["team"] != "platform" {
		t.Errorf("expected shared custom_dimensions inherited, got: %v", prod.Target.CustomDimensions)
	}
	if prod.Thresholds.LatencyWarnPct != 15.0 || prod.Thresholds.MinSampleCalls != 10 {
		t.Errorf("expected shared thresholds inherited by prod, got: %+v", prod.Thresholds)
	}

	// 2. staging overrides specific shared fields
	staging, err := cfg.GetProfile("staging")
	if err != nil {
		t.Fatalf("failed getting staging profile: %v", err)
	}
	if staging.Target.RoleName != "billing-service" {
		t.Errorf("expected profile role_name override to win over shared, got %v", staging.Target.RoleName)
	}
	if staging.Thresholds.LatencyWarnPct != 25.0 || staging.Thresholds.MinSampleCalls != 10 {
		t.Errorf("expected staging threshold override to win with rest inherited, got: %+v", staging.Thresholds)
	}
	if staging.Target.Pod != "order-service" || staging.Target.Logs.Database != "backend_ror" {
		t.Errorf("expected other shared filters still inherited, got: %+v", staging.Target)
	}
	if staging.Target.CustomDimensions["team"] != "staging-oncall" {
		t.Errorf("expected profile custom_dimensions key to win, got: %v", staging.Target.CustomDimensions)
	}

	// 3. shared config itself is untouched by merges (no mutation side effects)
	if cfg.Shared.RoleName != "order-service" {
		t.Errorf("shared target must not be mutated by GetProfile, got role_name %v", cfg.Shared.RoleName)
	}
}

func TestServiceDefUnmarshal(t *testing.T) {
	yamlData := `
role_name: checkout-svc
pod: checkout-app
`
	var sDef ServiceDef
	if err := yaml.Unmarshal([]byte(yamlData), &sDef); err != nil {
		t.Fatalf("unexpected unmarshal error: %v", err)
	}
	if sDef.RoleName != "checkout-svc" || sDef.Pod != "checkout-app" {
		t.Errorf("expected ServiceDef{RoleName: checkout-svc, Pod: checkout-app}, got %+v", sDef)
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
    insights:
      name: "app-insights-canonical"
    role_name: "checkout-service"
`
	cfg := parseConfigTest(t, canonicalYAML)
	prof, err := cfg.GetProfile("prod")
	if err != nil {
		t.Fatalf("failed getting prod profile: %v", err)
	}
	if prof.Target.Insights.Name != "app-insights-canonical" {
		t.Errorf("expected Insights.Name 'app-insights-canonical', got %q", prof.Target.Insights.Name)
	}
	if prof.Target.RoleName != "checkout-service" {
		t.Errorf("expected RoleName 'checkout-service', got %q", prof.Target.RoleName)
	}

	// Also verify explicit target: wrapper works if provided
	explicitTargetYAML := `
version: "1.0"
profiles:
  prod:
    name: "Production"
    target:
      insights:
        name: "app-insights-explicit"
      role_name: "checkout-service"
`
	cfgExplicit := parseConfigTest(t, explicitTargetYAML)
	profExplicit, err := cfgExplicit.GetProfile("prod")
	if err != nil {
		t.Fatalf("failed getting prod profile: %v", err)
	}
	if profExplicit.Target.Insights.Name != "app-insights-explicit" {
		t.Errorf("expected Insights.Name 'app-insights-explicit', got %q", profExplicit.Target.Insights.Name)
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

func TestServiceDatabaseConfiguration(t *testing.T) {
	tmpFile, err := os.CreateTemp("", "azlens-svc-db-*.yaml")
	if err != nil {
		t.Fatalf("failed creating temp file: %v", err)
	}
	defer func() { _ = os.Remove(tmpFile.Name()) }()

	yamlContent := `
version: "1.0"
defaults:
  profile: prod
  service: backend

shared:
  insights:
    name: "app-shared"
  logs:
    workspace_id: "ws-123"
    database: "default_db"
  services:
    backend:
      role_name: "msw-egm-backend-ror"
      database: "backend_ror"
    auth:
      role_name: "auth-service"
      database: "auth_db"
    legacy:
      role_name: "legacy-service"

profiles:
  prod:
    name: "Production"
`
	if err := os.WriteFile(tmpFile.Name(), []byte(yamlContent), 0644); err != nil {
		t.Fatalf("failed writing temp file: %v", err)
	}

	cfg, err := LoadConfig(tmpFile.Name())
	if err != nil {
		t.Fatalf("failed loading config: %v", err)
	}

	prod, err := cfg.GetProfile("prod")
	if err != nil {
		t.Fatalf("failed getting prod profile: %v", err)
	}

	if prod.Target.Services["backend"].Database != "backend_ror" {
		t.Errorf("expected backend service database 'backend_ror', got %q", prod.Target.Services["backend"].Database)
	}
	if prod.Target.Services["auth"].Database != "auth_db" {
		t.Errorf("expected auth service database 'auth_db', got %q", prod.Target.Services["auth"].Database)
	}
	if prod.Target.Services["legacy"].Database != "" {
		t.Errorf("expected legacy service database '', got %q", prod.Target.Services["legacy"].Database)
	}
}

func TestDynamicServiceFilters(t *testing.T) {
	yamlData := `
role_name: checkout-svc
dependencies:
  sql_types:
    - PostgreSQL
  cache_types:
    - Redis
  ignored_targets:
    - localhost:8080
exceptions:
  ignored_types:
    - OperationCanceledException
    - TaskCanceledException
  ignored_messages:
    - "context canceled"
endpoints:
  ignored_endpoints:
    - /metrics
    - /livez
  ignored_user_agents:
    - Prometheus
`
	var sDef ServiceDef
	if err := yaml.Unmarshal([]byte(yamlData), &sDef); err != nil {
		t.Fatalf("failed unmarshaling service def with dynamic filters: %v", err)
	}

	if len(sDef.Dependencies.SQLTypes) != 1 || sDef.Dependencies.SQLTypes[0] != "PostgreSQL" {
		t.Errorf("expected SQLTypes [PostgreSQL], got %v", sDef.Dependencies.SQLTypes)
	}
	if len(sDef.Dependencies.CacheTypes) != 1 || sDef.Dependencies.CacheTypes[0] != "Redis" {
		t.Errorf("expected CacheTypes [Redis], got %v", sDef.Dependencies.CacheTypes)
	}
	if len(sDef.Dependencies.IgnoredTargets) != 1 || sDef.Dependencies.IgnoredTargets[0] != "localhost:8080" {
		t.Errorf("expected IgnoredTargets [localhost:8080], got %v", sDef.Dependencies.IgnoredTargets)
	}
	if len(sDef.Exceptions.IgnoredTypes) != 2 || sDef.Exceptions.IgnoredTypes[0] != "OperationCanceledException" {
		t.Errorf("expected IgnoredTypes [OperationCanceledException TaskCanceledException], got %v", sDef.Exceptions.IgnoredTypes)
	}
	if len(sDef.Endpoints.IgnoredEndpoints) != 2 || sDef.Endpoints.IgnoredEndpoints[0] != "/metrics" {
		t.Errorf("expected IgnoredEndpoints [/metrics /livez], got %v", sDef.Endpoints.IgnoredEndpoints)
	}
}

func TestMergeDynamicFilters(t *testing.T) {
	baseDep := DependenciesConfig{
		SQLTypes:       []string{"SQL"},
		IgnoredTargets: []string{"localhost"},
	}
	overrideDep := DependenciesConfig{
		SQLTypes:       []string{"PostgreSQL"},
		IgnoredTargets: []string{"consul"},
	}
	mergedDep := MergeDependencies(baseDep, overrideDep)
	if len(mergedDep.SQLTypes) != 1 || mergedDep.SQLTypes[0] != "PostgreSQL" {
		t.Errorf("expected overridden SQLTypes [PostgreSQL], got %v", mergedDep.SQLTypes)
	}
	if len(mergedDep.IgnoredTargets) != 2 {
		t.Errorf("expected combined IgnoredTargets [localhost consul], got %v", mergedDep.IgnoredTargets)
	}

	baseExc := ExceptionsConfig{
		IgnoredTypes: []string{"BaseException"},
	}
	overrideExc := ExceptionsConfig{
		IgnoredTypes: []string{"TaskCanceledException"},
	}
	mergedExc := MergeExceptions(baseExc, overrideExc)
	if len(mergedExc.IgnoredTypes) != 2 {
		t.Errorf("expected combined IgnoredTypes, got %v", mergedExc.IgnoredTypes)
	}
}

