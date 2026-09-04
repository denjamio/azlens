package cmd

import (
	"context"
	"fmt"
	"io"
	"os"
	"time"

	"github.com/fatih/color"
	"github.com/spf13/cobra"

	"github.com/denjamio/azlens/pkg/config"
	"github.com/denjamio/azlens/pkg/domain"
	"github.com/denjamio/azlens/pkg/model"
	"github.com/denjamio/azlens/pkg/reporter"
	"github.com/denjamio/azlens/pkg/telemetry"
)

var (
	inspectLimit   int
	inspectDepType string
)

// inspectCmd represents the inspect command (Section 6.3).
// Question answered: "Show me the evidence."
var inspectCmd = &cobra.Command{
	Use:   "inspect <view> [window]",
	Short: "Inspect operational evidence: endpoints, dependencies, queries, errors, or runtime",
	Long: `Inspect provides direct operational visibility into the underlying telemetry
driving AzLens analysis. Views represent inspectable operational things, ordered
by operational impact rather than raw metrics alone.

Views:
  endpoints     - API endpoints and routes with latency percentiles and error rates
  dependencies  - External services, databases, Redis, and HTTP dependency calls
  queries       - Database queries and slow log statements
  errors        - Grouped exceptions, HTTP 5xx errors, and affected endpoints
  runtime       - Workload availability, pod restarts, OOM kills, and saturation`,
	GroupID: "operational",
}

