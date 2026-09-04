package detectors

import (
	"fmt"

	"github.com/denjamio/azlens/pkg/domain"
	"github.com/denjamio/azlens/pkg/model"
)

// RequestLatencyRegressionDetector detects persistent endpoint and overall latency regressions.
type RequestLatencyRegressionDetector struct {
	cfg Config
}

func NewRequestLatencyRegressionDetector(cfg Config) *RequestLatencyRegressionDetector {
	return &RequestLatencyRegressionDetector{cfg: cfg}
}

func (d *RequestLatencyRegressionDetector) Name() string {
	return "RequestLatencyRegression"
}

func (d *RequestLatencyRegressionDetector) Detect(snapshot *domain.Snapshot) []domain.Finding {
	var findings []domain.Finding
	minCalls := d.cfg.MinSampleCalls

	baseMap := make(map[string]model.RequestMetric)
	for _, ep := range snapshot.BaselineEndpoints {
		baseMap[ep.Name] = ep
	}

	for _, curr := range snapshot.CurrentEndpoints {
		base, exists := baseMap[curr.Name]
		if !exists {
			continue
		}

		// Noise policy: minimum sample requirement
		if curr.TotalCalls < minCalls && base.TotalCalls < minCalls {
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
			Role:     snapshot.Scope.Role,
			Endpoint: curr.Name,
		}

		findings = append(findings, domain.Finding{
			Kind:  domain.FindingRequestLatencyRegression,
			Scope: scope,
			Summary: fmt.Sprintf("%s p95 latency increased by %.0f%% (%.0fms -> %.0fms)",
				curr.Name, p95Pct, base.Latency.P95, curr.Latency.P95),
			Severity:    sev,
			SampleCount: curr.TotalCalls,
			Evidence: []domain.Evidence{
				{
					Signal:   "p95 latency",
					Current:  domain.Value{Val: curr.Latency.P95, Unit: "ms", Text: fmt.Sprintf("%.0fms", curr.Latency.P95)},
					Baseline: &domain.Value{Val: base.Latency.P95, Unit: "ms", Text: fmt.Sprintf("%.0fms", base.Latency.P95)},
					Change:   &domain.Change{Delta: curr.Latency.P95 - base.Latency.P95, Pct: p95Pct},
					Scope:    scope,
				},
			},
		})
	}

	// Overall latency regression fallback if no endpoint-specific regressed
	if len(findings) == 0 && snapshot.BaselineOverall != nil {
		base := *snapshot.BaselineOverall
		curr := snapshot.CurrentOverall
		if curr.TotalCalls >= minCalls {
			p95Pct := calcPctChange(base.Latency.P95, curr.Latency.P95)
			if p95Pct >= d.cfg.LatencyWarnPct {
				sev := "WARNING"
				if p95Pct >= d.cfg.LatencyCritPct {
					sev = "CRITICAL"
				}
				scope := domain.Scope{Role: snapshot.Scope.Role}
				findings = append(findings, domain.Finding{
					Kind:  domain.FindingRequestLatencyRegression,
					Scope: scope,
					Summary: fmt.Sprintf("Overall p95 latency increased by %.0f%% (%.0fms -> %.0fms)",
						p95Pct, base.Latency.P95, curr.Latency.P95),
					Severity:    sev,
					SampleCount: curr.TotalCalls,
					Evidence: []domain.Evidence{
						{
							Signal:   "p95 latency",
							Current:  domain.Value{Val: curr.Latency.P95, Unit: "ms", Text: fmt.Sprintf("%.0fms", curr.Latency.P95)},
							Baseline: &domain.Value{Val: base.Latency.P95, Unit: "ms", Text: fmt.Sprintf("%.0fms", base.Latency.P95)},
							Change:   &domain.Change{Delta: curr.Latency.P95 - base.Latency.P95, Pct: p95Pct},
							Scope:    scope,
						},
					},
				})
			}
		}
	}

	return findings
}

// RequestErrorRegressionDetector detects increases in HTTP error rate.
type RequestErrorRegressionDetector struct {
	cfg Config
}

func NewRequestErrorRegressionDetector(cfg Config) *RequestErrorRegressionDetector {
	return &RequestErrorRegressionDetector{cfg: cfg}
}

