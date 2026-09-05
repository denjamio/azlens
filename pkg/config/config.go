// Package config implements the azlens configuration schema, loading,
// profile resolution, and validation with actionable diagnostics.
package config

import (
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"

	"gopkg.in/yaml.v3"
)

// System defaults (convention over configuration): the single source of truth
// for fallback values used when neither the config file nor CLI flags provide one.
const (
	DefaultProfile = "prod"
	DefaultWindow  = "1h"
	DefaultSince   = "1h"
	DefaultLimit   = 15
	DefaultOutput  = "table"
)

// Defaults defines operational preferences (active profile, default service, timeframes, limits, output).
// These are inherited by profiles when not explicitly overridden.
type Defaults struct {
	Profile string `yaml:"profile,omitempty" json:"profile,omitempty"`
	Service string `yaml:"service,omitempty" json:"service,omitempty"`
	Window  string `yaml:"window,omitempty" json:"window,omitempty"`
	Since   string `yaml:"since,omitempty" json:"since,omitempty"`
	Limit   int    `yaml:"limit,omitempty" json:"limit,omitempty"`
	Output  string `yaml:"output,omitempty" json:"output,omitempty"`
}

// InsightsConfig holds configuration for Application Insights.
type InsightsConfig struct {
	Name           string `yaml:"name,omitempty" json:"name,omitempty"`
	ResourceGroup  string `yaml:"resource_group,omitempty" json:"resource_group,omitempty"`
	DirectoryID    string `yaml:"directory_id,omitempty" json:"directory_id,omitempty"`
	SubscriptionID string `yaml:"subscription_id,omitempty" json:"subscription_id,omitempty"`
}

// LogsConfig holds configuration for Log Analytics.
type LogsConfig struct {
	WorkspaceID    string `yaml:"workspace_id,omitempty" json:"workspace_id,omitempty"`
	DirectoryID    string `yaml:"directory_id,omitempty" json:"directory_id,omitempty"`
	SubscriptionID string `yaml:"subscription_id,omitempty" json:"subscription_id,omitempty"`
	Database       string `yaml:"database,omitempty" json:"database,omitempty"`
}

// BoolPtr returns a pointer to b. Target boolean filters use pointers so that
// nil means "not set here" (inherit from shared) while an explicit value can
// override the shared configuration.
func BoolPtr(b bool) *bool { return &b }

// ServiceDef maps a service name to its Application Insights targeting dimensions:
// - role_name: exact cloud_RoleName in Application Insights
// - pod: cloud_RoleInstance token base (pod name without deployment hash)
type ServiceDef struct {
	RoleName string `yaml:"role_name,omitempty" json:"role_name,omitempty"`
	Pod      string `yaml:"pod,omitempty" json:"pod,omitempty"`
}

// TargetConfig encapsulates telemetry destination and filter criteria.
// DHH: Un concepto, un nombre, un lugar.
//
// Filter reference (what each option filters in KQL):
//   - service           -> Resolved service name from shared.services or CLI (-s / --service)
//   - insights.name     -> App Insights component name or App ID (GUID)
//   - role_name         -> cloud_RoleName (EXACT microservice name; =~ equality)
//   - pod               -> cloud_RoleInstance (pod name WITHOUT the deployment hash; token match: has)
//   - logs.database     -> Db (MySqlSlowLogs in Log Analytics; mandatory tenant filter)
//   - resource_id       -> _ResourceId (Log Analytics multi-resource workspaces)
//   - exclude_synthetic -> operation_SyntheticSource / syntheticSource
//   - exclude_probes    -> kube-probe User-Agent + /healthz-style routes
//   - custom_dimensions -> customDimensions['<key>'] =~ '<value>'
type TargetConfig struct {
	Insights         InsightsConfig        `yaml:"insights,omitempty" json:"insights,omitempty"`
	Logs             LogsConfig            `yaml:"logs,omitempty" json:"logs,omitempty"`
	Service          string                `yaml:"service,omitempty" json:"service,omitempty"`
	Services         map[string]ServiceDef `yaml:"services,omitempty" json:"services,omitempty"`
	RoleName         string                `yaml:"role_name,omitempty" json:"role_name,omitempty"`
	Pod              string                `yaml:"pod,omitempty" json:"pod,omitempty"`
	ResourceID       string                `yaml:"resource_id,omitempty" json:"resource_id,omitempty"`
	ExcludeSynthetic *bool                 `yaml:"exclude_synthetic,omitempty" json:"exclude_synthetic,omitempty"`
	ExcludeProbes    *bool                 `yaml:"exclude_probes,omitempty" json:"exclude_probes,omitempty"`
	CustomDimensions map[string]string     `yaml:"custom_dimensions,omitempty" json:"custom_dimensions,omitempty"`
}

