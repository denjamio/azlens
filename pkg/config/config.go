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

// Defaults defines operational preferences (active profile, timeframes, limits, output).
// These are inherited by profiles when not explicitly overridden.
type Defaults struct {
	Profile string `yaml:"profile,omitempty" json:"profile,omitempty"` // Default active profile name (e.g. "prod", "staging")
	Window  string `yaml:"window,omitempty" json:"window,omitempty"`   // Default timeframe for 'top' (e.g. "1h", "30m")
	Since   string `yaml:"since,omitempty" json:"since,omitempty"`     // Default deploy diff comparison window (e.g. "1h", "30m")
	Limit   int    `yaml:"limit,omitempty" json:"limit,omitempty"`     // Default number of items returned (--limit / -n)
	Output  string `yaml:"output,omitempty" json:"output,omitempty"`   // Default output format (table, markdown, json)
}

// InsightsConfig holds configuration for Application Insights (mapping only, no scalar)
type InsightsConfig struct {
	Name         string `yaml:"name,omitempty" json:"name,omitempty"`                 // Resource name or App ID
	Subscription string `yaml:"subscription,omitempty" json:"subscription,omitempty"` // Subscription ID hosting Application Insights (routes the query; az resolves the directory from it)
}

// LogsConfig holds configuration for Log Analytics (mapping only, no scalar)
type LogsConfig struct {
	WorkspaceID  string `yaml:"workspace_id,omitempty" json:"workspace_id,omitempty"` // Log Analytics workspace Customer ID (GUID), not the resource name
	Subscription string `yaml:"subscription,omitempty" json:"subscription,omitempty"` // Subscription ID hosting Log Analytics (routes the query; az resolves the directory from it)
	Namespace    string `yaml:"namespace,omitempty" json:"namespace,omitempty"`       // Kubernetes namespace (PodNamespace / Kubernetes.Namespace dimensions)
	Database     string `yaml:"database,omitempty" json:"database,omitempty"`         // Database name for MySqlSlowLogs filtering
}

// BoolPtr returns a pointer to b. Target boolean filters use pointers so that
// nil means "not set here" (inherit from shared) while an explicit value can
// override the shared configuration.
func BoolPtr(b bool) *bool { return &b }

// StringList accepts either a scalar string or a YAML sequence of strings, so
// both `roles: order-service` and `roles: [order-service, billing-service]` are
// valid. An empty scalar or an empty list means "not set" (inherit from shared).
type StringList []string

func (s *StringList) UnmarshalYAML(value *yaml.Node) error {
	switch value.Kind {
	case yaml.ScalarNode:
		var str string
		if err := value.Decode(&str); err != nil {
			return err
		}
		if strings.TrimSpace(str) == "" {
			*s = nil
			return nil
		}
		*s = StringList{str}
		return nil
	case yaml.SequenceNode:
		var items []string
		if err := value.Decode(&items); err != nil {
			return err
		}
		list := make(StringList, 0, len(items))
		for _, item := range items {
			if trimmed := strings.TrimSpace(item); trimmed != "" {
				list = append(list, trimmed)
			}
		}
		if len(list) == 0 {
			*s = nil
			return nil
		}
		*s = list
		return nil
	default:
		return fmt.Errorf("expected a string or a list of strings")
	}
}

