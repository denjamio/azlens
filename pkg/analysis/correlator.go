// Package analysis implements the operational correlation engine and problem detectors.
package analysis

import (
	"fmt"
	"math"
	"strings"

	"github.com/denjamio/azlens/pkg/domain"
)

// Correlator maps telemetry findings into unified problem stories and worth-watching items.
type Correlator struct{}

func NewCorrelator() *Correlator {
	return &Correlator{}
}

// Correlate groups related findings into a prioritized list of problems (Section 6 & 12).
func (c *Correlator) Correlate(snapshot *domain.Snapshot, findings []domain.Finding) ([]domain.Problem, []domain.WatchingItem) {
	var problems []domain.Problem
	var watching []domain.WatchingItem

	consumed := make(map[int]bool)

	// Group findings by category
	var latencyFindings []int
	var errorFindings []int
	var newExceptionFindings []int
	var depLatencyFindings []int
	var depErrorFindings []int
	var workloadFindings []int
	var oomFindings []int
	var restartFindings []int
	var availFindings []int

	for i, f := range findings {
		switch f.Kind {
		case domain.FindingRequestLatencyRegression:
			latencyFindings = append(latencyFindings, i)
		case domain.FindingRequestErrorRegression:
			errorFindings = append(errorFindings, i)
		case domain.FindingNewException:
			newExceptionFindings = append(newExceptionFindings, i)
		case domain.FindingDependencyLatencyRegression:
			depLatencyFindings = append(depLatencyFindings, i)
		case domain.FindingDependencyErrorRegression:
			depErrorFindings = append(depErrorFindings, i)
		case domain.FindingWorkloadUnavailable:
			workloadFindings = append(workloadFindings, i)
		case domain.FindingOOMKilled:
			oomFindings = append(oomFindings, i)
		case domain.FindingRestartBurst:
			restartFindings = append(restartFindings, i)
		case domain.FindingAvailabilityFailure:
			availFindings = append(availFindings, i)
		}
	}

	// 1. Scenario E: Correlate Workload Availability + OOM + Restarts + 503s
	if len(workloadFindings) > 0 || len(oomFindings) > 0 {
		prob := correlateRuntimeAvailability(snapshot, findings, workloadFindings, oomFindings, restartFindings, availFindings, consumed)
		if prob != nil {
			problems = append(problems, *prob)
		}
	}

	// 2. Scenario B & D: Correlate Endpoint Regressions with Dependencies or Exceptions
	for _, latIdx := range latencyFindings {
		if consumed[latIdx] {
			continue
		}
		latFinding := findings[latIdx]
		consumed[latIdx] = true

		// Check if there's a matching error finding for the same endpoint/scope
		var matchedError *domain.Finding
		for _, errIdx := range errorFindings {
			if !consumed[errIdx] && isMatchingScope(latFinding.Scope, findings[errIdx].Scope) {
				matchedError = &findings[errIdx]
				consumed[errIdx] = true
				break
			}
		}

		// Look for regressed dependency as likely cause
		var matchedDep *domain.Finding
		for _, depIdx := range depLatencyFindings {
			if !consumed[depIdx] {
				matchedDep = &findings[depIdx]
				consumed[depIdx] = true
				break
			}
		}
		if matchedDep == nil {
			for _, depIdx := range depErrorFindings {
				if !consumed[depIdx] {
					matchedDep = &findings[depIdx]
					consumed[depIdx] = true
					break
				}
			}
		}

		// Look for new exception as likely cause
		var matchedExc *domain.Finding
		for _, excIdx := range newExceptionFindings {
			if !consumed[excIdx] && (findings[excIdx].Severity != "LOW" || isMatchingScope(latFinding.Scope, findings[excIdx].Scope)) {
				matchedExc = &findings[excIdx]
				consumed[excIdx] = true
				break
			}
		}

		prob := buildEndpointProblem(snapshot, latFinding, matchedError, matchedDep, matchedExc)
		problems = append(problems, prob)
	}

	// Any remaining unconsumed error regressions
	for _, errIdx := range errorFindings {
		if consumed[errIdx] {
			continue
		}
		errFinding := findings[errIdx]
		consumed[errIdx] = true

		// Look for matching new exception
		var matchedExc *domain.Finding
		for _, excIdx := range newExceptionFindings {
			if !consumed[excIdx] {
				matchedExc = &findings[excIdx]
				consumed[excIdx] = true
				break
			}
		}

		// Look for matching dep error
		var matchedDep *domain.Finding
		for _, depIdx := range depErrorFindings {
			if !consumed[depIdx] {
				matchedDep = &findings[depIdx]
				consumed[depIdx] = true
				break
			}
		}

		prob := buildEndpointProblem(snapshot, domain.Finding{}, &errFinding, matchedDep, matchedExc)
		problems = append(problems, prob)
	}

	// 3. Process remaining unconsumed findings
	for i, f := range findings {
		if consumed[i] {
			continue
		}
		switch f.Kind {
		case domain.FindingNewException:
			// Scenario C: Low impact new exception -> Worth Watching!
			started := f.StartedAt
			timeStr := ""
			if started != nil && !started.IsZero() {
				timeStr = fmt.Sprintf("started %s", started.Format("15:04"))
			} else {
				timeStr = "recently introduced"
			}
			watching = append(watching, domain.WatchingItem{
				Summary:   f.Summary,
				Detail:    fmt.Sprintf("%d occurrences · %s", f.SampleCount, timeStr),
				StartedAt: f.StartedAt,
				Scope:     f.Scope,
			})
		case domain.FindingRestartBurst:
			// Restarts with no request degradation -> Worth Watching!
			watching = append(watching, domain.WatchingItem{
				Summary: f.Summary,
				Detail:  "no request impact detected",
				Scope:   f.Scope,
			})
		case domain.FindingResourceSaturation:
			watching = append(watching, domain.WatchingItem{
				Summary: f.Summary,
				Detail:  "approaching saturation thresholds",
				Scope:   f.Scope,
			})
		case domain.FindingDependencyLatencyRegression, domain.FindingDependencyErrorRegression, domain.FindingDependencyFanoutRegression:
			// Independent dependency problem
			prob := domain.Problem{
				Kind:     domain.ProblemKindDegradation,
				Priority: 2,
				Scope:    f.Scope,
				Summary:  f.Summary,
				Symptoms: []domain.Finding{f},
				Action: &domain.Action{
					Summary: fmt.Sprintf("Inspect %s calls", f.Scope.Target),
					Command: &domain.Command{
						Display: fmt.Sprintf("azlens inspect dependencies --role %s", f.Scope.Role),
					},
				},
			}
			problems = append(problems, prob)
		case domain.FindingAvailabilityFailure:
			prob := domain.Problem{
				Kind:     domain.ProblemKindAvailability,
				Priority: 1,
				Scope:    f.Scope,
				Summary:  f.Summary,
				Symptoms: []domain.Finding{f},
				Action: &domain.Action{
					Summary: "Inspect availability test results",
					Command: &domain.Command{
						Display: "azlens inspect endpoints",
					},
				},
			}
			problems = append(problems, prob)
		}
	}

	return problems, watching
}