// ExcludesSynthetic reports whether synthetic traffic / availability tests must be filtered
func (t TargetConfig) ExcludesSynthetic() bool {
	return t.ExcludeSynthetic != nil && *t.ExcludeSynthetic
}

// ExcludesProbes reports whether health probes (/healthz, kube-probe) must be filtered
func (t TargetConfig) ExcludesProbes() bool { return t.ExcludeProbes != nil && *t.ExcludeProbes }

// MergeTarget combines shared and profile-specific target configs: the profile
// wins on every field it sets; shared fills everything else. Inheritance over
// duplication — profiles should only declare what differs per environment.
func MergeTarget(shared, override TargetConfig) TargetConfig {
	merged := shared
	if override.Insights.Name != "" {
		merged.Insights.Name = override.Insights.Name
	}
	if override.Insights.ResourceGroup != "" {
		merged.Insights.ResourceGroup = override.Insights.ResourceGroup
	}
	if override.Insights.DirectoryID != "" {
		merged.Insights.DirectoryID = override.Insights.DirectoryID
	}
	if override.Insights.SubscriptionID != "" {
		merged.Insights.SubscriptionID = override.Insights.SubscriptionID
	}
	if override.Logs.WorkspaceID != "" {
		merged.Logs.WorkspaceID = override.Logs.WorkspaceID
	}
	if override.Logs.DirectoryID != "" {
		merged.Logs.DirectoryID = override.Logs.DirectoryID
	}
	if override.Logs.SubscriptionID != "" {
		merged.Logs.SubscriptionID = override.Logs.SubscriptionID
	}
	if override.Logs.Database != "" {
		merged.Logs.Database = override.Logs.Database
	}
	if override.Service != "" {
		merged.Service = override.Service
	}
	if len(override.Services) > 0 {
		svcs := make(map[string]ServiceDef, len(shared.Services)+len(override.Services))
		for k, v := range shared.Services {
			svcs[k] = v
		}
		for k, v := range override.Services {
			svcs[k] = v
		}
		merged.Services = svcs
	}
	if override.RoleName != "" {
		merged.RoleName = override.RoleName
	}
	if override.Pod != "" {
		merged.Pod = override.Pod
	}
	if override.ResourceID != "" {
		merged.ResourceID = override.ResourceID
	}
	if override.ExcludeSynthetic != nil {
		merged.ExcludeSynthetic = override.ExcludeSynthetic
	}
	if override.ExcludeProbes != nil {
		merged.ExcludeProbes = override.ExcludeProbes
	}
	if len(override.CustomDimensions) > 0 {
		dims := make(map[string]string, len(shared.CustomDimensions)+len(override.CustomDimensions))
		for k, v := range shared.CustomDimensions {
			dims[k] = v
		}
		for k, v := range override.CustomDimensions {
			dims[k] = v
		}
		merged.CustomDimensions = dims
	}

	return merged
}

// Profile represents a specific project or environment telemetry configuration.
// Telemetry targets (insights, logs, service, role_name, etc.) are inlined,
// matching the shared structure, while explicit target: wrappers remain supported.
type Profile struct {
	Name   string       `yaml:"name,omitempty" json:"name,omitempty"`
	Target TargetConfig `yaml:",inline" json:"target"`
	// Per-profile operational overrides (window, since, limit, output)
	Defaults   Defaults          `yaml:"defaults,omitempty" json:"defaults,omitempty"`
	Thresholds ProfileThresholds `yaml:"thresholds,omitempty" json:"thresholds,omitempty"`
}

