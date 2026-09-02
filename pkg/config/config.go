// Package config implements the azlens configuration schema, loading,
// profile resolution, and validation with actionable diagnostics.
package config

import (
	"fmt"
	"os"
	"path/filepath"
	"sort"

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
	Subscription string `yaml:"subscription,omitempty" json:"subscription,omitempty"` // Subscription ID for Application Insights
	Tenant       string `yaml:"tenant,omitempty" json:"tenant,omitempty"`             // Optional Entra ID Tenant / Directory ID
}

// LogsConfig holds configuration for Log Analytics (mapping only, no scalar)
type LogsConfig struct {
	WorkspaceID  string `yaml:"workspace_id,omitempty" json:"workspace_id,omitempty"` // Log Analytics workspace Customer ID (GUID), not the resource name
	Subscription string `yaml:"subscription,omitempty" json:"subscription,omitempty"` // Subscription ID for Log Analytics
	Tenant       string `yaml:"tenant,omitempty" json:"tenant,omitempty"`             // Optional Entra ID Tenant / Directory ID
}

// TargetConfig encapsulates telemetry destination and filter criteria for a profile.
// DHH: Un concepto, un nombre, un lugar.
type TargetConfig struct {
	Insights         InsightsConfig    `yaml:"insights,omitempty" json:"insights,omitempty"`
	Logs             LogsConfig        `yaml:"logs,omitempty" json:"logs,omitempty"`
	Role             string            `yaml:"role,omitempty" json:"role,omitempty"`                           // App Insights cloud_RoleName (microservice isolation)
	Pod              string            `yaml:"pod,omitempty" json:"pod,omitempty"`                             // Pod name match (ContainerLogV2 / cloud_RoleInstance)
	Namespace        string            `yaml:"namespace,omitempty" json:"namespace,omitempty"`                 // Kubernetes namespace
	Database         string            `yaml:"database,omitempty" json:"database,omitempty"`                   // Target database name for slow query logs
	ResourceID       string            `yaml:"resource_id,omitempty" json:"resource_id,omitempty"`             // Azure Resource ID (_ResourceId)
	ExcludeSynthetic bool              `yaml:"exclude_synthetic,omitempty" json:"exclude_synthetic,omitempty"` // Filter out synthetic traffic / availability tests
	ExcludeProbes    bool              `yaml:"exclude_probes,omitempty" json:"exclude_probes,omitempty"`       // Exclude /healthz, /ready, kube-probe requests
	CustomDimensions map[string]string `yaml:"custom_dimensions,omitempty" json:"custom_dimensions,omitempty"` // Custom key-value pairs
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

// Config represents the top-level azlens configuration file (.azlens.yaml)
type Config struct {
	Version    string             `yaml:"version" json:"version"`
	Defaults   Defaults           `yaml:"defaults,omitempty" json:"defaults,omitempty"`
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

// DefaultConfig returns an initialized configuration with the 3 environment structures (prod by default)
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
		Profiles: map[string]Profile{
			"prod": {
				Name: "Production",
				Target: TargetConfig{
					ExcludeSynthetic: true,
					ExcludeProbes:    true,
				},
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
				Target: TargetConfig{
					ExcludeSynthetic: true,
					ExcludeProbes:    true,
				},
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
				Target: TargetConfig{
					ExcludeSynthetic: true,
					ExcludeProbes:    true,
				},
			},
		},
	}
}

// StarterConfigTemplate provides a commented, empty-value template for 'azlens config init'
const StarterConfigTemplate = `# AzLens Configuration
# Docs: https://github.com/denjamio/azlens
version: "1.0"

# Operational defaults (inherited by all profiles)
defaults:
  profile: prod
  window: "1h"
  since: "1h"
  limit: 15
  output: "table"

# Environment targets: one concept, one name, one place
profiles:
  prod:
    name: "Production"
    target:
      insights:
        name: ""         # App Insights resource name or App ID (e.g. app-prod)
        subscription: "" # Optional subscription ID (if different from default az account)
        tenant: ""       # Optional directory/tenant ID
      logs:
        workspace_id: "" # Log Analytics workspace Customer ID GUID (for slow logs & container logs)
        subscription: "" # Optional subscription ID
        tenant: ""       # Optional directory/tenant ID
      role: ""           # cloud_RoleName (microservice isolation, e.g. order-service)
      pod: ""            # Pod / instance name token (e.g. order-service)
      namespace: ""      # Kubernetes namespace (e.g. ecommerce)
      database: ""       # Database name for slow query logs
      exclude_synthetic: true
      exclude_probes: true
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
        subscription: ""
        tenant: ""
      logs:
        workspace_id: ""
        subscription: ""
        tenant: ""
      role: ""
      pod: ""
      namespace: ""
      database: ""
      exclude_synthetic: true
      exclude_probes: true
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
        subscription: ""
        tenant: ""
      logs:
        workspace_id: ""
        subscription: ""
        tenant: ""
      role: ""
      pod: ""
      namespace: ""
      database: ""
      exclude_synthetic: true
      exclude_probes: true
`

// LoadConfig resolves and loads configuration from explicit path or default search paths
func LoadConfig(explicitPath string) (*Config, error) {
	if explicitPath != "" {
		if _, err := os.Stat(explicitPath); err != nil {
			return nil, fmt.Errorf("configuration file not found: %s", explicitPath)
		}
		return loadConfigFile(explicitPath)
	}

	// Search paths (convention over configuration): 2 paths only
	paths := []string{".azlens.yaml"}
	home, err := os.UserHomeDir()
	if err == nil && home != "" {
		paths = append(paths, filepath.Join(home, ".config", "azlens", "azlens.yaml"))
	}

	for _, p := range paths {
		if _, err := os.Stat(p); err == nil {
			return loadConfigFile(p)
		}
	}

	fmt.Fprintln(os.Stderr, "⚠️  Warning: no configuration file found (.azlens.yaml or ~/.config/azlens/azlens.yaml); using default placeholder config. Run 'azlens config init' to create one.")
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

// GetProfile retrieves the requested profile (or default)
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
