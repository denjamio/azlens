package cmd

import (
	"time"

	"github.com/spf13/cobra"

	"github.com/denjamio/azlens/pkg/config"
)

// DefaultResolver centralizes the resolution of CLI overrides with config-level
// and system-level defaults across all commands (output, limit, window, since).
// System fallbacks live in pkg/config (single source of truth).
type DefaultResolver struct {
	defaults config.Defaults
}

// NewDefaultResolver creates a resolver bound to the effective configuration defaults
func NewDefaultResolver(defaults config.Defaults) *DefaultResolver {
	return &DefaultResolver{defaults: defaults}
}

// ResolveOutput resolves output format: CLI flag > EffectiveDefaults > "table"
func (r *DefaultResolver) ResolveOutput(cmd *cobra.Command, flagValue string) string {
	if cmd != nil {
		if f := cmd.Flags().Lookup("output"); f != nil && f.Changed {
			return flagValue
		}
	}
	if r.defaults.Output != "" {
		return r.defaults.Output
	}
	if flagValue != "" {
		return flagValue
	}
	return config.DefaultOutput
}

// ResolveLimit resolves limit: CLI flag > EffectiveDefaults > 15
func (r *DefaultResolver) ResolveLimit(cmd *cobra.Command, flagValue int) int {
	if cmd != nil {
		if f := cmd.Flag("limit"); f != nil && f.Changed {
			return flagValue
		}
	}
	if r.defaults.Limit > 0 {
		return r.defaults.Limit
	}
	if flagValue > 0 {
		return flagValue
	}
	return config.DefaultLimit
}

// ResolveWindow resolves a top time window: positional arg > EffectiveDefaults.Window > config.DefaultWindow
func (r *DefaultResolver) ResolveWindow(arg string) (time.Time, time.Time, error) {
	windowStr := arg
	if windowStr == "" {
		windowStr = r.defaults.Window
	}
	if windowStr == "" {
		windowStr = config.DefaultWindow
	}
	return parseDurationWindow(windowStr)
}

// ResolveSince resolves deploy comparison duration: positional arg > EffectiveDefaults.Since > config.DefaultSince
func (r *DefaultResolver) ResolveSince(arg string) (time.Duration, error) {
	durStr := arg
	if durStr == "" {
		durStr = r.defaults.Since
	}
	if durStr == "" {
		durStr = config.DefaultSince
	}
	return parseRelativeDuration(durStr)
}