// UnmarshalYAML supports both omitted target (direct inlined insights and logs)
// and explicit 'target:' wrappers.
func (p *Profile) UnmarshalYAML(value *yaml.Node) error {
	type rawProfile Profile
	var raw rawProfile
	if err := value.Decode(&raw); err != nil {
		return err
	}
	*p = Profile(raw)

	var wrapper struct {
		Target TargetConfig `yaml:"target"`
	}
	if err := value.Decode(&wrapper); err == nil {
		p.Target = MergeTarget(p.Target, wrapper.Target)
	}
	return nil
}

// ProfileThresholds defines regression limits for the profile
type ProfileThresholds struct {
	LatencyWarnPct     float64 `yaml:"p95_latency_warn_pct,omitempty" json:"p95_latency_warn_pct,omitempty"`
	LatencyCritPct     float64 `yaml:"p95_latency_crit_pct,omitempty" json:"p95_latency_crit_pct,omitempty"`
	ErrorRateWarnDelta float64 `yaml:"error_rate_warn_delta,omitempty" json:"error_rate_warn_delta,omitempty"`
	ErrorRateCritDelta float64 `yaml:"error_rate_crit_delta,omitempty" json:"error_rate_crit_delta,omitempty"`
	MinSampleCalls     int64   `yaml:"min_sample_calls,omitempty" json:"min_sample_calls,omitempty"`
}

// MergeThresholds combines shared and profile-specific quality-gate policy:
// every field the profile sets (non-zero) wins; shared fills the rest.
func MergeThresholds(shared, override ProfileThresholds) ProfileThresholds {
	merged := shared
	if override.LatencyWarnPct > 0 {
		merged.LatencyWarnPct = override.LatencyWarnPct
	}
	if override.LatencyCritPct > 0 {
		merged.LatencyCritPct = override.LatencyCritPct
	}
	if override.ErrorRateWarnDelta > 0 {
		merged.ErrorRateWarnDelta = override.ErrorRateWarnDelta
	}
	if override.ErrorRateCritDelta > 0 {
		merged.ErrorRateCritDelta = override.ErrorRateCritDelta
	}
	if override.MinSampleCalls > 0 {
		merged.MinSampleCalls = override.MinSampleCalls
	}
	return merged
}

// SharedConfig holds everything inherited by every profile: the telemetry
// target scope (inlined — insights, logs, roles, pods, filters) and the
// shared quality-gate policy (thresholds). Declare once what does not vary
// across environments; profiles override field-by-field.
type SharedConfig struct {
	TargetConfig `yaml:",inline"`
	Thresholds   ProfileThresholds `yaml:"thresholds,omitempty" json:"thresholds,omitempty"`
}

// Config represents the top-level azlens configuration file (azlens.yaml)
type Config struct {
	Version  string             `yaml:"version" json:"version"`
	Defaults Defaults           `yaml:"defaults,omitempty" json:"defaults,omitempty"`
	Shared   SharedConfig       `yaml:"shared,omitempty" json:"shared,omitempty"`
	Profiles map[string]Profile `yaml:"profiles" json:"profiles"`
	// LoadedPath is where the config was resolved from
	LoadedPath string `yaml:"-" json:"-"`
}

// ResolveProfile resolves the active profile name in exact precedence order (Section 4):
// 1. --profile / -p (passed as cliProfile)
// 2. AZLENS_PROFILE environment variable
// 3. defaults.profile in azlens.yaml
// 4. the only configured profile, if exactly one exists
// 5. DefaultProfile ("prod") fallback
func (c *Config) ResolveProfile(cliProfile string) (string, error) {
	if trimmed := strings.TrimSpace(cliProfile); trimmed != "" {
		return trimmed, nil
	}
	if envProf := strings.TrimSpace(os.Getenv("AZLENS_PROFILE")); envProf != "" {
		return envProf, nil
	}
	if c != nil && strings.TrimSpace(c.Defaults.Profile) != "" {
		return strings.TrimSpace(c.Defaults.Profile), nil
	}
	if c != nil && len(c.Profiles) == 1 {
		for name := range c.Profiles {
			return name, nil
		}
	}
	if c != nil && len(c.Profiles) > 1 {
		return "", fmt.Errorf("no profile selected and multiple profiles configured (%s): select one with --profile / -p, set AZLENS_PROFILE, or configure defaults.profile in azlens.yaml", strings.Join(c.AvailableProfiles(), ", "))
	}
	return DefaultProfile, nil
}

