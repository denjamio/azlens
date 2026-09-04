package cmd

import (
	"context"
	"fmt"
	"sort"
	"strings"
	"time"

	"github.com/spf13/cobra"

	"github.com/denjamio/azlens/pkg/analysis"
	"github.com/denjamio/azlens/pkg/analysis/detectors"
	"github.com/denjamio/azlens/pkg/domain"
	"github.com/denjamio/azlens/pkg/reporter"
	"github.com/denjamio/azlens/pkg/telemetry"
)

// explainCmd represents the explain command (Section 6.2).
// Question answered: "Why is this happening?"
var explainCmd = &cobra.Command{
	Use:   "explain [subject] [window]",
	Short: "Explain why an operational problem is happening and show supporting evidence",
	Long: `Explain provides root-cause analysis and factual evidence for an operational problem.
Without a subject, it explains the highest-priority current problem.
With a subject, it resolves the subject against current problems, configured services,
known endpoints, and known dependencies.

Exact matches win; ambiguous matches return deterministic candidates without guessing.`,
	Args:    cobra.MaximumNArgs(2),
	GroupID: "operational",
	RunE: func(cmd *cobra.Command, args []string) error {
		ctx, cancel := context.WithTimeout(cmd.Context(), queryTimeout)
		defer cancel()
		rt := runtimeFrom(cmd)

		var rawSubject string
		var windowArg string

		// Parse args: explain [subject] [window]
		// If 1 arg, check whether it looks like a duration ("1h", "30m", "24h") or a subject
		if len(args) == 1 {
			if isDurationString(args[0]) {
				windowArg = args[0]
			} else {
				rawSubject = args[0]
			}
		} else if len(args) == 2 {
			rawSubject = args[0]
			windowArg = args[1]
		}

		start, end, err := rt.Resolver.ResolveWindow(windowArg)
		if err != nil {
			return fmt.Errorf("invalid time window: %w", err)
		}
		windowLabel := formatWindowLabel(start, end)

		// 1. Fetch source-neutral telemetry snapshot
		builder := telemetry.NewSnapshotBuilder(rt.Client)
		snap, err := builder.BuildSnapshot(ctx, rt.ProfileName, rt.Profile, start, end, windowLabel)
		if err != nil && snap == nil {
			return err
		}

		// 2. Analyze snapshot
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
		res := engine.Analyze(snap)

		// 3. Resolve subject to target problem or component
		var configuredServices []string
		if rt.Profile.Target.Service != "" {
			configuredServices = append(configuredServices, rt.Profile.Target.Service)
		}
		if rt.Profile.Target.Role != "" && rt.Profile.Target.Role != rt.Profile.Target.Service {
			configuredServices = append(configuredServices, rt.Profile.Target.Role)
		}
		for s := range rt.Profile.Target.Services {
			if s != rt.Profile.Target.Service {
				configuredServices = append(configuredServices, s)
			}
		}
		targetProblem, resolvedSubject, err := resolveExplainSubject(rawSubject, res, snap, configuredServices)
		if err != nil {
			return err
		}

		// 4. Render output
		out := cmd.OutOrStdout()
		if rt.Output == "json" {
			return reporter.PrintJSON(out, res)
		} else if rt.Output == "markdown" || rt.Output == "md" {
			reporter.PrintExplainMarkdown(out, res, targetProblem, resolvedSubject)
			return nil
		}

		reporter.PrintExplainTerminal(out, res, targetProblem, resolvedSubject)
		return nil
	},
}

func isDurationString(s string) bool {
	s = strings.TrimSpace(s)
	if s == "" {
		return false
	}
	_, err := time.ParseDuration(s)
	return err == nil
}

func formatWindowLabel(start, end time.Time) string {
	diff := end.Sub(start).Round(time.Minute)
	return fmt.Sprintf("last %s", diff)
}

