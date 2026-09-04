// Package cmd implements the azlens command-line interface and its command suite.
package cmd

import (
	"context"
	"errors"
	"fmt"
	"os"
	"strings"
	"time"

	"github.com/fatih/color"
	"github.com/spf13/cobra"

	"github.com/denjamio/azlens/pkg/azure"
	"github.com/denjamio/azlens/pkg/config"
)

var (
	configPathFlag string
	profileFlag    string
	outputFlag     string
	colorModeFlag  string
	mockFlag       bool
	printQueryFlag bool
	roleFlag       []string
	podFlag        []string
)

// applyColorMode resolves the --color policy for this run. The default 'auto'
// keeps the fatih/color default behavior: colored output only when stdout is a
// terminal, honoring the NO_COLOR environment variable.
func applyColorMode(mode string) {
	switch strings.ToLower(strings.TrimSpace(mode)) {
	case "always":
		color.NoColor = false
	case "never":
		color.NoColor = true
	}
}

// defaultQueryTimeout is the default per-query budget; override with --query-timeout
const defaultQueryTimeout = 45 * time.Second

// splitCSVValues expands repeatable, comma-separated flag values into a clean list
func splitCSVValues(values []string) []string {
	var out []string
	for _, v := range values {
		for _, part := range strings.Split(v, ",") {
			if p := strings.TrimSpace(part); p != "" {
				out = append(out, p)
			}
		}
	}
	return out
}

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
		applyColorMode(colorModeFlag)

		// Only commands that actually interact with config or telemetry need config loading
		if !requiresConfig(cmd) {
			return nil
		}

		// 1. Load config file (azlens.yaml or ~/.config/azlens/azlens.yaml) — single source of truth
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

		// 3. Apply per-run target overrides (--role / --pod win over shared and profile config)
		if len(roleFlag) > 0 {
			prof.Target.Roles = splitCSVValues(roleFlag)
		}
		if len(podFlag) > 0 {
			prof.Target.Pods = splitCSVValues(podFlag)
		}

		// 4. Apply config defaults (output) when not explicitly set by flags
		effDefaults := cfg.EffectiveDefaults(activeProfileName)
		resolver := NewDefaultResolver(effDefaults)
		resolvedOutput := resolver.ResolveOutput(cmd, outputFlag)

		// 5. Pre-flight diagnostics on telemetry commands (top, deploy-check) when not running in offline mock mode
		if isTelemetryCommand(cmd) && !mockFlag {
			if err := runPreflightDiagnostics(prof); err != nil {
				return err
			}
		}

		// 6. Inject the runtime for the executing command
		cmd.SetContext(context.WithValue(cmd.Context(), runtimeContextKey{}, &appRuntime{
			Config:      cfg,
			ProfileName: activeProfileName,
			Profile:     prof,
			Client: azure.NewClient(azure.ClientOptions{
				Profile:    prof,
				IsMock:     mockFlag,
				PrintQuery: printQueryFlag,
				OnAuthRequired: func(tenant string) error {
					if isInteractiveTerminal() {
						return launchAzLogin(tenant)
					}
					return nil
				},
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
	RootCmd.PersistentFlags().StringVarP(&profileFlag, "profile", "p", "", "Profile to use (defined in azlens.yaml)")
	RootCmd.PersistentFlags().StringVarP(&outputFlag, "output", "o", config.DefaultOutput, "Output format (table, markdown, json)")
	RootCmd.PersistentFlags().StringVar(&colorModeFlag, "color", "auto", "Colorize output (auto, always, never)")
	RootCmd.PersistentFlags().BoolVar(&mockFlag, "mock", false, "Use mock/simulated telemetry data (no Azure connection needed)")
	RootCmd.PersistentFlags().BoolVarP(&printQueryFlag, "print-query", "q", false, "Print generated KQL query statements before executing")
	RootCmd.PersistentFlags().StringArrayVar(&roleFlag, "role", nil, "Override target.roles (App Insights cloud_RoleName) for this run; repeatable or comma-separated")
	RootCmd.PersistentFlags().StringArrayVar(&podFlag, "pod", nil, "Override target.pods (cloud_RoleInstance token) for this run; repeatable or comma-separated")
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

	_ = RootCmd.RegisterFlagCompletionFunc("color", func(cmd *cobra.Command, args []string, toComplete string) ([]string, cobra.ShellCompDirective) {
		return []string{"auto", "always", "never"}, cobra.ShellCompDirectiveNoFileComp
	})

	// --role / --pod complete from the config file first (instant), falling
	// back to live telemetry discovery: filters are picked, never typed
	_ = RootCmd.RegisterFlagCompletionFunc("role", completeTelemetryValue("role"))
	_ = RootCmd.RegisterFlagCompletionFunc("pod", completeTelemetryValue("pod"))
}