// GetDefaultProfile resolves the active profile from defaults.profile (fallback: DefaultProfile)
func (c *Config) GetDefaultProfile() string {
	if c == nil {
		return DefaultProfile
	}
	if c.Defaults.Profile != "" {
		return c.Defaults.Profile
	}
	return DefaultProfile
}

// EffectiveDefaults merges system defaults <- global defaults <- profile defaults (operational only)
func (c *Config) EffectiveDefaults(profileName string) Defaults {
	res := Defaults{
		Window: DefaultWindow,
		Since:  DefaultSince,
		Limit:  DefaultLimit,
		Output: DefaultOutput,
	}

	if c == nil {
		return res
	}

	if c.Defaults.Service != "" {
		res.Service = c.Defaults.Service
	}
	if c.Defaults.Window != "" {
		res.Window = c.Defaults.Window
	}
	if c.Defaults.Since != "" {
		res.Since = c.Defaults.Since
	}
	if c.Defaults.Limit > 0 {
		res.Limit = c.Defaults.Limit
	}
	if c.Defaults.Output != "" {
		res.Output = c.Defaults.Output
	}

	if prof, exists := c.Profiles[profileName]; exists {
		if prof.Defaults.Service != "" {
			res.Service = prof.Defaults.Service
		}
		if prof.Defaults.Window != "" {
			res.Window = prof.Defaults.Window
		}
		if prof.Defaults.Since != "" {
			res.Since = prof.Defaults.Since
		}
		if prof.Defaults.Limit > 0 {
			res.Limit = prof.Defaults.Limit
		}
		if prof.Defaults.Output != "" {
			res.Output = prof.Defaults.Output
		}
	}

	return res
}

// DefaultConfig returns an initialized configuration with the 3 environment structures (prod by default).
// Target values that do not vary across environments live in Shared; profiles declare only what differs.
func DefaultConfig() *Config {
	return &Config{
		Version: "1.0",
		Defaults: Defaults{
			Profile: "prod",
			Service: "checkout",
			Window:  "1h",
			Since:   "1h",
			Limit:   15,
			Output:  "table",
		},
		Shared: SharedConfig{
			TargetConfig: TargetConfig{
				ExcludeSynthetic: BoolPtr(true),
				ExcludeProbes:    BoolPtr(true),
			},
			Thresholds: ProfileThresholds{
				LatencyWarnPct:     15.0,
				LatencyCritPct:     30.0,
				ErrorRateWarnDelta: 1.0,
				ErrorRateCritDelta: 3.0,
				MinSampleCalls:     5,
			},
		},
		Profiles: map[string]Profile{
			// Production inherits shared thresholds
			"prod": {
				Name: "Production",
			},
			"staging": {
				Name: "Staging",
				Defaults: Defaults{
					Window: "30m",
					Since:  "15m",
				},
				// Per-environment override of shared policy
				Thresholds: ProfileThresholds{
					LatencyWarnPct:     25.0,
					LatencyCritPct:     50.0,
					ErrorRateWarnDelta: 2.0,
					ErrorRateCritDelta: 5.0,
					MinSampleCalls:     5,
				},
			},
			"dev": {
				Name: "Development",
			},
		},
	}
}

