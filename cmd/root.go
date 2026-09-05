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

	"github.com/denjamio/azlens/pkg/analysis"
	"github.com/denjamio/azlens/pkg/analysis/detectors"
	"github.com/denjamio/azlens/pkg/azure"
	"github.com/denjamio/azlens/pkg/config"
	"github.com/denjamio/azlens/pkg/domain"
	"github.com/denjamio/azlens/pkg/reporter"
	"github.com/denjamio/azlens/pkg/telemetry"
)

var (
	configPathFlag string
	profileFlag    string
	outputFlag     string
	colorModeFlag  string
	mockFlag       bool
	printQueryFlag bool
	serviceFlag    string
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
	return commandChain(cmd, "inspect", "deploy", "explain") || !cmd.HasParent()
}

// requiresConfig returns true if the command needs configuration loading and
// runtime injection. 'config init' and 'version' are exempt.
func requiresConfig(cmd *cobra.Command) bool {
	if cmd.Name() == "init" || cmd.Name() == "version" {
		return false
	}
	return isTelemetryCommand(cmd) || commandChain(cmd, "config", "doctor")
}

// RootCmd represents the base operational command (Section 6.1).
// Question answered: "Does anything need my attention right now?"
var RootCmd = &cobra.Command{
	Use:   "azlens [window]",
	Short: "AzLens - Actionable Observability & Operational CLI for Azure",
	Long: `AzLens answers one primary question:
  "Does this environment need my attention right now?"

It interprets telemetry and returns:
  problem -> impact -> likely cause -> evidence -> next action

Healthy signals are silent by default. Operational issues collapse
into clear, actionable stories with supporting evidence and next actions.`,
	SilenceUsage:  true,
	SilenceErrors: true,
	Args:          cobra.MaximumNArgs(1),
	RunE: func(cmd *cobra.Command, args []string) error {
		ctx, cancel := context.WithTimeout(cmd.Context(), queryTimeout)
		defer cancel()
		rt := runtimeFrom(cmd)

		windowArg := firstArg(args)
		start, end, err := rt.Resolver.ResolveWindow(windowArg)
		if err != nil {
			return fmt.Errorf("invalid time window: %w", err)
		}
		windowLabel := formatWindowLabel(start, end)

		builder := telemetry.NewSnapshotBuilder(rt.Client)
		snapshot, err := builder.BuildSnapshot(ctx, rt.ProfileName, rt.Profile, start, end, windowLabel)
		if err != nil && snapshot == nil {
			return err
		}

		detCfg := detectors.DefaultConfig()
		if rt.Profile.Thresholds.LatencyWarnPct > 0 {
			detCfg.LatencyWarnPct = rt.Profile.Thresholds.LatencyWarnPct
		}
		if rt.Profile.Thresholds.LatencyCritPct > 0 {
			detCfg.LatencyCritPct = rt.Profile.Thresholds.LatencyCritPct
		}
		if rt.Profile.Thresholds.MinSampleCalls > 0 {
			detCfg.MinSampleCalls = rt.Profile.Thresholds.MinSampleCalls
		}

		engine := analysis.NewEngine(detCfg)
		res := engine.Analyze(snapshot)

		out := cmd.OutOrStdout()
		if rt.Output == "json" {
			_ = reporter.PrintJSON(out, res)
		} else if rt.Output == "markdown" || rt.Output == "md" {
			reporter.PrintOperationalMarkdown(out, res)
		} else {
			reporter.PrintOperationalTerminal(out, res)
		}

		switch res.State {
		case domain.HealthStateDegraded:
			return newActionableProblemError("%d problem(s) need attention", len(res.Problems))
		case domain.HealthStateUnknown:
			return newUnknownHealthError("insufficient visibility to determine health")
		default:
			return nil
		}
	},
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

		// 2. Resolve active profile in exact precedence order (Section 4)
		activeProfileName, err := cfg.ResolveProfile(profileFlag)
		if err != nil && !mockFlag {
			return err
		}

		prof, profErr := cfg.GetProfile(activeProfileName)
		if profErr != nil && !mockFlag {
			return profErr
		}

		// 3. Apply config defaults (output) when not explicitly set by flags
		effDefaults := cfg.EffectiveDefaults(activeProfileName)
		resolver := NewDefaultResolver(effDefaults)
		resolvedOutput := resolver.ResolveOutput(cmd, outputFlag)

		// 4. Resolve active service: CLI flag --service > profile target.service > defaults.service
		targetService := serviceFlag
		if targetService == "" {
			targetService = prof.Target.Service
		}
		if targetService == "" {
			targetService = effDefaults.Service
		}
		prof.Target.Service = targetService

		// Resolve role and pod from declared services map or fallback ad-hoc
		if targetService != "" {
			if sDef, ok := prof.Target.Services[targetService]; ok {
				role := sDef.GetRoleName()
				if role == "" {
					role = targetService
				}
				prof.Target.RoleName = role
				prof.Target.Role = role

				if sDef.Pod != "" {
					prof.Target.Pod = sDef.Pod
				} else {
					prof.Target.Pod = targetService
				}
			} else {
				prof.Target.RoleName = targetService
				prof.Target.Role = targetService
				prof.Target.Pod = targetService
			}
		}

		// 5. Pre-flight diagnostics on telemetry commands when not running in offline mock mode
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
	RootCmd.AddGroup(&cobra.Group{
		ID:    "operational",
		Title: "Operational Commands:",
	})
	RootCmd.AddGroup(&cobra.Group{
		ID:    "supporting",
		Title: "Supporting Commands:",
	})

	RootCmd.Version = Version
	RootCmd.SetVersionTemplate(fmt.Sprintf("azlens version %s (commit: %s, built: %s)\n", Version, Commit, Date))

	RootCmd.PersistentFlags().StringVarP(&configPathFlag, "config", "c", "", "Path to azlens configuration file")
	RootCmd.PersistentFlags().StringVarP(&profileFlag, "profile", "p", "", "Profile to use (defined in azlens.yaml)")
	RootCmd.PersistentFlags().StringVarP(&outputFlag, "output", "o", config.DefaultOutput, "Output format (table, markdown, json)")
	RootCmd.PersistentFlags().StringVar(&colorModeFlag, "color", "auto", "Colorize output (auto, always, never)")
	RootCmd.PersistentFlags().BoolVar(&mockFlag, "mock", false, "Use mock/simulated telemetry data (no Azure connection needed)")
	RootCmd.PersistentFlags().BoolVarP(&printQueryFlag, "print-query", "q", false, "Print generated KQL query statements before executing")
	RootCmd.PersistentFlags().StringVarP(&serviceFlag, "service", "s", "", "Target service name defined in services or ad-hoc (sets role and pod filters)")
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

	_ = RootCmd.RegisterFlagCompletionFunc("service", completeServiceValue())
}
