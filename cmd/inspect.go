package cmd

import (
	"context"
	"fmt"
	"io"
	"os"
	"time"

	"github.com/spf13/cobra"

	"github.com/denjamio/azlens/pkg/config"
	"github.com/denjamio/azlens/pkg/model"
	"github.com/denjamio/azlens/pkg/reporter"
)

var (
	inspectLimit   int
	inspectDepType string
)

// resolveInspectLimit resolves the row limit: CLI flag > config defaults > system default
func resolveInspectLimit(cmd *cobra.Command) int {
	return runtimeFrom(cmd).Resolver.ResolveLimit(cmd, inspectLimit)
}

// resolveInspectWindow resolves the triage time window: positional arg > config defaults > system default
func resolveInspectWindow(cmd *cobra.Command, args []string) (time.Time, time.Time, error) {
	return runtimeFrom(cmd).Resolver.ResolveWindow(firstArg(args))
}

// runInspectQuery is the shared skeleton for every inspect subcommand: it resolves the
// time window, fetches telemetry through fetch, and renders the result in the
// configured output format (table | markdown | json)
func runInspectQuery[T any](
	cmd *cobra.Command,
	args []string,
	what string,
	fetch func(ctx context.Context, start, end time.Time) (T, error),
	table func(io.Writer, T),
	markdown func(io.Writer, T),
) error {
	ctx, cancel := context.WithTimeout(cmd.Context(), queryTimeout)
	defer cancel()
	rt := runtimeFrom(cmd)

	start, end, err := resolveInspectWindow(cmd, args)
	if err != nil {
		return fmt.Errorf("invalid time window: %w", err)
	}

	data, err := fetch(ctx, start, end)
	if err != nil {
		return fmt.Errorf("failed fetching %s: %w", what, err)
	}

	return reporter.Render(os.Stdout, rt.Output, data, table, markdown)
}

// inspectCmd represents the inspect command (Section 6.3).
// Question answered: "Show me the evidence."
var inspectCmd = &cobra.Command{
	Use:   "inspect <view> [window]",
	Short: "Inspect operational evidence: endpoints, dependencies, slow-queries, n-plus-one, breakdown, errors, deprecations",
	Long: `Inspect provides direct operational visibility into the underlying telemetry
driving AzLens analysis. Views represent inspectable operational components, ordered
by operational impact rather than raw metrics alone.

Views:
  endpoints     - API endpoints and routes with latency percentiles (P50, P90, P95, P99) and error rates
  dependencies  - External services, databases, Redis, and HTTP dependency calls
  slow-queries  - Database engine slow query logs ordered by duration descending (aliases: queries, slow-logs)
  n-plus-one    - Detect endpoints with excessive SQL calls per request (N+1 queries)
  breakdown     - Endpoint latency breakdown across Database, External APIs, Cache, and App Code
  errors        - Grouped exceptions, HTTP 5xx errors, and affected endpoints
  deprecations  - Grouped framework, language, and library deprecation warnings`,
	GroupID: "operational",
}

var inspectEndpointsCmd = &cobra.Command{
	Use:   "endpoints [duration]",
	Short: "Inspect endpoint traffic, latency percentiles (P50, P90, P95, P99), and error rates",
	Args:  cobra.MaximumNArgs(1),
	RunE: func(cmd *cobra.Command, args []string) error {
		limit := resolveInspectLimit(cmd)
		return runInspectQuery(cmd, args, "endpoint metrics",
			func(ctx context.Context, start, end time.Time) ([]model.RequestMetric, error) {
				return runtimeFrom(cmd).Client.QueryEndpoints(ctx, start, end, limit)
			},
			reporter.PrintRequestsTable, reporter.PrintRequestsMarkdown)
	},
}

var inspectDependenciesCmd = &cobra.Command{
	Use:   "dependencies [duration]",
	Short: "Inspect external dependencies (HTTP, SQL, Redis, Cosmos DB) by latency and error impact",
	Args:  cobra.MaximumNArgs(1),
	RunE: func(cmd *cobra.Command, args []string) error {
		limit := resolveInspectLimit(cmd)
		return runInspectQuery(cmd, args, "dependency metrics",
			func(ctx context.Context, start, end time.Time) ([]model.DependencyMetric, error) {
				return runtimeFrom(cmd).Client.QuerySlowDependencies(ctx, start, end, inspectDepType, limit)
			},
			reporter.PrintDependenciesTable, reporter.PrintDependenciesMarkdown)
	},
}