func (d *RequestErrorRegressionDetector) Name() string {
	return "RequestErrorRegression"
}

func (d *RequestErrorRegressionDetector) Detect(snapshot *domain.Snapshot) []domain.Finding {
	var findings []domain.Finding
	minCalls := d.cfg.MinSampleCalls

	baseMap := make(map[string]model.RequestMetric)
	for _, ep := range snapshot.BaselineEndpoints {
		baseMap[ep.Name] = ep
	}

	for _, curr := range snapshot.CurrentEndpoints {
		base, exists := baseMap[curr.Name]
		if !exists {
			continue
		}

		if curr.TotalCalls < minCalls && base.TotalCalls < minCalls {
			continue
		}

		errDelta := curr.ErrorRate - base.ErrorRate
		if errDelta < d.cfg.ErrorRateWarnDelta {
			continue
		}

		sev := "WARNING"
		if errDelta >= d.cfg.ErrorRateCritDelta {
			sev = "CRITICAL"
		}

		scope := domain.Scope{
			Role:     snapshot.Scope.Role,
			Endpoint: curr.Name,
		}

		findings = append(findings, domain.Finding{
			Kind:  domain.FindingRequestErrorRegression,
			Scope: scope,
			Summary: fmt.Sprintf("%s error rate increased from %.1f%% to %.1f%% (+%.1fpp)",
				curr.Name, base.ErrorRate, curr.ErrorRate, errDelta),
			Severity:    sev,
			SampleCount: curr.TotalCalls,
			Evidence: []domain.Evidence{
				{
					Signal:   "error rate",
					Current:  domain.Value{Val: curr.ErrorRate, Unit: "%", Text: fmt.Sprintf("%.1f%%", curr.ErrorRate)},
					Baseline: &domain.Value{Val: base.ErrorRate, Unit: "%", Text: fmt.Sprintf("%.1f%%", base.ErrorRate)},
					Change:   &domain.Change{Delta: errDelta, Summary: fmt.Sprintf("+%.1fpp", errDelta)},
					Scope:    scope,
				},
			},
		})
	}

	// Overall error rate fallback
	if len(findings) == 0 && snapshot.BaselineOverall != nil {
		base := *snapshot.BaselineOverall
		curr := snapshot.CurrentOverall
		if curr.TotalCalls >= minCalls {
			errDelta := curr.ErrorRate - base.ErrorRate
			if errDelta >= d.cfg.ErrorRateWarnDelta {
				sev := "WARNING"
				if errDelta >= d.cfg.ErrorRateCritDelta {
					sev = "CRITICAL"
				}
				scope := domain.Scope{Role: snapshot.Scope.Role}
				findings = append(findings, domain.Finding{
					Kind:  domain.FindingRequestErrorRegression,
					Scope: scope,
					Summary: fmt.Sprintf("Overall error rate increased from %.1f%% to %.1f%% (+%.1fpp)",
						base.ErrorRate, curr.ErrorRate, errDelta),
					Severity:    sev,
					SampleCount: curr.TotalCalls,
					Evidence: []domain.Evidence{
						{
							Signal:   "error rate",
							Current:  domain.Value{Val: curr.ErrorRate, Unit: "%", Text: fmt.Sprintf("%.1f%%", curr.ErrorRate)},
							Baseline: &domain.Value{Val: base.ErrorRate, Unit: "%", Text: fmt.Sprintf("%.1f%%", base.ErrorRate)},
							Change:   &domain.Change{Delta: errDelta, Summary: fmt.Sprintf("+%.1fpp", errDelta)},
							Scope:    scope,
						},
					},
				})
			}
		}
	}

	return findings
}

// NewExceptionDetector detects exception types absent from the baseline window.
type NewExceptionDetector struct {
	cfg Config
}

func NewNewExceptionDetector(cfg Config) *NewExceptionDetector {
	return &NewExceptionDetector{cfg: cfg}
}

func (d *NewExceptionDetector) Name() string {
	return "NewException"
}

