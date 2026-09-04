// Package analyzer implements the deploy regression detection engine that
// compares telemetry across two time windows and correlates root causes.
package analyzer

import (
	"fmt"
	"math"
	"strings"

	"github.com/denjamio/azlens/pkg/model"
)

// RegressionThresholds configures tolerance limits for regressions
type RegressionThresholds struct {
	LatencyWarnPct     float64 // e.g. 10.0 (%)
	LatencyCritPct     float64 // e.g. 25.0 (%)
	ErrorRateWarnDelta float64 // e.g. 1.0 (absolute % increase)
	ErrorRateCritDelta float64 // e.g. 3.0 (absolute % increase)
	MinSampleCalls     int64   // Minimum calls to consider endpoint statistically significant
}

// DefaultThresholds returns sensible default thresholds for deploy regressions
func DefaultThresholds() RegressionThresholds {
	return RegressionThresholds{
		LatencyWarnPct:     15.0,
		LatencyCritPct:     30.0,
		ErrorRateWarnDelta: 1.0,
		ErrorRateCritDelta: 3.0,
		MinSampleCalls:     5,
	}
}

// CompareOptions bundles all metrics needed for comprehensive regression analysis
type CompareOptions struct {
	AppName        string
	BaselineWindow model.TimeWindow
	CurrentWindow  model.TimeWindow
	BaseReqOverall model.RequestMetric
	CurrReqOverall model.RequestMetric
	BaseEndpoints  []model.RequestMetric
	CurrEndpoints  []model.RequestMetric
	BaseDeps       []model.DependencyMetric
	CurrDeps       []model.DependencyMetric
	BaseErrors     []model.ErrorSummary
	CurrErrors     []model.ErrorSummary
	BaseFanout     []model.FanoutMetric
	CurrFanout     []model.FanoutMetric
	Thresholds     RegressionThresholds
}

// Compare performs a complete pre-vs-post deploy regression analysis with cross-correlations.
// It orchestrates six analysis phases, each isolated in its own function.
func Compare(opts CompareOptions) model.DiffReport {
	report := model.DiffReport{
		AppName:         opts.AppName,
		BaselineWindow:  opts.BaselineWindow,
		CurrentWindow:   opts.CurrentWindow,
		OverallVerdict:  model.SeverityNone,
		SummaryDeltas:   make([]model.MetricDelta, 0),
		EndpointDeltas:  make([]model.EndpointDiff, 0),
		NewErrors:       make([]model.ErrorSummary, 0),
		RegressedDeps:   make([]model.DependencyDiff, 0),
		NewDependencies: make([]model.DependencyMetric, 0),
		FanoutDeltas:    make([]model.FanoutDiff, 0),
		RootCauseHints:  make([]string, 0),
	}

	errDelta := summarizeDeltas(&report, opts)
	compareEndpoints(&report, opts)
	compareDependencies(&report, opts, errDelta)
	detectNewErrors(&report, opts)
	compareFanout(&report, opts)
	correlateRootCauses(&report)
	correlateInstrumentationNoise(&report, opts)

	return report
}