var inspectSlowQueriesCmd = &cobra.Command{
	Use:     "slow-queries [duration]",
	Aliases: []string{"queries", "slow-logs"},
	Short:   "Inspect database engine slow query logs (MySqlSlowLogs)",
	Long: `Inspect database engine slow query logs (MySqlSlowLogs in Log Analytics).
Displays individual slow query log executions ordered by execution duration descending.`,
	Args: cobra.MaximumNArgs(1),
	RunE: func(cmd *cobra.Command, args []string) error {
		limit := resolveInspectLimit(cmd)
		rt := runtimeFrom(cmd)
		dbName := rt.Profile.Target.Logs.Database
		return runInspectQuery(cmd, args, "slow query logs",
			func(ctx context.Context, start, end time.Time) ([]model.SlowLogEntry, error) {
				return rt.Client.QueryMySQLSlowLogs(ctx, start, end, dbName, limit)
			},
			reporter.PrintSlowLogsTable, reporter.PrintSlowLogsMarkdown)
	},
}

var inspectNPlusOneCmd = &cobra.Command{
	Use:   "n-plus-one [duration]",
	Short: "Detect endpoints with excessive SQL calls per request (N+1 queries)",
	Args:  cobra.MaximumNArgs(1),
	RunE: func(cmd *cobra.Command, args []string) error {
		limit := resolveInspectLimit(cmd)
		return runInspectQuery(cmd, args, "n-plus-one metrics",
			func(ctx context.Context, start, end time.Time) ([]model.FanoutMetric, error) {
				return runtimeFrom(cmd).Client.QueryFanout(ctx, start, end, limit)
			},
			reporter.PrintFanoutTable, reporter.PrintFanoutMarkdown)
	},
}

var inspectBreakdownCmd = &cobra.Command{
	Use:   "breakdown [duration]",
	Short: "Break down endpoint latency across Database, External APIs, Cache, and App Code",
	Args:  cobra.MaximumNArgs(1),
	RunE: func(cmd *cobra.Command, args []string) error {
		limit := resolveInspectLimit(cmd)
		return runInspectQuery(cmd, args, "latency breakdown",
			func(ctx context.Context, start, end time.Time) ([]model.LatencyBreakdown, error) {
				return runtimeFrom(cmd).Client.QueryLatencyBreakdown(ctx, start, end, limit)
			},
			reporter.PrintLatencyBreakdownTable, reporter.PrintLatencyBreakdownMarkdown)
	},
}

var inspectErrorsCmd = &cobra.Command{
	Use:   "errors [duration]",
	Short: "Inspect grouped exceptions, HTTP 5xx errors, and affected endpoints",
	Args:  cobra.MaximumNArgs(1),
	RunE: func(cmd *cobra.Command, args []string) error {
		limit := resolveInspectLimit(cmd)
		return runInspectQuery(cmd, args, "exceptions",
			func(ctx context.Context, start, end time.Time) ([]model.ErrorSummary, error) {
				return runtimeFrom(cmd).Client.QueryExceptions(ctx, start, end, limit)
			},
			reporter.PrintErrorsTable, reporter.PrintErrorsMarkdown)
	},
}

var inspectDeprecationsCmd = &cobra.Command{
	Use:   "deprecations [duration]",
	Short: "Summarize and group framework, language, and library deprecation warnings",
	Args:  cobra.MaximumNArgs(1),
	RunE: func(cmd *cobra.Command, args []string) error {
		limit := resolveInspectLimit(cmd)
		return runInspectQuery(cmd, args, "deprecations",
			func(ctx context.Context, start, end time.Time) ([]model.DeprecationSummary, error) {
				return runtimeFrom(cmd).Client.QueryDeprecations(ctx, start, end, limit)
			},
			reporter.PrintDeprecationsTable, reporter.PrintDeprecationsMarkdown)
	},
}

func init() {
	inspectCmd.PersistentFlags().IntVarP(&inspectLimit, "limit", "n", config.DefaultLimit, "Number of items to return")

	inspectDependenciesCmd.Flags().StringVarP(&inspectDepType, "type", "t", "all", "Dependency type filter ('SQL', 'HTTP', 'Redis', 'Cosmos', 'all')")
	_ = inspectDependenciesCmd.RegisterFlagCompletionFunc("type", func(cmd *cobra.Command, args []string, toComplete string) ([]string, cobra.ShellCompDirective) {
		return []string{"SQL", "HTTP", "Redis", "Cosmos", "all"}, cobra.ShellCompDirectiveNoFileComp
	})

	inspectCmd.AddCommand(inspectEndpointsCmd)
	inspectCmd.AddCommand(inspectDependenciesCmd)
	inspectCmd.AddCommand(inspectSlowQueriesCmd)
	inspectCmd.AddCommand(inspectNPlusOneCmd)
	inspectCmd.AddCommand(inspectBreakdownCmd)
	inspectCmd.AddCommand(inspectErrorsCmd)
	inspectCmd.AddCommand(inspectDeprecationsCmd)

	RootCmd.AddCommand(inspectCmd)
}
