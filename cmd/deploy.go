package cmd

import (
	"context"
	"fmt"
	"sync"
	"time"

	"github.com/spf13/cobra"

	"github.com/denjamio/azlens/pkg/analysis"
	"github.com/denjamio/azlens/pkg/analysis/detectors"
	"github.com/denjamio/azlens/pkg/azure"
	"github.com/denjamio/azlens/pkg/domain"
	"github.com/denjamio/azlens/pkg/model"
	"github.com/denjamio/azlens/pkg/reporter"
)

var (
	deployAtTimeFlag string
)

// deployCmd represents the deploy command (Section 6.4).
// Question answered: "Did this deployment make things worse?"
var deployCmd = &cobra.Command{
	Use:   "deploy [duration] [--at TIME]",
	Short: "Verify whether a deployment introduced performance regressions, errors, or risks",
	Long: `Analyze telemetry metrics across two equal time windows (before vs after deploy)
to verify whether a release degraded endpoints, introduced new exceptions, or caused
operational regressions.

Exit codes:
  0 - This deploy looks safe (no meaningful regression)
  1 - Command or query execution failure
  2 - Meaningful production regression detected
  3 - Comparison data is insufficient to verify safety (unknown)`,
	Args:    cobra.MaximumNArgs(1),
	GroupID: "operational",
	RunE: func(cmd *cobra.Command, args []string) error {
		ctx, cancel := context.WithTimeout(cmd.Context(), queryTimeout)
		defer cancel()
		rt := runtimeFrom(cmd)

		now := time.Now()
		dur, err := rt.Resolver.ResolveSince(firstArg(args))
		if err != nil {
			return fmt.Errorf("invalid duration: %w", err)
		}

		var (
			baseStart time.Time
			baseEnd   time.Time
			currStart time.Time
			currEnd   time.Time
			timeLabel string
		)

		if deployAtTimeFlag != "" {
			deployTime, err := parseDeployTime(deployAtTimeFlag, now)
			if err != nil {
				return err
			}
			baseEnd = deployTime
			baseStart = deployTime.Add(-dur)
			currStart = deployTime
			currEnd = deployTime.Add(dur)
			if currEnd.After(now) {
				currEnd = now
			}
			timeLabel = deployTime.Format("15:04")
		} else {
			currEnd = now
			currStart = now.Add(-dur)
			baseEnd = currStart
			baseStart = currStart.Add(-dur)
			timeLabel = currStart.Format("15:04")
		}

		// Fetch baseline and post-deploy telemetry in parallel
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
				cancelFetch()
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

		if currErr != nil {
			return fmt.Errorf("telemetry query failed: %w", currErr)
		}

		// Build domain Snapshot
		snapshot := domain.NewSnapshot(
			domain.ProfileContext{
				Name:        rt.ProfileName,
				DisplayName: rt.Profile.Name,
			},
			domain.Scope{
				Role:      firstOrEmpty(rt.Profile.Target.Roles),
				Pod:       firstOrEmpty(rt.Profile.Target.Pods),
				Namespace: rt.Profile.Target.Logs.Namespace,
			},
			domain.WindowContext{
				Label:    fmt.Sprintf("deploy at %s", timeLabel),
				Duration: dur.Round(time.Minute).String(),
				Start:    currStart,
				End:      currEnd,
			},
		)

		snapshot.BaselineOverall = &baseWM.Overall
		snapshot.BaselineEndpoints = baseWM.Endpoints
		snapshot.BaselineDependencies = baseWM.Deps
		snapshot.BaselineExceptions = baseWM.Errors
		snapshot.BaselineFanout = baseWM.Fanout

		snapshot.CurrentOverall = currWM.Overall
		snapshot.CurrentEndpoints = currWM.Endpoints
		snapshot.CurrentDependencies = currWM.Deps
		snapshot.CurrentExceptions = currWM.Errors
		snapshot.CurrentFanout = currWM.Fanout

		// Scenario J: Insufficient baseline period
		minRequiredCalls := rt.Profile.Thresholds.MinSampleCalls
		if minRequiredCalls <= 0 {
			minRequiredCalls = 5
		}
		if baseErr != nil || baseWM.Overall.TotalCalls < minRequiredCalls {
			snapshot.CapabilityStates[domain.CapabilityRequests] = domain.CapabilityStateUnavailable
		}

		// Analyze
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

		// If baseline was insufficient, mark unknown explicitly (Scenario J)
		if baseErr != nil || baseWM.Overall.TotalCalls < minRequiredCalls {
			res.State = domain.HealthStateUnknown
			res.StatusMessage = "Baseline period has insufficient samples to determine safety."
		}

		// Render
		out := cmd.OutOrStdout()
		if rt.Output == "json" {
			_ = reporter.PrintJSON(out, res)
		} else if rt.Output == "markdown" || rt.Output == "md" {
			reporter.PrintDeployMarkdown(out, res, timeLabel)
		} else {
			reporter.PrintDeployTerminal(out, res, timeLabel)
		}

		// Exit code handling (Section 19)
		switch res.State {
		case domain.HealthStateUnknown:
			return &ExitCodeError{Code: ExitCodeInsufficient, Err: fmt.Errorf("insufficient comparison data")}
		case domain.HealthStateDegraded:
			return &ExitCodeError{Code: ExitCodeActionable, Err: fmt.Errorf("meaningful regression detected")}
		default:
			return nil
		}
	},
}

func firstOrEmpty(list []string) string {
	if len(list) > 0 {
		return list[0]
	}
	return ""
}

func init() {
	deployCmd.Flags().StringVarP(&deployAtTimeFlag, "at", "a", "", "Deployment time to center comparison around (e.g. '14:30', '-20m')")

	RootCmd.AddCommand(deployCmd)
}
