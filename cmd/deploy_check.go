package cmd

import (
	"context"
	"fmt"
	"os"
	"sync"
	"time"

	"github.com/spf13/cobra"

	"github.com/denjamio/azlens/pkg/analyzer"
	"github.com/denjamio/azlens/pkg/azure"
	"github.com/denjamio/azlens/pkg/config"
	"github.com/denjamio/azlens/pkg/model"
	"github.com/denjamio/azlens/pkg/reporter"
)

var (
	deployAtTime string
)

// deployCheckCmd represents the deploy-check command (deploy regressions quality gate)
var deployCheckCmd = &cobra.Command{
	Use:   "deploy-check [duration]",
	Short: "Check telemetry before vs after deploy to detect latency regressions, N+1 queries, and errors",
	Long: `Analyze telemetry metrics across two equal time windows (baseline vs post-deploy)
to automatically detect performance regressions, N+1 database queries, slow SQL, and newly introduced errors.

Examples:
  # Check last 1h vs previous 1h (default or explicit)
  azlens deploy-check
  azlens deploy-check 1h

  # Compare 30m before vs 30m after deploy at 14:30 today
  azlens deploy-check 30m --at 14:30

  # Compare windows centered around deploy that completed 20 minutes ago
  azlens deploy-check 30m --at -20m

  # Generate Markdown report for GitHub PR / Slack
  azlens deploy-check 30m -p staging -o markdown

  # Offline demo mode (no Azure connection needed)
  azlens deploy-check --mock

Exit codes: 0 = no critical regressions, 1 = error, 2 = critical regressions detected (quality gate failure).`,
	Args: cobra.MaximumNArgs(1),
	RunE: func(cmd *cobra.Command, args []string) error {
		ctx, cancel := context.WithTimeout(cmd.Context(), queryTimeout)
		defer cancel()
		rt := runtimeFrom(cmd)
		prof := rt.Profile

		now := time.Now()

		// Resolve duration: Positional arg > Profile/Global defaults > "1h"
		dur, err := rt.Resolver.ResolveSince(firstArg(args))
		if err != nil {
			return fmt.Errorf("invalid duration: %w", err)
		}

		var (
			baseStart time.Time
			baseEnd   time.Time
			currStart time.Time
			currEnd   time.Time
			baseLabel string
			currLabel string
		)

		if deployAtTime != "" {
			deployTime, err := parseDeployTime(deployAtTime, now)
			if err != nil {
				return err
			}

			baseEnd = deployTime
			baseStart = deployTime.Add(-dur)
			currStart = deployTime
			currEnd = deployTime.Add(dur)
			if currEnd.After(now) {
				postDur := now.Sub(currStart).Round(time.Second)
				fmt.Fprintf(os.Stderr, "⚠️  Warning: Post-deploy window is truncated to %s (current time) vs %s baseline. Asymmetric window sizes may cause skewed comparisons.\n", postDur, dur)
				currEnd = now
			}

			baseLabel = fmt.Sprintf("Baseline (Pre-Deploy %s - %s)", baseStart.Format("15:04"), baseEnd.Format("15:04"))
			currLabel = fmt.Sprintf("Post-Deploy (since %s)", currStart.Format("15:04"))
		} else {
			currEnd = now
			currStart = now.Add(-dur)
			baseEnd = currStart
			baseStart = currStart.Add(-dur)

			baseLabel = "Baseline (Pre-Deploy)"
			currLabel = "Post-Deploy"
		}

		baseWin := model.TimeWindow{Start: baseStart, End: baseEnd, Label: baseLabel}
		currWin := model.TimeWindow{Start: currStart, End: currEnd, Label: currLabel}

		// High-performance batched fetch: one KQL request per window (5 queries each),
		// both windows in parallel with fast-fail cancellation
		fetchCtx, cancelFetch := context.WithCancel(ctx)
		defer cancelFetch()

		var (
			wg      sync.WaitGroup
			baseWM  model.WindowMetrics
			currWM  model.WindowMetrics
			baseErr error
			currErr error
		)

		wg.Add(2)
		go func() {
			defer wg.Done()
			baseCtx := azure.WithBaseline(fetchCtx)
			baseWM, baseErr = rt.Client.QueryWindowMetrics(baseCtx, baseStart, baseEnd, 30)
			if baseErr != nil {
				cancelFetch() // Fast-fail: cancel the sibling window immediately
			}
		}()
		go func() {
			defer wg.Done()
			currWM, currErr = rt.Client.QueryWindowMetrics(fetchCtx, currStart, currEnd, 30)
			if currErr != nil {
				cancelFetch()
			}
		}()
		wg.Wait()

		if baseErr != nil {
			return fmt.Errorf("baseline telemetry query failed: %w", baseErr)
		}
		if currErr != nil {
			return fmt.Errorf("post-deploy telemetry query failed: %w", currErr)
		}

		diffReport := analyzer.Compare(analyzer.CompareOptions{
			AppName:        appName(prof),
			BaselineWindow: baseWin,
			CurrentWindow:  currWin,
			BaseReqOverall: baseWM.Overall,
			CurrReqOverall: currWM.Overall,
			BaseEndpoints:  baseWM.Endpoints,
			CurrEndpoints:  currWM.Endpoints,
			BaseDeps:       baseWM.Deps,
			CurrDeps:       currWM.Deps,
			BaseErrors:     baseWM.Errors,
			CurrErrors:     currWM.Errors,
			BaseFanout:     baseWM.Fanout,
			CurrFanout:     currWM.Fanout,
			Thresholds:     resolveThresholds(prof.Thresholds),
		})

		if err := reporter.Render(os.Stdout, rt.Output, diffReport,
			reporter.PrintDiffTerminal, reporter.PrintDiffMarkdown); err != nil {
			return err
		}

		// Quality gate: fail the pipeline when critical regressions are detected
		if diffReport.OverallVerdict == model.SeverityCritical {
			return newQualityGateError("deploy-check quality gate failed: critical regressions detected in '%s'", diffReport.AppName)
		}

		return nil
	},
}

// appName resolves the display name for the report
func appName(prof config.Profile) string {
	if prof.Name != "" {
		return prof.Name
	}
	if prof.Target.Insights.Name != "" {
		return prof.Target.Insights.Name
	}
	return "Azure App Telemetry"
}

// resolveThresholds merges profile-configured thresholds on top of the analyzer
// defaults (zero values keep the default)
func resolveThresholds(pt config.ProfileThresholds) analyzer.RegressionThresholds {
	t := analyzer.DefaultThresholds()
	if pt.LatencyWarnPct > 0 {
		t.LatencyWarnPct = pt.LatencyWarnPct
	}
	if pt.LatencyCritPct > 0 {
		t.LatencyCritPct = pt.LatencyCritPct
	}
	if pt.ErrorRateWarnDelta > 0 {
		t.ErrorRateWarnDelta = pt.ErrorRateWarnDelta
	}
	if pt.ErrorRateCritDelta > 0 {
		t.ErrorRateCritDelta = pt.ErrorRateCritDelta
	}
	if pt.MinSampleCalls > 0 {
		t.MinSampleCalls = pt.MinSampleCalls
	}
	return t
}

func init() {
	deployCheckCmd.Flags().StringVarP(&deployAtTime, "at", "a", "", "Deployment time to center comparison around (e.g. '14:30', '2026-09-03T14:30:00Z', '-20m')")

	RootCmd.AddCommand(deployCheckCmd)
}