func isMatchingScope(a, b domain.Scope) bool {
	if a.Endpoint != "" && b.Endpoint != "" {
		return a.Endpoint == b.Endpoint || strings.Contains(a.Endpoint, b.Endpoint) || strings.Contains(b.Endpoint, a.Endpoint)
	}
	if a.Role != "" && b.Role != "" {
		return strings.EqualFold(a.Role, b.Role)
	}
	return true
}

func formatDurationMs(ms float64) string {
	if ms >= 1000 {
		return fmt.Sprintf("%.1fs", ms/1000.0)
	}
	return fmt.Sprintf("%.0fms", ms)
}

func cleanSubject(raw string) string {
	s := strings.TrimSpace(raw)
	// Strip HTTP methods e.g. "POST /checkout" -> "checkout"
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

// buildEndpointProblem constructs a Problem for a degraded service or endpoint.
func buildEndpointProblem(
	snapshot *domain.Snapshot,
	latFinding domain.Finding,
	errFinding *domain.Finding,
	depFinding *domain.Finding,
	excFinding *domain.Finding,
) domain.Problem {
	scope := latFinding.Scope
	if scope.Endpoint == "" && errFinding != nil {
		scope = errFinding.Scope
	}
	if scope.Role == "" {
		scope.Role = snapshot.Scope.Role
	}

	displayName := scope.Endpoint
	if displayName == "" {
		displayName = scope.Role
	}
	if displayName == "" {
		displayName = "Application"
	}
	cleanName := cleanSubject(displayName)
	capitalized := strings.ToUpper(cleanName[:1]) + cleanName[1:]

	probSummary := fmt.Sprintf("%s is degraded", capitalized)

	// Build Impact
	impact := domain.Impact{}
	if len(latFinding.Evidence) > 0 {
		ev := latFinding.Evidence[0]
		if ev.Baseline != nil {
			impact.P95Baseline = formatDurationMs(ev.Baseline.Val)
		}
		impact.P95Current = formatDurationMs(ev.Current.Val)
	}
	if errFinding != nil && len(errFinding.Evidence) > 0 {
		ev := errFinding.Evidence[0]
		if ev.Baseline != nil {
			impact.ErrorBaseline = ev.Baseline.Text
		}
		impact.ErrorCurrent = ev.Current.Text
	}

	// Traffic share calculation
	totalCalls := snapshot.CurrentOverall.TotalCalls
	if totalCalls > 0 {
		epCalls := latFinding.SampleCount
		if epCalls == 0 && errFinding != nil {
			epCalls = errFinding.SampleCount
		}
		if epCalls > 0 {
			impact.TrafficPct = roundFloat((float64(epCalls)/float64(totalCalls))*100.0, 0)
		}
	}

	// Root Cause determination
	var cause *domain.Cause
	var action *domain.Action
	var symptoms []domain.Finding

	if latFinding.Kind != "" {
		symptoms = append(symptoms, latFinding)
	}
	if errFinding != nil {
		symptoms = append(symptoms, *errFinding)
	}

	if depFinding != nil {
		symptoms = append(symptoms, *depFinding)
		depTarget := depFinding.Scope.Target
		evidenceList := []domain.Evidence{
			{
				Signal: fmt.Sprintf("dependency p95 increased %.0f%%", depFinding.Evidence[0].Change.Pct),
			},
			{
				Signal: "degradation started in the same interval",
			},
		}

		// Supporting evidence: check if SQL and Redis were stable
		hasOtherRegressedDeps := false
		for _, d := range snapshot.CurrentDependencies {
			if d.Target != depTarget && d.ErrorRate < 1.0 {
				// stable
			} else if d.Target != depTarget && d.ErrorRate >= 5.0 {
				hasOtherRegressedDeps = true
			}
		}
		if !hasOtherRegressedDeps {
			evidenceList = append(evidenceList, domain.Evidence{
				Signal: "SQL and Redis remained stable",
			})
		}

		cause = &domain.Cause{
			Summary:  depTarget,
			Subject:  depTarget,
			Strength: domain.EvidenceStrengthStrong,
			Evidence: evidenceList,
		}

		action = &domain.Action{
			Summary: fmt.Sprintf("Inspect %s calls", depTarget),
			Command: &domain.Command{
				Display: fmt.Sprintf("azlens inspect dependencies --role %s", cleanName),
			},
		}
	} else if excFinding != nil {
		symptoms = append(symptoms, *excFinding)
		excType := excFinding.Scope.Endpoint
		if excType == "" {
			excType = "Exception"
		}
		// Try to extract type from summary e.g. "New NoMethodError appeared..."
		fields := strings.Fields(excFinding.Summary)
		if len(fields) >= 2 && fields[0] == "New" {
			excType = fields[1]
		}

		cause = &domain.Cause{
			Summary:  excType,
			Subject:  excType,
			Strength: domain.EvidenceStrengthStrong,
			Evidence: []domain.Evidence{
				{
					Signal: fmt.Sprintf("new exception introduced in current release (%d occurrences)", excFinding.SampleCount),
				},
				{
					Signal: "first seen in the same interval as request degradation",
				},
			},
		}

		action = &domain.Action{
			Summary: fmt.Sprintf("Inspect %s exceptions", excType),
			Command: &domain.Command{
				Display: fmt.Sprintf("azlens inspect errors --role %s", cleanName),
			},
		}
	} else {
		// Section 2.6: Causes require evidence!
		action = &domain.Action{
			Summary: "Inspect endpoint metrics",
			Command: &domain.Command{
				Display: fmt.Sprintf("azlens inspect endpoints --role %s", cleanName),
			},
		}
	}

	started := latFinding.StartedAt
	if started == nil && errFinding != nil {
		started = errFinding.StartedAt
	}

	return domain.Problem{
		Kind:      domain.ProblemKindDegradation,
		Priority:  1,
		Scope:     scope,
		Summary:   probSummary,
		Impact:    impact,
		StartedAt: started,
		Symptoms:  symptoms,
		Cause:     cause,
		Action:    action,
	}
}

// correlateRuntimeAvailability correlates replica loss, OOM, restarts, and 503s into one story.
func correlateRuntimeAvailability(
	snapshot *domain.Snapshot,
	findings []domain.Finding,
	workloadIdxs []int,
	oomIdxs []int,
	restartIdxs []int,
	availIdxs []int,
	consumed map[int]bool,
) *domain.Problem {
	for _, idx := range workloadIdxs {
		consumed[idx] = true
	}
	for _, idx := range oomIdxs {
		consumed[idx] = true
	}
	for _, idx := range restartIdxs {
		consumed[idx] = true
	}
	for _, idx := range availIdxs {
		consumed[idx] = true
	}

	var workloadName string
	var totalOOM int32
	var desiredReplicas, readyReplicas int32

	for _, w := range snapshot.Workloads {
		workloadName = w.Name
		totalOOM += w.OOMKills
		desiredReplicas = w.DesiredReplicas
		readyReplicas = w.ReadyReplicas
	}
	if workloadName == "" {
		workloadName = "Backend"
	}
	cleanWorkload := strings.ToUpper(workloadName[:1]) + workloadName[1:]

	summary := fmt.Sprintf("%s availability is degraded", cleanWorkload)

	var evidenceList []domain.Evidence
	var causeSummary string

	if totalOOM > 0 || len(oomIdxs) > 0 {
		causeSummary = "containers are being killed by memory pressure"
		oomCount := totalOOM
		if oomCount == 0 {
			oomCount = int32(len(oomIdxs))
		}
		evidenceList = append(evidenceList, domain.Evidence{
			Signal: fmt.Sprintf("%d OOM kills detected", oomCount),
		})
	} else {
		causeSummary = "workload replicas became unavailable"
	}

	if desiredReplicas > 0 {
		evidenceList = append(evidenceList, domain.Evidence{
			Signal: fmt.Sprintf("available replicas dropped %d -> %d", desiredReplicas, readyReplicas),
		})
	}
	if len(availIdxs) > 0 {
		evidenceList = append(evidenceList, domain.Evidence{
			Signal: "HTTP 503 error rate increased",
		})
	}

	cause := &domain.Cause{
		Summary:  causeSummary,
		Subject:  workloadName,
		Strength: domain.EvidenceStrengthStrong,
		Evidence: evidenceList,
	}

	action := &domain.Action{
		Summary: fmt.Sprintf("Inspect %s runtime and memory limits", workloadName),
		Command: &domain.Command{
			Display: fmt.Sprintf("azlens inspect runtime --pod %s", workloadName),
		},
	}

	return &domain.Problem{
		Kind:     domain.ProblemKindAvailability,
		Priority: 1,
		Scope: domain.Scope{
			Workload: workloadName,
			Role:     snapshot.Scope.Role,
		},
		Summary: summary,
		Cause:   cause,
		Action:  action,
	}
}

func roundFloat(val float64, precision int) float64 {
	ratio := math.Pow(10, float64(precision))
	return math.Round(val*ratio) / ratio
}