// summarizeDeltas computes overall latency & error summary deltas (phase 1),
// escalates the overall verdict, and returns the error-rate delta for downstream correlation
func summarizeDeltas(report *model.DiffReport, opts CompareOptions) model.MetricDelta {
	t := opts.Thresholds

	p50Delta := calcDelta("P50 Latency", opts.BaseReqOverall.Latency.P50, opts.CurrReqOverall.Latency.P50, "ms", t.LatencyWarnPct, t.LatencyCritPct, false)
	p95Delta := calcDelta("P95 Latency", opts.BaseReqOverall.Latency.P95, opts.CurrReqOverall.Latency.P95, "ms", t.LatencyWarnPct, t.LatencyCritPct, false)
	p99Delta := calcDelta("P99 Latency", opts.BaseReqOverall.Latency.P99, opts.CurrReqOverall.Latency.P99, "ms", t.LatencyWarnPct, t.LatencyCritPct, false)
	errDelta := calcDelta("Error Rate", opts.BaseReqOverall.ErrorRate, opts.CurrReqOverall.ErrorRate, "%", t.ErrorRateWarnDelta, t.ErrorRateCritDelta, true)
	rpsDelta := calcDelta("Total Requests", float64(opts.BaseReqOverall.TotalCalls), float64(opts.CurrReqOverall.TotalCalls), "reqs", 50.0, 80.0, false)

	report.SummaryDeltas = append(report.SummaryDeltas, p50Delta, p95Delta, p99Delta, errDelta, rpsDelta)

	// Update overall verdict from summaries
	for _, delta := range report.SummaryDeltas {
		if delta.Severity == model.SeverityCritical {
			report.OverallVerdict = model.SeverityCritical
			break
		} else if delta.Severity == model.SeverityWarning && report.OverallVerdict != model.SeverityCritical {
			report.OverallVerdict = model.SeverityWarning
		}
	}

	return errDelta
}

// compareEndpoints diffs per-endpoint latency and error rates (phase 2)
func compareEndpoints(report *model.DiffReport, opts CompareOptions) {
	t := opts.Thresholds

	baseEpMap := make(map[string]model.RequestMetric)
	for _, ep := range opts.BaseEndpoints {
		baseEpMap[ep.Name] = ep
	}

	minCalls := t.MinSampleCalls
	if minCalls <= 0 {
		minCalls = 5
	}

	for _, curr := range opts.CurrEndpoints {
		base, exists := baseEpMap[curr.Name]
		if !exists {
			// Brand new endpoint
			report.EndpointDeltas = append(report.EndpointDeltas, model.EndpointDiff{
				Name:        curr.Name,
				Baseline:    model.RequestMetric{},
				Current:     curr,
				P95DeltaPct: 0,
				ErrDeltaPct: 0,
				Severity:    model.SeverityNone,
			})
			continue
		}

		p95DeltaPct := calcPctChange(base.Latency.P95, curr.Latency.P95)
		errDeltaPct := curr.ErrorRate - base.ErrorRate

		sev := model.SeverityNone
		if (base.TotalCalls < minCalls && curr.TotalCalls < minCalls) && errDeltaPct < 50.0 {
			// Statistically insignificant sample size, skip critical false alarm
			sev = model.SeverityNone
		} else if p95DeltaPct >= t.LatencyCritPct || errDeltaPct >= t.ErrorRateCritDelta {
			sev = model.SeverityCritical
		} else if p95DeltaPct >= t.LatencyWarnPct || errDeltaPct >= t.ErrorRateWarnDelta {
			sev = model.SeverityWarning
		} else if p95DeltaPct <= -15.0 {
			sev = model.SeverityImprove
		}

		if sev == model.SeverityCritical && report.OverallVerdict != model.SeverityCritical {
			report.OverallVerdict = model.SeverityCritical
		}

		report.EndpointDeltas = append(report.EndpointDeltas, model.EndpointDiff{
			Name:        curr.Name,
			Baseline:    base,
			Current:     curr,
			P95DeltaPct: p95DeltaPct,
			ErrDeltaPct: errDeltaPct,
			Severity:    sev,
		})
	}
}