var inspectEndpointsCmd = &cobra.Command{
	Use:   "endpoints [duration]",
	Short: "Inspect endpoint traffic, latency percentiles (P50, P90, P95, P99), and error rates",
	Args:  cobra.MaximumNArgs(1),
	RunE: func(cmd *cobra.Command, args []string) error {
		limit := resolveInspectLimit(cmd)
		return runTopQuery(cmd, args, "endpoint metrics",
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
		return runTopQuery(cmd, args, "dependency metrics",
			func(ctx context.Context, start, end time.Time) ([]model.DependencyMetric, error) {
				return runtimeFrom(cmd).Client.QuerySlowDependencies(ctx, start, end, inspectDepType, limit)
			},
			reporter.PrintDependenciesTable, reporter.PrintDependenciesMarkdown)
	},
}

var inspectQueriesCmd = &cobra.Command{
	Use:   "queries [duration]",
	Short: "Inspect database queries and slow logs by latency impact",
	Args:  cobra.MaximumNArgs(1),
	RunE: func(cmd *cobra.Command, args []string) error {
		limit := resolveInspectLimit(cmd)
		rt := runtimeFrom(cmd)
		if rt.Profile.Target.Logs.Database != "" {
			return runTopQuery(cmd, args, "slow query logs",
				func(ctx context.Context, start, end time.Time) ([]model.SlowLogGroup, error) {
					return rt.Client.QueryMySQLSlowLogsGrouped(ctx, start, end, rt.Profile.Target.Logs.Database, limit)
				},
				reporter.PrintSlowLogsGroupTable, reporter.PrintSlowLogsGroupMarkdown)
		}
		return runTopQuery(cmd, args, "database queries",
			func(ctx context.Context, start, end time.Time) ([]model.DependencyMetric, error) {
				return rt.Client.QuerySlowDependencies(ctx, start, end, "SQL", limit)
			},
			reporter.PrintDependenciesTable, reporter.PrintDependenciesMarkdown)
	},
}

var inspectErrorsCmd = &cobra.Command{
	Use:   "errors [duration]",
	Short: "Inspect grouped exceptions, HTTP 5xx errors, and affected endpoints",
	Args:  cobra.MaximumNArgs(1),
	RunE: func(cmd *cobra.Command, args []string) error {
		limit := resolveInspectLimit(cmd)
		return runTopQuery(cmd, args, "exceptions",
			func(ctx context.Context, start, end time.Time) ([]model.ErrorSummary, error) {
				return runtimeFrom(cmd).Client.QueryExceptions(ctx, start, end, limit)
			},
			reporter.PrintErrorsTable, reporter.PrintErrorsMarkdown)
	},
}

var inspectRuntimeCmd = &cobra.Command{
	Use:   "runtime [duration]",
	Short: "Inspect workload availability, pod restarts, OOM kills, and resource saturation",
	Args:  cobra.MaximumNArgs(1),
	RunE: func(cmd *cobra.Command, args []string) error {
		ctx, cancel := context.WithTimeout(cmd.Context(), queryTimeout)
		defer cancel()
		rt := runtimeFrom(cmd)

		start, end, err := resolveTopWindow(cmd, args)
		if err != nil {
			return fmt.Errorf("invalid time window: %w", err)
		}

		builder := telemetry.NewSnapshotBuilder(rt.Client)
		snap, err := builder.BuildSnapshot(ctx, rt.ProfileName, rt.Profile, start, end, "runtime")
		if err != nil && snap == nil {
			return err
		}

		return reporter.Render(os.Stdout, rt.Output, snap,
			printRuntimeTable, printRuntimeMarkdown)
	},
}

func printRuntimeTable(w io.Writer, snap *domain.Snapshot) {
	if len(snap.Workloads) == 0 && len(snap.Pods) == 0 {
		fmt.Fprintln(w, color.GreenString("✓ Workloads and runtime components are healthy (no restarts or OOM kills)."))
		return
	}

	table := reporter.NewTable(w, []string{"Workload", "Ready", "Desired", "Restarts", "OOM Kills", "Status"},
		[]int{reporter.AlignLeft, reporter.AlignRight, reporter.AlignRight, reporter.AlignRight, reporter.AlignRight, reporter.AlignLeft})

	for _, wl := range snap.Workloads {
		status := "Ready"
		if wl.CrashLooping {
			status = "CrashLoopBackOff"
		} else if wl.ReadyReplicas < wl.DesiredReplicas {
			status = "Degraded"
		}
		table.Append([]string{
			wl.Name,
			fmt.Sprintf("%d", wl.ReadyReplicas),
			fmt.Sprintf("%d", wl.DesiredReplicas),
			fmt.Sprintf("%d", wl.Restarts),
			fmt.Sprintf("%d", wl.OOMKills),
			status,
		})
	}
	table.Render()
}

func printRuntimeMarkdown(w io.Writer, snap *domain.Snapshot) {
	if len(snap.Workloads) == 0 && len(snap.Pods) == 0 {
		fmt.Fprintln(w, "Workloads and runtime components are healthy.")
		return
	}

	fmt.Fprintln(w, "| Workload | Ready | Desired | Restarts | OOM Kills | Status |")
	fmt.Fprintln(w, "| --- | --- | --- | --- | --- | --- |")
	for _, wl := range snap.Workloads {
		status := "Ready"
		if wl.CrashLooping {
			status = "CrashLoopBackOff"
		} else if wl.ReadyReplicas < wl.DesiredReplicas {
			status = "Degraded"
		}
		fmt.Fprintf(w, "| %s | %d | %d | %d | %d | %s |\n",
			wl.Name, wl.ReadyReplicas, wl.DesiredReplicas, wl.Restarts, wl.OOMKills, status)
	}
}

func resolveInspectLimit(cmd *cobra.Command) int {
	return runtimeFrom(cmd).Resolver.ResolveLimit(cmd, inspectLimit)
}

func init() {
	inspectCmd.PersistentFlags().IntVarP(&inspectLimit, "limit", "n", config.DefaultLimit, "Number of items to return")
	inspectDependenciesCmd.Flags().StringVarP(&inspectDepType, "type", "t", "all", "Dependency type filter ('SQL', 'HTTP', 'Redis', 'Cosmos', 'all')")

	inspectCmd.AddCommand(inspectEndpointsCmd)
	inspectCmd.AddCommand(inspectDependenciesCmd)
	inspectCmd.AddCommand(inspectQueriesCmd)
	inspectCmd.AddCommand(inspectErrorsCmd)
	inspectCmd.AddCommand(inspectRuntimeCmd)

	RootCmd.AddCommand(inspectCmd)
}