// resolveExplainSubject resolves subject in exact order (Section 6.2):
// 1. Current problems
// 2. Configured roles/services
// 3. Known endpoints
// 4. Known dependencies
// Exact matches win. Ambiguous matches return candidate list.
func resolveExplainSubject(
	rawSubject string,
	res *domain.AnalysisResult,
	snap *domain.Snapshot,
	configuredServices []string,
) (*domain.Problem, string, error) {
	subject := strings.TrimSpace(rawSubject)

	// Case 1: No subject given -> explain highest priority current problem
	if subject == "" {
		if len(res.Problems) > 0 {
			p := res.Problems[0]
			name := p.Scope.String()
			if name == "" {
				name = p.Summary
			}
			return &p, cleanSubject(name), nil
		}
		return nil, "", nil
	}

	lowerSubj := strings.ToLower(subject)

	// Collect candidates and exact matches
	candidatesMap := make(map[string]string) // name -> kind

	// 1. Check current problems
	var exactProblem *domain.Problem
	var exactName string

	for i := range res.Problems {
		p := &res.Problems[i]
		names := []string{p.Scope.Endpoint, p.Scope.Role, p.Scope.Target}
		if p.Cause != nil {
			names = append(names, p.Cause.Summary)
		}
		for _, n := range names {
			n = strings.TrimSpace(n)
			if n == "" {
				continue
			}
			clean := cleanSubject(n)
			if strings.EqualFold(n, subject) || strings.EqualFold(clean, subject) {
				exactProblem = p
				exactName = clean
				break
			}
			if strings.Contains(strings.ToLower(n), lowerSubj) || strings.Contains(strings.ToLower(clean), lowerSubj) {
				candidatesMap[n] = "problem"
			}
		}
		if exactProblem != nil {
			break
		}
	}
	if exactProblem != nil {
		return exactProblem, exactName, nil
	}

	// 2. Check configured services
	for _, r := range configuredServices {
		r = strings.TrimSpace(r)
		if r == "" {
			continue
		}
		if strings.EqualFold(r, subject) {
			return findProblemForScope(res.Problems, r), r, nil
		}
		if strings.Contains(strings.ToLower(r), lowerSubj) {
			candidatesMap[r] = "service"
		}
	}

	// 3. Check known endpoints
	for _, ep := range snap.CurrentEndpoints {
		name := strings.TrimSpace(ep.Name)
		if name == "" {
			continue
		}
		clean := cleanSubject(name)
		if strings.EqualFold(name, subject) || strings.EqualFold(clean, subject) {
			return findProblemForScope(res.Problems, name), clean, nil
		}
		if strings.Contains(strings.ToLower(name), lowerSubj) || strings.Contains(strings.ToLower(clean), lowerSubj) {
			candidatesMap[name] = "endpoint"
		}
	}

	// 4. Check known dependencies
	for _, dep := range snap.CurrentDependencies {
		target := strings.TrimSpace(dep.Target)
		if target != "" {
			if strings.EqualFold(target, subject) {
				return findProblemForScope(res.Problems, target), target, nil
			}
			if strings.Contains(strings.ToLower(target), lowerSubj) {
				candidatesMap[target] = "dependency"
			}
		}
		name := strings.TrimSpace(dep.Name)
		if name != "" {
			if strings.EqualFold(name, subject) {
				return findProblemForScope(res.Problems, name), name, nil
			}
			if strings.Contains(strings.ToLower(name), lowerSubj) {
				candidatesMap[name] = "dependency"
			}
		}
	}

	// Evaluate candidate matches
	if len(candidatesMap) > 1 {
		// Ambiguity! Deterministic candidate list
		candidates := make([]string, 0, len(candidatesMap))
		for c, kind := range candidatesMap {
			candidates = append(candidates, fmt.Sprintf("%s (%s)", c, kind))
		}
		sort.Strings(candidates)

		var sb strings.Builder
		sb.WriteString(fmt.Sprintf("%q matches:\n\n", subject))
		for _, c := range candidates {
			sb.WriteString(fmt.Sprintf("  %s\n", c))
		}
		sb.WriteString("\nBe more specific.")
		return nil, "", fmt.Errorf("%s", sb.String())
	}

	if len(candidatesMap) == 1 {
		for single := range candidatesMap {
			return findProblemForScope(res.Problems, single), cleanSubject(single), nil
		}
	}

	return nil, subject, fmt.Errorf("no problems, endpoints, or dependencies matching %q found", subject)
}

func findProblemForScope(problems []domain.Problem, name string) *domain.Problem {
	clean := cleanSubject(name)
	for i := range problems {
		p := &problems[i]
		if strings.EqualFold(p.Scope.Endpoint, name) ||
			strings.EqualFold(p.Scope.Role, name) ||
			strings.EqualFold(p.Scope.Target, name) ||
			strings.Contains(strings.ToLower(p.Summary), strings.ToLower(clean)) {
			return p
		}
		if p.Cause != nil && strings.EqualFold(p.Cause.Summary, name) {
			return p
		}
	}
	return nil
}

func cleanSubject(raw string) string {
	s := strings.TrimSpace(raw)
	parts := strings.Fields(s)
	if len(parts) >= 2 {
		s = parts[1]
	}
	s = strings.TrimPrefix(s, "/")
	if s == "" {
		return raw
	}
	return s
}

func init() {
	RootCmd.AddCommand(explainCmd)
}