// StarterConfigTemplate provides a commented, empty-value template for 'azlens config init'
const StarterConfigTemplate = `# AzLens Configuration (azlens.yaml)
# Docs: https://github.com/denjamio/azlens
#
# ARCHITECTURE & USAGE GUIDE:
# 1. Team-Shared Config: Commit this file to git. It is the single source of truth.
# 2. Multi-Tenancy: 'shared.logs.database' and an active service are mandatory to ensure
#    isolated telemetry queries across database and application telemetry.
# 3. Microservice Catalog: Declare services under 'shared.services' with their App Insights
#    cloud_RoleName ('role_name') and pod token base ('pod', without deployment hashes).
# 4. Targeting: Target any service using '-s <name>' / '--service <name>'. Defaults to 'defaults.service'.
# 5. Sessions: azlens leverages 'az' CLI authentication directly. No credentials are saved on disk.

version: "1.0"

# Operational defaults (inherited by all profiles)
defaults:
  profile: prod
  service: checkout
  window: "1h"
  since: "1h"
  limit: 15
  output: "table"

# ─── Shared target ────────────────────────────────────────────────────────
# Declare ONCE everything that does NOT vary across environments.
# Profiles below inherit every field here and only declare what differs
# (typically insights.name and logs.workspace_id).
shared:
  insights:
    resource_group: ""
    directory_id: ""
    subscription_id: ""
  logs:
    directory_id: ""
    subscription_id: ""
    database: ""
  services:
    checkout:
      role_name: checkout-service
      pod: checkout-service
  exclude_synthetic: true
  exclude_probes: true

  # Quality gate policy shared by every profile (per-profile overrides allowed)
  thresholds:
    p95_latency_warn_pct: 15.0
    p95_latency_crit_pct: 30.0
    error_rate_warn_delta: 1.0
    error_rate_crit_delta: 3.0
    min_sample_calls: 5

# Environment targets: only what differs per environment
# - insights.name: App Insights resource name or App ID GUID
# - logs.workspace_id: Log Analytics workspace Customer ID GUID
profiles:
  prod:
    name: "Production"
    insights:
      name: ""
    logs:
      workspace_id: ""

  staging:
    name: "Staging"
    insights:
      name: ""
    logs:
      workspace_id: ""

  dev:
    name: "Development"
    insights:
      name: ""
    logs:
      workspace_id: ""
`

// LoadConfig resolves and loads configuration from explicit path or default search paths
func LoadConfig(explicitPath string) (*Config, error) {
	if explicitPath != "" {
		if _, err := os.Stat(explicitPath); err != nil {
			return nil, fmt.Errorf("configuration file not found: %s", explicitPath)
		}
		return loadConfigFile(explicitPath)
	}

	// Search paths (convention over configuration): the team-shared, committed
	// azlens.yaml in the project root; a per-user global file comes last.
	// First match wins.
	paths := []string{"azlens.yaml"}
	home, err := os.UserHomeDir()
	if err == nil && home != "" {
		paths = append(paths, filepath.Join(home, ".config", "azlens", "azlens.yaml"))
	}

	for _, p := range paths {
		if _, err := os.Stat(p); err == nil {
			return loadConfigFile(p)
		}
	}

	fmt.Fprintln(os.Stderr, "⚠️  Warning: no configuration file found (azlens.yaml or ~/.config/azlens/azlens.yaml); using default placeholder config. Run 'azlens config init' to create one.")
	return DefaultConfig(), nil
}

// loadConfigFile reads and parses a single YAML configuration file
func loadConfigFile(path string) (*Config, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, fmt.Errorf("failed reading config from %s: %w", path, err)
	}

	var cfg Config
	if err := yaml.Unmarshal(data, &cfg); err != nil {
		return nil, fmt.Errorf("failed parsing YAML config from %s: %w", path, err)
	}
	cfg.LoadedPath = path
	return &cfg, nil
}

// GetProfile retrieves the requested profile (or default) with the shared target
// applied: every field the profile sets wins, shared fills the rest
func (c *Config) GetProfile(profileName string) (Profile, error) {
	if profileName == "" {
		profileName = c.GetDefaultProfile()
	}

	p, exists := c.Profiles[profileName]
	if !exists {
		return Profile{}, fmt.Errorf("profile '%s' not found in configuration (available: %v)", profileName, c.AvailableProfiles())
	}
	if p.Name == "" {
		p.Name = profileName
	}
	p.Target = MergeTarget(c.Shared.TargetConfig, p.Target)
	p.Thresholds = MergeThresholds(c.Shared.Thresholds, p.Thresholds)
	return p, nil
}

// AvailableProfiles lists all profile keys in deterministic (sorted) order
func (c *Config) AvailableProfiles() []string {
	keys := make([]string, 0, len(c.Profiles))
	for k := range c.Profiles {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	return keys
}