// TargetConfig encapsulates telemetry destination and filter criteria.
// DHH: Un concepto, un nombre, un lugar.
//
// Filter reference (what each option filters in KQL):
//   - roles             -> cloud_RoleName          (EXACT microservice names, one per service; single =~ / multiple in~)
//   - pods              -> cloud_RoleInstance      (pod names WITHOUT the deployment hash; token match: single has / multiple has_any)
//   - logs.namespace    -> customDimensions['Kubernetes.Namespace'] / PodNamespace
//   - logs.database     -> Db / DatabaseName_s     (MySqlSlowLogs in Log Analytics)
//   - resource_id       -> _ResourceId             (Log Analytics multi-resource workspaces)
//   - exclude_synthetic -> operation_SyntheticSource / syntheticSource
//   - exclude_probes    -> kube-probe User-Agent + /healthz-style routes
//   - custom_dimensions -> customDimensions['<key>'] =~ '<value>'
type TargetConfig struct {
	Insights         InsightsConfig    `yaml:"insights,omitempty" json:"insights,omitempty"`
	Logs             LogsConfig        `yaml:"logs,omitempty" json:"logs,omitempty"`
	Roles            StringList        `yaml:"roles,omitempty" json:"roles,omitempty"`                         // App Insights cloud_RoleName(s): EXACT microservice names (scalar or list)
	Pods             StringList        `yaml:"pods,omitempty" json:"pods,omitempty"`                           // App Insights cloud_RoleInstance token(s): pod names WITHOUT the deployment hash (scalar or list)
	ResourceID       string            `yaml:"resource_id,omitempty" json:"resource_id,omitempty"`             // Azure Resource ID (_ResourceId)
	ExcludeSynthetic *bool             `yaml:"exclude_synthetic,omitempty" json:"exclude_synthetic,omitempty"` // Filter out synthetic traffic / availability tests (nil = inherit)
	ExcludeProbes    *bool             `yaml:"exclude_probes,omitempty" json:"exclude_probes,omitempty"`       // Exclude /healthz, /ready, kube-probe requests (nil = inherit)
	CustomDimensions map[string]string `yaml:"custom_dimensions,omitempty" json:"custom_dimensions,omitempty"` // Custom key-value pairs
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
	if override.Insights.Subscription != "" {
		merged.Insights.Subscription = override.Insights.Subscription
	}
	if override.Logs.WorkspaceID != "" {
		merged.Logs.WorkspaceID = override.Logs.WorkspaceID
	}
	if override.Logs.Subscription != "" {
		merged.Logs.Subscription = override.Logs.Subscription
	}
	if len(override.Roles) > 0 {
		merged.Roles = override.Roles
	}
	if len(override.Pods) > 0 {
		merged.Pods = override.Pods
	}
	if override.Logs.Namespace != "" {
		merged.Logs.Namespace = override.Logs.Namespace
	}
	if override.Logs.Database != "" {
		merged.Logs.Database = override.Logs.Database
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

// Profile represents a specific project or environment telemetry configuration
type Profile struct {
	Name       string            `yaml:"name,omitempty" json:"name,omitempty"`
	Target     TargetConfig      `yaml:"target,omitempty" json:"target,omitempty"`
	Defaults   Defaults          `yaml:"defaults,omitempty" json:"defaults,omitempty"` // Per-profile operational overrides (window, since, limit, output)
	Thresholds ProfileThresholds `yaml:"thresholds,omitempty" json:"thresholds,omitempty"`
}

// ProfileThresholds defines regression limits for the profile
type ProfileThresholds struct {
	LatencyWarnPct     float64 `yaml:"p95_latency_warn_pct,omitempty" json:"p95_latency_warn_pct,omitempty"`
	LatencyCritPct     float64 `yaml:"p95_latency_crit_pct,omitempty" json:"p95_latency_crit_pct,omitempty"`
	ErrorRateWarnDelta float64 `yaml:"error_rate_warn_delta,omitempty" json:"error_rate_warn_delta,omitempty"`
	ErrorRateCritDelta float64 `yaml:"error_rate_crit_delta,omitempty" json:"error_rate_crit_delta,omitempty"`
	MinSampleCalls     int64   `yaml:"min_sample_calls,omitempty" json:"min_sample_calls,omitempty"` // Ignore noise on endpoints with fewer calls than this (default: 5)
}

// Config represents the top-level azlens configuration file (azlens.yaml)
type Config struct {
	Version    string             `yaml:"version" json:"version"`
	Defaults   Defaults           `yaml:"defaults,omitempty" json:"defaults,omitempty"`
	Shared     TargetConfig       `yaml:"shared,omitempty" json:"shared,omitempty"` // Target values inherited by every profile: declare once what does not vary across environments
	Profiles   map[string]Profile `yaml:"profiles" json:"profiles"`
	LoadedPath string             `yaml:"-" json:"-"` // Where the config was resolved from
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
			Window:  "1h",
			Since:   "1h",
			Limit:   15,
			Output:  "table",
		},
		Shared: TargetConfig{
			ExcludeSynthetic: BoolPtr(true),
			ExcludeProbes:    BoolPtr(true),
		},
		Profiles: map[string]Profile{
			"prod": {
				Name: "Production",
				Thresholds: ProfileThresholds{
					LatencyWarnPct:     15.0,
					LatencyCritPct:     30.0,
					ErrorRateWarnDelta: 1.0,
					ErrorRateCritDelta: 3.0,
					MinSampleCalls:     5,
				},
			},
			"staging": {
				Name: "Staging",
				Defaults: Defaults{
					Window: "30m",
					Since:  "15m",
				},
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
const StarterConfigTemplate = `# AzLens Configuration
# Docs: https://github.com/denjamio/azlens
#
# This file (azlens.yaml) is meant to be COMMITTED and shared by the team.
version: "1.0"

# Operational defaults (inherited by all profiles)
defaults:
  profile: prod
  window: "1h"
  since: "1h"
  limit: 15
  output: "table"

# ─── Shared target ────────────────────────────────────────────────────────
# Declare ONCE everything that does NOT vary across environments.
# Profiles below inherit every field here and only declare what differs
# (typically insights.name and logs.workspace_id). Any shared field can be
# overridden per profile when needed.
#
# Sessions: azlens verifies that every configured subscription is available in
# the current az session and launches 'az login' for you when it is not
# (tokens stay entirely inside the az CLI — nothing is stored by azlens).
shared:
  insights:
    subscription: "" # App Insights subscription ID (routes the query; az resolves the directory from it)
  logs:
    subscription: "" # Log Analytics subscription ID (routes the query; az resolves the directory from it)
    namespace: ""    # Kubernetes namespace (e.g. ecommerce)
    database: ""     # Database name for slow query logs (MySqlSlowLogs)
  roles: []         # cloud_RoleName(s): EXACT microservice names — scalar or list (empty = all services)
  pods: []          # Pod names WITHOUT the deployment hash, token-matched — scalar or list (empty = all pods)
  exclude_synthetic: true
  exclude_probes: true

# Environment targets: only what differs per environment
profiles:
  prod:
    name: "Production"
    target:
      insights:
        name: ""         # App Insights resource name (e.g. app-prod)
      logs:
        workspace_id: "" # Log Analytics workspace Customer ID GUID
    # Optional per-environment overrides of any shared field:
    #   roles: [billing-service, returns-service]  # isolate specific microservices
    #   pods: order-service
    thresholds:
      p95_latency_warn_pct: 15.0
      p95_latency_crit_pct: 30.0
      error_rate_warn_delta: 1.0
      error_rate_crit_delta: 3.0
      min_sample_calls: 5

  staging:
    name: "Staging"
    target:
      insights:
        name: ""
      logs:
        workspace_id: ""
    thresholds:
      p95_latency_warn_pct: 25.0
      p95_latency_crit_pct: 50.0
      error_rate_warn_delta: 2.0
      error_rate_crit_delta: 5.0
      min_sample_calls: 5

  dev:
    name: "Development"
    target:
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
	p.Target = MergeTarget(c.Shared, p.Target)
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
