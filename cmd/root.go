// Package cmd implements the azlens command-line interface and its command suite.
package cmd

import (
	"context"
	"errors"
	"fmt"
	"os"
	"time"

	"github.com/spf13/cobra"

	"github.com/denjamio/azlens/pkg/azure"
	"github.com/denjamio/azlens/pkg/config"
)

var (
	configPathFlag string
	profileFlag    string
	outputFlag     string
	mockFlag       bool
	printQueryFlag bool
)

// defaultQueryTimeout is the default per-query budget; override with --query-timeout
const defaultQueryTimeout = 45 * time.Second

// queryTimeout is the per-query budget for Azure telemetry queries, bound to
// the hidden --query-timeout flag (useful for large windows on slow workspaces)
var queryTimeout = defaultQueryTimeout

// appRuntime carries the resolved configuration, active profile, and client
// dependencies for the executing command. It is built once in PersistentPreRunE
// and injected through the command context, keeping the package free of global state.
type appRuntime struct {
	Config            *config.Config
	ProfileName       string
	Profile           config.Profile
	Client            azure.AzureClient
	Output            string
	EffectiveDefaults config.Defaults
	Resolver          *DefaultResolver
}

type runtimeContextKey struct{}

// runtimeFrom retrieves the app runtime injected by PersistentPreRunE.
// It panics when the runtime is missing (direct RunE invocation or a new
// command missing from requiresConfig): a silent empty fallback would only
// hide the wiring bug.
func runtimeFrom(cmd *cobra.Command) *appRuntime {
	if rt, ok := cmd.Context().Value(runtimeContextKey{}).(*appRuntime); ok {
		return rt
	}
	panic("azlens: command runtime not injected; execute through RootCmd and include the command in requiresConfig")
}

// commandChain reports whether cmd or any of its ancestors matches one of the given names
func commandChain(cmd *cobra.Command, names ...string) bool {
	for curr := cmd; curr != nil && curr.HasParent(); curr = curr.Parent() {
		for _, name := range names {
			if curr.Name() == name {
				return true
			}
		}
	}
	return false
}

// isTelemetryCommand reports whether the command queries live Azure telemetry
func isTelemetryCommand(cmd *cobra.Command) bool {
	return commandChain(cmd, "top", "deploy-check")
}

// requiresConfig returns true if the command needs configuration loading and
// runtime injection. 'config init' is exempt: it must work when no config exists yet.
func requiresConfig(cmd *cobra.Command) bool {
	if cmd.Name() == "init" {
		return false
	}
	return isTelemetryCommand(cmd) || commandChain(cmd, "config", "doctor")
}

// RootCmd represents the base command when called without any subcommands
var RootCmd = &cobra.Command{
	Use:   "azlens",
	Short: "AzLens - Actionable Observability & Deploy Regressions Analyzer for Azure",
	Long: `AzLens is a high-performance CLI tool that interrogates Azure Monitor,
Application Insights, and Log Analytics to deliver actionable telemetry insights:
  * Detect latency percentiles (P50, P90, P95, P99) and error regressions before vs after deploy
  * Identify top slow requests, database queries, and external dependencies
  * Group exceptions and detect new error signatures introduced in releases
  * Manage multiple project/environment profiles with project and pod scoped safe KQL`,
	SilenceUsage:  true,
	SilenceErrors: true,
	PersistentPreRunE: func(cmd *cobra.Command, args []string) error {
		// Only commands that actually interact with config or telemetry need config loading
		if !requiresConfig(cmd) {
			return nil
		}

		// 1. Load config file (.azlens.yaml, ~/.config/azlens/azlens.yaml) — single source of truth
		cfg, err := config.LoadConfig(configPathFlag)
		if err != nil {
			return fmt.Errorf("failed loading configuration: %w", err)
		}

		// 2. Resolve active profile (defaults.profile or --profile/-p)
		activeProfileName := cfg.GetDefaultProfile()
		if profileFlag != "" {
			activeProfileName = profileFlag
		}

		prof, profErr := cfg.GetProfile(activeProfileName)
		if profErr != nil && !mockFlag {
			return profErr
		}

		// 3. Apply config defaults (output) when not explicitly set by flags
		effDefaults := cfg.EffectiveDefaults(activeProfileName)
		resolver := NewDefaultResolver(effDefaults)
		resolvedOutput := resolver.ResolveOutput(cmd, outputFlag)

		// 4. Pre-flight diagnostics on telemetry commands (top, deploy-check) when not running in offline mock mode
		if isTelemetryCommand(cmd) && !mockFlag {
			if err := runPreflightDiagnostics(prof); err != nil {
				return err
			}
		}

		// 5. Inject the runtime for the executing command
		cmd.SetContext(context.WithValue(cmd.Context(), runtimeContextKey{}, &appRuntime{
			Config:      cfg,
			ProfileName: activeProfileName,
			Profile:     prof,
			Client: azure.NewClient(azure.ClientOptions{
				Profile:    prof,
				IsMock:     mockFlag,
				PrintQuery: printQueryFlag,
			}),
			Output:            resolvedOutput,
			EffectiveDefaults: effDefaults,
			Resolver:          resolver,
		}))

		return nil
	},
}

func Execute() {
	err := RootCmd.Execute()
	if err == nil {
		return
	}

	fmt.Fprintf(os.Stderr, "Error: %v\n", err)

	exitCode := 1
	var ec *ExitCodeError
	if errors.As(err, &ec) {
		exitCode = ec.Code
	}
	os.Exit(exitCode)
}

func init() {
	RootCmd.Version = Version
	RootCmd.SetVersionTemplate(fmt.Sprintf("azlens version %s (commit: %s, built: %s)\n", Version, Commit, Date))

	RootCmd.PersistentFlags().StringVarP(&configPathFlag, "config", "c", "", "Path to azlens configuration file")
	RootCmd.PersistentFlags().StringVarP(&profileFlag, "profile", "p", "", "Profile to use (defined in .azlens.yaml)")
	RootCmd.PersistentFlags().StringVarP(&outputFlag, "output", "o", config.DefaultOutput, "Output format (table, markdown, json)")
	RootCmd.PersistentFlags().BoolVar(&mockFlag, "mock", false, "Use mock/simulated telemetry data (no Azure connection needed)")
	RootCmd.PersistentFlags().BoolVarP(&printQueryFlag, "print-query", "q", false, "Print generated KQL query statements before executing")
	RootCmd.PersistentFlags().DurationVar(&queryTimeout, "query-timeout", defaultQueryTimeout, "Per-query timeout budget (e.g. 30s, 2m)")
	_ = RootCmd.PersistentFlags().MarkHidden("query-timeout")

	_ = RootCmd.RegisterFlagCompletionFunc("profile", func(cmd *cobra.Command, args []string, toComplete string) ([]string, cobra.ShellCompDirective) {
		cfg, err := config.LoadConfig(configPathFlag)
		if err != nil || cfg == nil {
			return nil, cobra.ShellCompDirectiveNoFileComp
		}
		return cfg.AvailableProfiles(), cobra.ShellCompDirectiveNoFileComp
	})

	_ = RootCmd.RegisterFlagCompletionFunc("output", func(cmd *cobra.Command, args []string, toComplete string) ([]string, cobra.ShellCompDirective) {
		return []string{"table", "markdown", "json"}, cobra.ShellCompDirectiveNoFileComp
	})
}