// compareDependencies diffs slow dependencies and flags newly introduced ones (phase 3)
func compareDependencies(report *model.DiffReport, opts CompareOptions, errDelta model.MetricDelta) {
	t := opts.Thresholds

	baseDepMap := make(map[string]model.DependencyMetric)
	for _, dep := range opts.BaseDeps {
		key := fmt.Sprintf("%s|%s|%s", dep.Type, dep.Target, dep.Name)
		baseDepMap[key] = dep
	}

	for _, curr := range opts.CurrDeps {
		key := fmt.Sprintf("%s|%s|%s", curr.Type, curr.Target, curr.Name)
		base, exists := baseDepMap[key]
		if exists {
			p95DeltaPct := calcPctChange(base.Latency.P95, curr.Latency.P95)
			sev := model.SeverityNone
			if p95DeltaPct >= t.LatencyCritPct {
				sev = model.SeverityCritical
			} else if p95DeltaPct >= t.LatencyWarnPct {
				sev = model.SeverityWarning
			} else if p95DeltaPct <= -15.0 {
				sev = model.SeverityImprove
			}

			if sev != model.SeverityNone {
				report.RegressedDeps = append(report.RegressedDeps, model.DependencyDiff{
					Name:        curr.Name,
					Type:        curr.Type,
					Target:      curr.Target,
					Baseline:    base,
					Current:     curr,
					P95DeltaPct: p95DeltaPct,
					Severity:    sev,
				})
			}
		} else {
			// Brand new dependency introduced in this deploy!
			report.NewDependencies = append(report.NewDependencies, curr)
			if curr.Latency.P95 >= 300.0 || curr.TotalCalls >= 30 {
				report.RootCauseHints = append(report.RootCauseHints,
					fmt.Sprintf("[NEW QUERY/DEP] Introduced %s call '%s' on %s (P95: %.1fms, Calls: %d)",
						curr.Type, curr.Name, curr.Target, curr.Latency.P95, curr.TotalCalls))
			}
		}

		// Cascading 5xx causality check: failed dependency correlates with overall error rate increase
		if curr.FailedCalls > 0 && curr.ErrorRate >= 5.0 && errDelta.Delta > 0.5 {
			report.RootCauseHints = append(report.RootCauseHints,
				fmt.Sprintf("[CASCADING 5xx CAUSE] Failing %s dependency '%s' on %s (%.1f%% error rate, %d failures) contributes to 5xx spikes",
					curr.Type, curr.Name, curr.Target, curr.ErrorRate, curr.FailedCalls))
		}
	}
}

// detectNewErrors flags exception types absent from the baseline window (phase 4)
func detectNewErrors(report *model.DiffReport, opts CompareOptions) {
	baseErrMap := make(map[string]bool)
	for _, err := range opts.BaseErrors {
		baseErrMap[err.Type] = true
	}

	for _, curr := range opts.CurrErrors {
		if !baseErrMap[curr.Type] {
			report.NewErrors = append(report.NewErrors, curr)
			if report.OverallVerdict != model.SeverityCritical {
				report.OverallVerdict = model.SeverityCritical
			}
		}
	}
}

// compareFanout detects N+1 SQL fan-out regressions (phase 5)
func compareFanout(report *model.DiffReport, opts CompareOptions) {
	baseFanoutMap := make(map[string]model.FanoutMetric)
	for _, f := range opts.BaseFanout {
		baseFanoutMap[f.Endpoint] = f
	}

	for _, curr := range opts.CurrFanout {
		base, exists := baseFanoutMap[curr.Endpoint]
		if exists && curr.AvgSQLCalls >= 5.0 {
			diffCalls := curr.AvgSQLCalls - base.AvgSQLCalls
			pctChange := calcPctChange(base.AvgSQLCalls, curr.AvgSQLCalls)
			if pctChange >= 40.0 && diffCalls >= 3.0 {
				sev := model.SeverityWarning
				if curr.AvgSQLCalls >= 15.0 || pctChange >= 100.0 {
					sev = model.SeverityCritical
					if report.OverallVerdict != model.SeverityCritical {
						report.OverallVerdict = model.SeverityCritical
					}
				}
				report.FanoutDeltas = append(report.FanoutDeltas, model.FanoutDiff{
					Endpoint:      curr.Endpoint,
					BaselineCalls: base.AvgSQLCalls,
					CurrentCalls:  curr.AvgSQLCalls,
					DeltaPct:      pctChange,
					Severity:      sev,
				})
				report.RootCauseHints = append(report.RootCauseHints,
					fmt.Sprintf("[N+1 SQL REGRESSION] '%s' average SQL calls per request spiked from %.1f to %.1f (+%.1f%%, Max: %d in one req)",
						curr.Endpoint, base.AvgSQLCalls, curr.AvgSQLCalls, pctChange, curr.MaxSQLCalls))
			}
		}
	}
}

