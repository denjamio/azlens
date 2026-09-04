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
	topLimit   int
	topDepType string
)

// resolveTopLimit resolves the row limit: CLI flag > config defaults > system default
func resolveTopLimit(cmd *cobra.Command) int {
	return runtimeFrom(cmd).Resolver.ResolveLimit(cmd, topLimit)
}

// topCmd represents the live triage command
var topCmd = &cobra.Command{
	Use:   "top [endpoints | queries | slow-logs | n-plus-one | breakdown | errors | deprecations] [duration]",
	Short: "Inspect top latency bottlenecks, slow queries, slow logs, N+1 queries, latency breakdown, errors, or deprecations",
	Long: `Quickly identify performance bottlenecks and system health metrics over a recent time window.

Subcommands:
  endpoints    - Slowest API endpoints and routes with latency percentiles (P50, P90, P95, P99)
  queries      - Slowest database queries (SQL, Redis, Cosmos DB) and external HTTP dependencies
  slow-logs    - Database engine slow query logs (MySqlSlowLogs in Log Analytics)
  n-plus-one   - Detect endpoints with excessive SQL calls per request (N+1 queries)
  breakdown    - Break down endpoint latency across Database, External APIs, Cache, and App Code
  errors       - Most frequent exceptions, HTTP 5xx errors, and affected endpoints
  deprecations - Grouped framework, language, and library deprecation warnings

Examples:
  # Show top 10 slowest endpoints in the last 1h
  azlens top endpoints 1h -n 10

  # Show slow database engine logs
  azlens top slow-logs 2h
  azlens top slow-logs 2h -o markdown

  # Detect N+1 queries across endpoints
  azlens top n-plus-one 1h

  # Break down where time is spent (DB vs Ext APIs vs App code)
  azlens top breakdown 2h
  azlens top breakdown -o markdown

  # Show top SQL queries in markdown
  azlens top queries -t SQL -o markdown

  # Show top exceptions in the last 24h
  azlens top errors 24h

  # Show framework deprecation warnings in the last 24h
  azlens top deprecations 24h`,
}

// resolveTopWindow resolves the triage time window: positional arg > config defaults > system default
func resolveTopWindow(cmd *cobra.Command, args []string) (time.Time, time.Time, error) {
	return runtimeFrom(cmd).Resolver.ResolveWindow(firstArg(args))
}

// runTopQuery is the shared skeleton for every 'top' subcommand: it resolves the
// time window, fetches telemetry through fetch, and renders the result in the
// configured output format (table | markdown | json)
func runTopQuery[T any](
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

	start, end, err := resolveTopWindow(cmd, args)
	if err != nil {
		return fmt.Errorf("invalid time window: %w", err)
	}

	data, err := fetch(ctx, start, end)
	if err != nil {
		return fmt.Errorf("failed fetching %s: %w", what, err)
	}

	return reporter.Render(os.Stdout, rt.Output, data, table, markdown)
}

var topEndpointsCmd = &cobra.Command{
	Use:   "endpoints [duration]",
	Short: "List slowest API endpoints and operations",
	Args:  cobra.MaximumNArgs(1),
	RunE: func(cmd *cobra.Command, args []string) error {
		limit := resolveTopLimit(cmd)
		return runTopQuery(cmd, args, "endpoint metrics",
			func(ctx context.Context, start, end time.Time) ([]model.RequestMetric, error) {
				return runtimeFrom(cmd).Client.QueryEndpoints(ctx, start, end, limit)
			},
			reporter.PrintRequestsTable, reporter.PrintRequestsMarkdown)
	},
}

var topQueriesCmd = &cobra.Command{
	Use:   "queries [duration]",
	Short: "Identify slowest database queries and external HTTP dependencies",
	Args:  cobra.MaximumNArgs(1),
	RunE: func(cmd *cobra.Command, args []string) error {
		limit := resolveTopLimit(cmd)
		return runTopQuery(cmd, args, "dependency metrics",
			func(ctx context.Context, start, end time.Time) ([]model.DependencyMetric, error) {
				return runtimeFrom(cmd).Client.QuerySlowDependencies(ctx, start, end, topDepType, limit)
			},
			reporter.PrintDependenciesTable, reporter.PrintDependenciesMarkdown)
	},
}