func (d *NewExceptionDetector) Detect(snapshot *domain.Snapshot) []domain.Finding {
	var findings []domain.Finding

	baseMap := make(map[string]bool)
	for _, e := range snapshot.BaselineExceptions {
		baseMap[e.Type] = true
	}

	totalTraffic := snapshot.CurrentOverall.TotalCalls

	for _, curr := range snapshot.CurrentExceptions {
		if baseMap[curr.Type] {
			continue // Existed in baseline
		}

		// Calculate traffic share if total traffic is known
		var trafficShare float64
		if totalTraffic > 0 {
			trafficShare = (float64(curr.Count) / float64(totalTraffic)) * 100.0
		}

		primaryEndpoint := ""
		if len(curr.AffectedPaths) > 0 {
			primaryEndpoint = curr.AffectedPaths[0]
		}

		scope := domain.Scope{
			Role:     snapshot.Scope.Role,
			Endpoint: primaryEndpoint,
		}

		// Determine severity: if very low traffic share (e.g. < 0.1%), lower severity
		sev := "WARNING"
		if trafficShare >= 1.0 || curr.Count >= 50 {
			sev = "CRITICAL"
		} else if trafficShare < 0.1 && curr.Count < 20 {
			sev = "LOW"
		}

		findings = append(findings, domain.Finding{
			Kind:  domain.FindingNewException,
			Scope: scope,
			Summary: fmt.Sprintf("New %s appeared (%d occurrences)",
				curr.Type, curr.Count),
			Severity:    sev,
			SampleCount: curr.Count,
			StartedAt:   &curr.FirstSeen,
			Evidence: []domain.Evidence{
				{
					Signal:  "new exception occurrences",
					Current: domain.Value{Val: float64(curr.Count), Text: fmt.Sprintf("%d occurrences", curr.Count)},
					Scope:   scope,
				},
				{
					Signal:  "exception traffic share",
					Current: domain.Value{Val: trafficShare, Unit: "%", Text: fmt.Sprintf("%.2f%% of traffic", trafficShare)},
					Scope:   scope,
				},
			},
		})
	}

	return findings
}

// ExceptionRegressionDetector detects significant spikes in existing exceptions.
type ExceptionRegressionDetector struct {
	cfg Config
}

func NewExceptionRegressionDetector(cfg Config) *ExceptionRegressionDetector {
	return &ExceptionRegressionDetector{cfg: cfg}
}

func (d *ExceptionRegressionDetector) Name() string {
	return "ExceptionRegression"
}

func (d *ExceptionRegressionDetector) Detect(snapshot *domain.Snapshot) []domain.Finding {
	var findings []domain.Finding

	baseMap := make(map[string]model.ErrorSummary)
	for _, e := range snapshot.BaselineExceptions {
		baseMap[e.Type] = e
	}

	for _, curr := range snapshot.CurrentExceptions {
		base, exists := baseMap[curr.Type]
		if !exists {
			continue // Handled by NewExceptionDetector
		}

		if base.Count < d.cfg.MinSampleCalls && curr.Count < d.cfg.MinSampleCalls {
			continue
		}

		pctChange := calcPctChange(float64(base.Count), float64(curr.Count))
		// An exception regressed if count jumped by >= 100% and delta >= 10
		if pctChange >= 100.0 && (curr.Count-base.Count) >= 10 {
			primaryEndpoint := ""
			if len(curr.AffectedPaths) > 0 {
				primaryEndpoint = curr.AffectedPaths[0]
			}
			scope := domain.Scope{
				Role:     snapshot.Scope.Role,
				Endpoint: primaryEndpoint,
			}

			findings = append(findings, domain.Finding{
				Kind:  domain.FindingExceptionRegression,
				Scope: scope,
				Summary: fmt.Sprintf("Exception %s increased +%.0f%% (%d -> %d)",
					curr.Type, pctChange, base.Count, curr.Count),
				Severity:    "WARNING",
				SampleCount: curr.Count,
				Evidence: []domain.Evidence{
					{
						Signal:   "exception occurrences",
						Current:  domain.Value{Val: float64(curr.Count)},
						Baseline: &domain.Value{Val: float64(base.Count)},
						Change:   &domain.Change{Delta: float64(curr.Count - base.Count), Pct: pctChange},
						Scope:    scope,
					},
				},
			})
		}
	}

	return findings
}
