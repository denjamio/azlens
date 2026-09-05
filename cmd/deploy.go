package cmd

import (
	"context"
	"fmt"
	"time"

	"github.com/spf13/cobra"

	"github.com/denjamio/azlens/pkg/domain"
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
			currStart time.Time
			currEnd   time.Time
			timeLabel string
		)

		if deployAtTimeFlag != "" {
			deployTime, err := parseDeployTime(deployAtTimeFlag, now)
			if err != nil {
				return err
			}
			currStart = deployTime
			currEnd = deployTime.Add(dur)
			if currEnd.After(now) {
				currEnd = now
			}
			timeLabel = deployTime.Format("15:04")
		} else {
			currEnd = now
			currStart = now.Add(-dur)
			timeLabel = currStart.Format("15:04")
		}

		pipeRes, err := runAnalysisPipeline(ctx, rt, currStart, currEnd, fmt.Sprintf("deploy at %s", timeLabel))
		if err != nil {
			return err
		}
		snapshot := pipeRes.Snapshot
		res := pipeRes.Analysis

		// Scenario J: Insufficient baseline period or failed baseline query
		minRequiredCalls := rt.Profile.Thresholds.MinSampleCalls
		if minRequiredCalls <= 0 {
			minRequiredCalls = 10
		}
		insufficientBaseline := snapshot.BaselineOverall == nil || snapshot.BaselineOverall.TotalCalls < minRequiredCalls || snapshot.QueryErrors[domain.CapabilityRequests] != nil
		if insufficientBaseline {
			snapshot.CapabilityStates[domain.CapabilityRequests] = domain.CapabilityStateUnavailable
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

func init() {
	deployCmd.Flags().StringVarP(&deployAtTimeFlag, "at", "a", "", "Deployment time to center comparison around (e.g. '14:30', '-20m')")

	RootCmd.AddCommand(deployCmd)
}