var topSlowLogsCmd = &cobra.Command{
	Use:   "slow-logs [duration]",
	Short: "Inspect database engine slow query logs (MySqlSlowLogs)",
	Args:  cobra.MaximumNArgs(1),
	RunE: func(cmd *cobra.Command, args []string) error {
		limit := resolveTopLimit(cmd)
		return runTopQuery(cmd, args, "slow query logs",
			func(ctx context.Context, start, end time.Time) (model.GenericQueryResult, error) {
				return runtimeFrom(cmd).Client.QueryMySQLSlowLogs(ctx, start, end, runtimeFrom(cmd).Profile.Target.Logs.Database, limit)
			},
			reporter.PrintGenericTable, reporter.PrintGenericMarkdown)
	},
}

var topErrorsCmd = &cobra.Command{
	Use:   "errors [duration]",
	Short: "Summarize and group top exceptions and HTTP 5xx errors",
	Args:  cobra.MaximumNArgs(1),
	RunE: func(cmd *cobra.Command, args []string) error {
		limit := resolveTopLimit(cmd)
		return runTopQuery(cmd, args, "exceptions",
			func(ctx context.Context, start, end time.Time) ([]model.ErrorSummary, error) {
				return runtimeFrom(cmd).Client.QueryExceptions(ctx, start, end, limit)
			},
			reporter.PrintErrorsTable, reporter.PrintErrorsMarkdown)
	},
}

var topNPlusOneCmd = &cobra.Command{
	Use:   "n-plus-one [duration]",
	Short: "Detect endpoints with excessive SQL calls per request (N+1 queries)",
	Args:  cobra.MaximumNArgs(1),
	RunE: func(cmd *cobra.Command, args []string) error {
		limit := resolveTopLimit(cmd)
		return runTopQuery(cmd, args, "n-plus-one metrics",
			func(ctx context.Context, start, end time.Time) ([]model.FanoutMetric, error) {
				return runtimeFrom(cmd).Client.QueryFanout(ctx, start, end, limit)
			},
			reporter.PrintFanoutTable, reporter.PrintFanoutMarkdown)
	},
}

var topBreakdownCmd = &cobra.Command{
	Use:   "breakdown [duration]",
	Short: "Break down endpoint latency across Database, External APIs, Cache, and App Code",
	Args:  cobra.MaximumNArgs(1),
	RunE: func(cmd *cobra.Command, args []string) error {
		limit := resolveTopLimit(cmd)
		return runTopQuery(cmd, args, "latency breakdown",
			func(ctx context.Context, start, end time.Time) ([]model.LatencyBreakdown, error) {
				return runtimeFrom(cmd).Client.QueryLatencyBreakdown(ctx, start, end, limit)
			},
			reporter.PrintLatencyBreakdownTable, reporter.PrintLatencyBreakdownMarkdown)
	},
}

var topDeprecationsCmd = &cobra.Command{
	Use:   "deprecations [duration]",
	Short: "Summarize and group framework, language, and library deprecation warnings",
	Args:  cobra.MaximumNArgs(1),
	RunE: func(cmd *cobra.Command, args []string) error {
		limit := resolveTopLimit(cmd)
		return runTopQuery(cmd, args, "deprecations",
			func(ctx context.Context, start, end time.Time) ([]model.DeprecationSummary, error) {
				return runtimeFrom(cmd).Client.QueryDeprecations(ctx, start, end, limit)
			},
			reporter.PrintDeprecationsTable, reporter.PrintDeprecationsMarkdown)
	},
}

func init() {
	topCmd.PersistentFlags().IntVarP(&topLimit, "limit", "n", config.DefaultLimit, "Number of items to return")

	topQueriesCmd.Flags().StringVarP(&topDepType, "type", "t", "all", "Dependency type filter ('SQL', 'HTTP', 'Redis', 'Cosmos', 'all')")

	_ = topQueriesCmd.RegisterFlagCompletionFunc("type", func(cmd *cobra.Command, args []string, toComplete string) ([]string, cobra.ShellCompDirective) {
		return []string{"SQL", "HTTP", "Redis", "Cosmos", "all"}, cobra.ShellCompDirectiveNoFileComp
	})

	topCmd.AddCommand(topEndpointsCmd)
	topCmd.AddCommand(topQueriesCmd)
	topCmd.AddCommand(topSlowLogsCmd)
	topCmd.AddCommand(topNPlusOneCmd)
	topCmd.AddCommand(topBreakdownCmd)
	topCmd.AddCommand(topErrorsCmd)
	topCmd.AddCommand(topDeprecationsCmd)

	RootCmd.AddCommand(topCmd)
}