// correlateRootCauses cross-references degraded endpoints with regressed dependencies
// and new exceptions to produce root cause hints (phase 6)
func correlateRootCauses(report *model.DiffReport) {
	for _, ep := range report.EndpointDeltas {
		if ep.Severity != model.SeverityCritical && ep.Severity != model.SeverityWarning {
			continue
		}
		// Check if any regressed dependency could be related
		for _, dep := range report.RegressedDeps {
			report.RootCauseHints = append(report.RootCauseHints,
				fmt.Sprintf("Degraded endpoint '%s' (+%.1f%% P95) correlates with regressed %s dependency '%s' on %s (+%.1f%% P95)",
					ep.Name, ep.P95DeltaPct, dep.Type, dep.Name, dep.Target, dep.P95DeltaPct))
		}
		// Check if any new error affected this endpoint
		for _, err := range report.NewErrors {
			for _, p := range err.AffectedPaths {
				if p == ep.Name || (p != "" && strings.Contains(ep.Name, p)) {
					report.RootCauseHints = append(report.RootCauseHints,
						fmt.Sprintf("New exception '%s' (%d occurrences) directly impacts endpoint '%s'",
							err.Type, err.Count, ep.Name))
				}
			}
		}
	}
}

// correlateInstrumentationNoise surfaces failures raised by the auto-instrumentation
// SDK itself (e.g. OpenTelemetry failing to hook 'fastapi'): they are easy to
// misread as application exceptions, but they signal a missing module in the
// deployed runtime image (phase 7)
func correlateInstrumentationNoise(report *model.DiffReport, opts CompareOptions) {
	seen := make(map[string]bool)
	for _, err := range opts.CurrErrors {
		target, ok := err.InstrumentationTarget()
		if !ok || seen[target] {
			continue
		}
		seen[target] = true
		report.RootCauseHints = append(report.RootCauseHints,
			fmt.Sprintf("[INSTRUMENTATION NOISE] '%s' while hooking '%s' is emitted by the auto-instrumentation SDK itself — "+
				"a required module is missing or not importable in the deployed image (verify the instrumentation package is installed), "+
				"not an application exception", err.Type, target))
	}
}

func calcPctChange(baseline, current float64) float64 {
	if baseline == 0 {
		if current == 0 {
			return 0
		}
		return 100.0
	}
	return ((current - baseline) / baseline) * 100.0
}

func calcDelta(name string, baseline, current float64, unit string, warnLimit, critLimit float64, isAbsoluteThreshold bool) model.MetricDelta {
	delta := current - baseline
	var pctChange float64
	if baseline == 0 {
		if current == 0 {
			pctChange = 0
		} else {
			pctChange = 100.0
		}
	} else {
		pctChange = (delta / baseline) * 100.0
	}

	sev := model.SeverityNone
	var compareValue float64
	if isAbsoluteThreshold {
		compareValue = delta // absolute difference for error rate
	} else {
		compareValue = pctChange // % difference for latency
	}

	explanation := fmt.Sprintf("%.2f%s -> %.2f%s", baseline, unit, current, unit)

	if compareValue >= critLimit {
		sev = model.SeverityCritical
		explanation = fmt.Sprintf("Regressed significantly by +%.1f%%", math.Abs(pctChange))
	} else if compareValue >= warnLimit {
		sev = model.SeverityWarning
		explanation = fmt.Sprintf("Degraded by +%.1f%%", math.Abs(pctChange))
	} else if pctChange <= -10.0 && !isAbsoluteThreshold {
		sev = model.SeverityImprove
		explanation = fmt.Sprintf("Improved by -%.1f%%", math.Abs(pctChange))
	}

	return model.MetricDelta{
		MetricName:  name,
		Baseline:    baseline,
		Current:     current,
		Delta:       delta,
		Percentage:  pctChange,
		Unit:        unit,
		Severity:    sev,
		Explanation: explanation,
	}
}
