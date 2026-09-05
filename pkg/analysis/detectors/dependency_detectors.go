package detectors

import (
	"fmt"

	"github.com/denjamio/azlens/pkg/domain"
	"github.com/denjamio/azlens/pkg/model"
)

// DependencyLatencyRegressionDetector detects latency spikes in dependencies (SQL, HTTP, Redis, etc.).
type DependencyLatencyRegressionDetector struct {
	cfg Config
}

func NewDependencyLatencyRegressionDetector(cfg Config) *DependencyLatencyRegressionDetector {
	return &DependencyLatencyRegressionDetector{cfg: cfg}
}

func (d *DependencyLatencyRegressionDetector) Name() string {
	return "DependencyLatencyRegression"
}

func (d *DependencyLatencyRegressionDetector) Detect(snapshot *domain.Snapshot) []domain.Finding {
	var findings []domain.Finding
	minCalls := d.cfg.MinSampleCalls

	baseMap := make(map[string]model.DependencyMetric)
	for _, dep := range snapshot.BaselineDependencies {
		key := fmt.Sprintf("%s|%s|%s", dep.Type, dep.Target, dep.Name)
		baseMap[key] = dep
	}

	for _, curr := range snapshot.CurrentDependencies {
		key := fmt.Sprintf("%s|%s|%s", curr.Type, curr.Target, curr.Name)
		base, exists := baseMap[key]
		if !exists {
			continue
		}

		if curr.TotalCalls < minCalls || base.TotalCalls < minCalls {
			continue
		}

		p95Pct := calcPctChange(base.Latency.P95, curr.Latency.P95)
		if p95Pct < d.cfg.LatencyWarnPct {
			continue
		}

		sev := "WARNING"
		if p95Pct >= d.cfg.LatencyCritPct {
			sev = "CRITICAL"
		}

		scope := domain.Scope{
			Target: curr.Target,
			Role:   snapshot.Scope.Role,
		}

		findings = append(findings, domain.Finding{
			Kind:  domain.FindingDependencyLatencyRegression,
			Scope: scope,
			Summary: fmt.Sprintf("%s dependency '%s' p95 increased by %.0f%% (%.0fms -> %.0fms)",
				curr.Type, curr.Target, p95Pct, base.Latency.P95, curr.Latency.P95),
			Severity:    sev,
			SampleCount: curr.TotalCalls,
			Evidence: []domain.Evidence{
				{
					Signal:   "dependency p95 latency",
					Current:  domain.Value{Val: curr.Latency.P95, Unit: "ms", Text: fmt.Sprintf("%.0fms", curr.Latency.P95)},
					Baseline: &domain.Value{Val: base.Latency.P95, Unit: "ms", Text: fmt.Sprintf("%.0fms", base.Latency.P95)},
					Change:   &domain.Change{Delta: curr.Latency.P95 - base.Latency.P95, Pct: p95Pct},
					Scope:    scope,
				},
			},
		})
	}

	return findings
}

// DependencyErrorRegressionDetector detects spikes in dependency errors/failures.
type DependencyErrorRegressionDetector struct {
	cfg Config
}

func NewDependencyErrorRegressionDetector(cfg Config) *DependencyErrorRegressionDetector {
	return &DependencyErrorRegressionDetector{cfg: cfg}
}

func (d *DependencyErrorRegressionDetector) Name() string {
	return "DependencyErrorRegression"
}

func (d *DependencyErrorRegressionDetector) Detect(snapshot *domain.Snapshot) []domain.Finding {
	var findings []domain.Finding
	minCalls := d.cfg.MinSampleCalls

	baseMap := make(map[string]model.DependencyMetric)
	for _, dep := range snapshot.BaselineDependencies {
		key := fmt.Sprintf("%s|%s|%s", dep.Type, dep.Target, dep.Name)
		baseMap[key] = dep
	}

	for _, curr := range snapshot.CurrentDependencies {
		key := fmt.Sprintf("%s|%s|%s", curr.Type, curr.Target, curr.Name)
		base, exists := baseMap[key]

		if curr.TotalCalls < minCalls {
			continue
		}

		var (
			errDelta    float64
			hasBaseline = exists && base.TotalCalls >= minCalls
			regressed   bool
		)

		if hasBaseline {
			errDelta = curr.ErrorRate - base.ErrorRate
			if errDelta >= d.cfg.ErrorRateWarnDelta {
				regressed = true
			}
		} else {
			if curr.ErrorRate >= d.cfg.ErrorRateWarnDelta && curr.FailedCalls > 0 {
				regressed = true
				errDelta = curr.ErrorRate
			}
		}

		if !regressed {
			continue
		}

		sev := "WARNING"
		if errDelta >= d.cfg.ErrorRateCritDelta || curr.ErrorRate >= 10.0 {
			sev = "CRITICAL"
		}

		scope := domain.Scope{
			Target: curr.Target,
			Role:   snapshot.Scope.Role,
		}

		var summary string
		var ev domain.Evidence
		if hasBaseline {
			summary = fmt.Sprintf("%s dependency '%s' error rate increased from %.1f%% to %.1f%% (+%.1fpp)",
				curr.Type, curr.Target, base.ErrorRate, curr.ErrorRate, errDelta)
			ev = domain.Evidence{
				Signal:   "dependency error rate",
				Current:  domain.Value{Val: curr.ErrorRate, Unit: "%", Text: fmt.Sprintf("%.1f%%", curr.ErrorRate)},
				Baseline: &domain.Value{Val: base.ErrorRate, Unit: "%", Text: fmt.Sprintf("%.1f%%", base.ErrorRate)},
				Change:   &domain.Change{Delta: errDelta, Summary: fmt.Sprintf("+%.1fpp", errDelta)},
				Scope:    scope,
			}
		} else {
			summary = fmt.Sprintf("%s dependency '%s' failure rate at %.1f%% (%d failed calls)",
				curr.Type, curr.Target, curr.ErrorRate, curr.FailedCalls)
			ev = domain.Evidence{
				Signal:  "dependency error rate",
				Current: domain.Value{Val: curr.ErrorRate, Unit: "%", Text: fmt.Sprintf("%.1f%%", curr.ErrorRate)},
				Scope:   scope,
			}
		}

		findings = append(findings, domain.Finding{
			Kind:        domain.FindingDependencyErrorRegression,
			Scope:       scope,
			Summary:     summary,
			Severity:    sev,
			SampleCount: curr.TotalCalls,
			Evidence: []domain.Evidence{
				ev,
				{
					Signal:  "dependency failures",
					Current: domain.Value{Val: float64(curr.FailedCalls), Text: fmt.Sprintf("%d failures", curr.FailedCalls)},
					Scope:   scope,
				},
			},
		})
	}

	return findings
}

// DependencyFanoutRegressionDetector detects N+1 queries.
type DependencyFanoutRegressionDetector struct {
	cfg Config
}

func NewDependencyFanoutRegressionDetector(cfg Config) *DependencyFanoutRegressionDetector {
	return &DependencyFanoutRegressionDetector{cfg: cfg}
}

func (d *DependencyFanoutRegressionDetector) Name() string {
	return "DependencyFanoutRegression"
}

func (d *DependencyFanoutRegressionDetector) Detect(snapshot *domain.Snapshot) []domain.Finding {
	var findings []domain.Finding
	minCalls := d.cfg.MinSampleCalls

	baseMap := make(map[string]model.FanoutMetric)
	for _, f := range snapshot.BaselineFanout {
		baseMap[f.Endpoint] = f
	}

	for _, curr := range snapshot.CurrentFanout {
		base, exists := baseMap[curr.Endpoint]
		if !exists || curr.AvgSQLCalls < 5.0 || curr.TotalRequests < minCalls || base.TotalRequests < minCalls {
			continue
		}

		pctChange := calcPctChange(base.AvgSQLCalls, curr.AvgSQLCalls)
		diffCalls := curr.AvgSQLCalls - base.AvgSQLCalls
		if pctChange >= 40.0 && diffCalls >= 3.0 {
			sev := "WARNING"
			if curr.AvgSQLCalls >= 15.0 || pctChange >= 100.0 {
				sev = "CRITICAL"
			}

			scope := domain.Scope{
				Endpoint: curr.Endpoint,
				Role:     snapshot.Scope.Role,
			}

			findings = append(findings, domain.Finding{
				Kind:  domain.FindingDependencyFanoutRegression,
				Scope: scope,
				Summary: fmt.Sprintf("N+1 SQL query fanout on '%s' spiked from %.1f to %.1f calls/request (+%.0f%%)",
					curr.Endpoint, base.AvgSQLCalls, curr.AvgSQLCalls, pctChange),
				Severity:    sev,
				SampleCount: curr.TotalRequests,
				Evidence: []domain.Evidence{
					{
						Signal:   "SQL calls per request",
						Current:  domain.Value{Val: curr.AvgSQLCalls, Text: fmt.Sprintf("%.1f calls/req", curr.AvgSQLCalls)},
						Baseline: &domain.Value{Val: base.AvgSQLCalls, Text: fmt.Sprintf("%.1f calls/req", base.AvgSQLCalls)},
						Change:   &domain.Change{Delta: diffCalls, Pct: pctChange},
						Scope:    scope,
					},
				},
			})
		}
	}

	return findings
}

// SQLFanoutDetector detects endpoints with high database call fan-out across requests.
type SQLFanoutDetector struct {
	cfg Config
}

func NewSQLFanoutDetector(cfg Config) *SQLFanoutDetector {
	return &SQLFanoutDetector{cfg: cfg}
}

func (d *SQLFanoutDetector) Name() string {
	return "SQLFanout"
}

func (d *SQLFanoutDetector) Detect(snapshot *domain.Snapshot) []domain.Finding {
	var findings []domain.Finding
	minCalls := d.cfg.MinSampleCalls

	hasBaseline := len(snapshot.BaselineFanout) > 0
	baseMap := make(map[string]model.FanoutMetric)
	for _, f := range snapshot.BaselineFanout {
		baseMap[f.Endpoint] = f
	}

	for _, curr := range snapshot.CurrentFanout {
		if curr.TotalRequests < minCalls {
			continue
		}

		p95Calls := curr.P95Calls
		if p95Calls == 0 {
			p95Calls = curr.AvgSQLCalls
		}
		if p95Calls < 5.0 && curr.MaxSQLCalls < 10 {
			continue
		}

		base, inBaseline := baseMap[curr.Endpoint]
		if hasBaseline && inBaseline {
			baseP95 := base.P95Calls
			if baseP95 == 0 {
				baseP95 = base.AvgSQLCalls
			}
			diff := p95Calls - baseP95
			pct := calcPctChange(baseP95, p95Calls)
			if diff < 3.0 || pct < 30.0 {
				continue
			}
		}

		sev := "WARNING"
		if p95Calls >= 15.0 || curr.MaxSQLCalls >= 30 {
			sev = "CRITICAL"
		}

		scope := domain.Scope{
			Endpoint: curr.Endpoint,
			Role:     snapshot.Scope.Role,
		}

		findings = append(findings, domain.Finding{
			Kind:  domain.FindingSQLFanout,
			Scope: scope,
			Summary: fmt.Sprintf("High SQL fan-out on '%s': p95=%.1f calls/req, max=%d calls (avg %.1fms SQL time)",
				curr.Endpoint, p95Calls, curr.MaxSQLCalls, curr.AvgSQLDurationMs),
			Severity:    sev,
			SampleCount: curr.TotalRequests,
			Evidence: []domain.Evidence{
				{
					Signal:  "SQL calls distribution (p95)",
					Current: domain.Value{Val: p95Calls, Text: fmt.Sprintf("%.1f calls/req", p95Calls)},
					Scope:   scope,
				},
				{
					Signal:  "Max SQL calls per request",
					Current: domain.Value{Val: float64(curr.MaxSQLCalls), Text: fmt.Sprintf("%d calls", curr.MaxSQLCalls)},
					Scope:   scope,
				},
			},
		})
	}

	return findings
}

// NPlusOneCandidateDetector detects deterministic N+1 query candidates by requiring
// evidence of repeated query shapes within individual requests (Section 6.2).
type NPlusOneCandidateDetector struct {
	cfg Config
}

func NewNPlusOneCandidateDetector(cfg Config) *NPlusOneCandidateDetector {
	return &NPlusOneCandidateDetector{cfg: cfg}
}

func (d *NPlusOneCandidateDetector) Name() string {
	return "NPlusOneCandidate"
}

func (d *NPlusOneCandidateDetector) Detect(snapshot *domain.Snapshot) []domain.Finding {
	var findings []domain.Finding

	for _, cand := range snapshot.CurrentNPlusOne {
		// Minimum sample requirement
		if cand.TotalRequests < 5 {
			continue
		}

		// Thresholds per spec: RepeatedShapeRatio >= 40.0% and MaxRepeatedShape >= 5
		if cand.AvgRepeatedRatio < 40.0 || cand.MaxRepeatedShape < 5 {
			continue
		}

		sev := "WARNING"
		if cand.AvgRepeatedRatio >= 70.0 || cand.MaxRepeatedShape >= 15 {
			sev = "CRITICAL"
		}

		scope := domain.Scope{
			Endpoint: cand.Endpoint,
			Role:     snapshot.Scope.Role,
		}

		shapeDesc := cand.SampleRepeatedShape
		if shapeDesc == "" {
			shapeDesc = "repeated query shape"
		}

		findings = append(findings, domain.Finding{
			Kind:  domain.FindingNPlusOneCandidate,
			Scope: scope,
			Summary: fmt.Sprintf("Deterministic N+1 query candidate on '%s': repeated shape %q executed up to %d times/request (%.1f%% repeated calls)",
				cand.Endpoint, shapeDesc, cand.MaxRepeatedShape, cand.AvgRepeatedRatio),
			Severity:    sev,
			SampleCount: cand.TotalRequests,
			Evidence: []domain.Evidence{
				{
					Signal:  "Repeated query shape",
					Current: domain.Value{Text: shapeDesc},
					Scope:   scope,
				},
				{
					Signal:  "Max repeated executions per request",
					Current: domain.Value{Val: float64(cand.MaxRepeatedShape), Text: fmt.Sprintf("%d executions", cand.MaxRepeatedShape)},
					Scope:   scope,
				},
				{
					Signal:  "Repeated shape ratio",
					Current: domain.Value{Val: cand.AvgRepeatedRatio, Unit: "%", Text: fmt.Sprintf("%.1f%%", cand.AvgRepeatedRatio)},
					Scope:   scope,
				},
			},
		})
	}

	return findings
}
