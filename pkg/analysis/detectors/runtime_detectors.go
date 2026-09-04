package detectors

import (
	"fmt"
	"time"

	"github.com/denjamio/azlens/pkg/domain"
)

// WorkloadUnavailableDetector detects unavailable replicas or pending pods in Kubernetes workloads.
type WorkloadUnavailableDetector struct {
	cfg Config
}

func NewWorkloadUnavailableDetector(cfg Config) *WorkloadUnavailableDetector {
	return &WorkloadUnavailableDetector{cfg: cfg}
}

func (d *WorkloadUnavailableDetector) Name() string {
	return "WorkloadUnavailable"
}

func (d *WorkloadUnavailableDetector) Detect(snapshot *domain.Snapshot) []domain.Finding {
	var findings []domain.Finding

	for _, w := range snapshot.Workloads {
		if w.ReadyReplicas < w.DesiredReplicas || w.CrashLooping || w.PendingReplicas > 0 {
			scope := domain.Scope{
				Workload:  w.Name,
				Namespace: w.Namespace,
				Role:      snapshot.Scope.Role,
			}

			summary := fmt.Sprintf("Workload '%s' has %d/%d available replicas",
				w.Name, w.ReadyReplicas, w.DesiredReplicas)
			if w.CrashLooping {
				summary = fmt.Sprintf("Workload '%s' is in CrashLoopBackOff (%d/%d replicas ready)",
					w.Name, w.ReadyReplicas, w.DesiredReplicas)
			}

			findings = append(findings, domain.Finding{
				Kind:     domain.FindingWorkloadUnavailable,
				Scope:    scope,
				Summary:  summary,
				Severity: "CRITICAL",
				Evidence: []domain.Evidence{
					{
						Signal:   "available replicas",
						Current:  domain.Value{Val: float64(w.ReadyReplicas), Text: fmt.Sprintf("%d ready", w.ReadyReplicas)},
						Baseline: &domain.Value{Val: float64(w.DesiredReplicas), Text: fmt.Sprintf("%d desired", w.DesiredReplicas)},
						Scope:    scope,
					},
				},
			})
		}
	}

	return findings
}

// RestartBurstDetector detects frequent pod restarts.
type RestartBurstDetector struct {
	cfg Config
}

func NewRestartBurstDetector(cfg Config) *RestartBurstDetector {
	return &RestartBurstDetector{cfg: cfg}
}

func (d *RestartBurstDetector) Name() string {
	return "RestartBurst"
}

func (d *RestartBurstDetector) Detect(snapshot *domain.Snapshot) []domain.Finding {
	var findings []domain.Finding

	// Check workloads
	for _, w := range snapshot.Workloads {
		if w.Restarts >= 2 {
			scope := domain.Scope{
				Workload:  w.Name,
				Namespace: w.Namespace,
				Role:      snapshot.Scope.Role,
			}
			findings = append(findings, domain.Finding{
				Kind:     domain.FindingRestartBurst,
				Scope:    scope,
				Summary:  fmt.Sprintf("Workload '%s' restarted %d times in window", w.Name, w.Restarts),
				Severity: "WARNING",
				Evidence: []domain.Evidence{
					{
						Signal:  "restarts count",
						Current: domain.Value{Val: float64(w.Restarts), Text: fmt.Sprintf("%d restarts", w.Restarts)},
						Scope:   scope,
					},
				},
			})
		}
	}

	// Check individual pods
	for _, p := range snapshot.Pods {
		if p.Restarts >= 2 {
			scope := domain.Scope{
				Pod:       p.Name,
				Workload:  p.Workload,
				Namespace: p.Namespace,
				Role:      snapshot.Scope.Role,
			}
			findings = append(findings, domain.Finding{
				Kind:     domain.FindingRestartBurst,
				Scope:    scope,
				Summary:  fmt.Sprintf("Pod '%s' restarted %d times", p.Name, p.Restarts),
				Severity: "WARNING",
				Evidence: []domain.Evidence{
					{
						Signal:  "pod restarts",
						Current: domain.Value{Val: float64(p.Restarts), Text: fmt.Sprintf("%d restarts", p.Restarts)},
						Scope:   scope,
					},
				},
			})
		}
	}

	return findings
}

// OOMKilledDetector detects container terminations caused by memory pressure.
type OOMKilledDetector struct {
	cfg Config
}

func NewOOMKilledDetector(cfg Config) *OOMKilledDetector {
	return &OOMKilledDetector{cfg: cfg}
}

func (d *OOMKilledDetector) Name() string {
	return "OOMKilled"
}

func (d *OOMKilledDetector) Detect(snapshot *domain.Snapshot) []domain.Finding {
	var findings []domain.Finding

	for _, w := range snapshot.Workloads {
		if w.OOMKills > 0 {
			scope := domain.Scope{
				Workload:  w.Name,
				Namespace: w.Namespace,
				Role:      snapshot.Scope.Role,
			}
			findings = append(findings, domain.Finding{
				Kind:     domain.FindingOOMKilled,
				Scope:    scope,
				Summary:  fmt.Sprintf("%d OOM kills detected in workload '%s'", w.OOMKills, w.Name),
				Severity: "CRITICAL",
				Evidence: []domain.Evidence{
					{
						Signal:  "OOM kills",
						Current: domain.Value{Val: float64(w.OOMKills), Text: fmt.Sprintf("%d OOM kills", w.OOMKills)},
						Scope:   scope,
					},
				},
			})
		}
	}

	for _, p := range snapshot.Pods {
		if p.OOMKilled {
			scope := domain.Scope{
				Pod:       p.Name,
				Workload:  p.Workload,
				Namespace: p.Namespace,
				Role:      snapshot.Scope.Role,
			}
			findings = append(findings, domain.Finding{
				Kind:     domain.FindingOOMKilled,
				Scope:    scope,
				Summary:  fmt.Sprintf("Pod '%s' was terminated by OOMKilled", p.Name),
				Severity: "CRITICAL",
				Evidence: []domain.Evidence{
					{
						Signal:  "OOM killed",
						Current: domain.Value{Val: 1, Text: "OOMKilled"},
						Scope:   scope,
					},
				},
			})
		}
	}

	return findings
}

// ResourceSaturationDetector detects high CPU and memory pressure.
type ResourceSaturationDetector struct {
	cfg Config
}

func NewResourceSaturationDetector(cfg Config) *ResourceSaturationDetector {
	return &ResourceSaturationDetector{cfg: cfg}
}

func (d *ResourceSaturationDetector) Name() string {
	return "ResourceSaturation"
}

func (d *ResourceSaturationDetector) Detect(snapshot *domain.Snapshot) []domain.Finding {
	var findings []domain.Finding

	for _, s := range snapshot.Saturation {
		if !s.HasData {
			continue
		}

		if s.CPUPct >= 85.0 || s.MemoryPct >= 90.0 || s.IsSaturated {
			sev := "WARNING"
			if s.CPUPct >= 95.0 || s.MemoryPct >= 95.0 {
				sev = "CRITICAL"
			}

			summary := fmt.Sprintf("Resource saturation detected: CPU %.1f%%, Memory %.1f%%",
				s.CPUPct, s.MemoryPct)

			findings = append(findings, domain.Finding{
				Kind:     domain.FindingResourceSaturation,
				Scope:    s.Scope,
				Summary:  summary,
				Severity: sev,
				Evidence: []domain.Evidence{
					{
						Signal:  "CPU utilization",
						Current: domain.Value{Val: s.CPUPct, Unit: "%", Text: fmt.Sprintf("%.1f%%", s.CPUPct)},
						Scope:   s.Scope,
					},
					{
						Signal:  "Memory utilization",
						Current: domain.Value{Val: s.MemoryPct, Unit: "%", Text: fmt.Sprintf("%.1f%%", s.MemoryPct)},
						Scope:   s.Scope,
					},
				},
			})
		}
	}

	return findings
}

// TelemetryStaleDetector detects telemetry disappearance or stale signals (Section 10.6, Scenario F).
type TelemetryStaleDetector struct {
	cfg Config
}

func NewTelemetryStaleDetector(cfg Config) *TelemetryStaleDetector {
	return &TelemetryStaleDetector{cfg: cfg}
}

func (d *TelemetryStaleDetector) Name() string {
	return "TelemetryStale"
}

func (d *TelemetryStaleDetector) Detect(snapshot *domain.Snapshot) []domain.Finding {
	var findings []domain.Finding
	now := snapshot.Timestamp
	if now.IsZero() {
		now = time.Now()
	}

	// 1. Check last seen timestamp
	if snapshot.Freshness.RequestsLastSeen != nil {
		lastSeen := *snapshot.Freshness.RequestsLastSeen
		if !lastSeen.IsZero() && now.Sub(lastSeen) > d.cfg.StaleDuration {
			diff := now.Sub(lastSeen).Round(time.Minute)
			findings = append(findings, domain.Finding{
				Kind:      domain.FindingTelemetryStale,
				Scope:     snapshot.Scope,
				Summary:   fmt.Sprintf("Application telemetry stopped %s ago", diff),
				Severity:  "CRITICAL",
				StartedAt: &lastSeen,
				Evidence: []domain.Evidence{
					{
						Signal:  "telemetry last seen",
						Current: domain.Value{Text: fmt.Sprintf("%s ago (%s)", diff, lastSeen.Format("15:04:05"))},
						Scope:   snapshot.Scope,
					},
				},
			})
			return findings
		}
	}

	// 2. Disappearance: baseline had normal traffic, but current window has 0 calls
	if snapshot.BaselineOverall != nil && snapshot.BaselineOverall.TotalCalls > 10 && snapshot.CurrentOverall.TotalCalls == 0 {
		findings = append(findings, domain.Finding{
			Kind:     domain.FindingTelemetryStale,
			Scope:    snapshot.Scope,
			Summary:  "Application telemetry disappeared: zero requests received in current window",
			Severity: "CRITICAL",
			Evidence: []domain.Evidence{
				{
					Signal:   "total requests",
					Current:  domain.Value{Val: 0, Text: "0 requests"},
					Baseline: &domain.Value{Val: float64(snapshot.BaselineOverall.TotalCalls), Text: fmt.Sprintf("%d requests", snapshot.BaselineOverall.TotalCalls)},
					Scope:    snapshot.Scope,
				},
			},
		})
	}

	return findings
}

// AvailabilityFailureDetector detects synthetic availability probe failures and HTTP 503 spikes.
type AvailabilityFailureDetector struct {
	cfg Config
}

func NewAvailabilityFailureDetector(cfg Config) *AvailabilityFailureDetector {
	return &AvailabilityFailureDetector{cfg: cfg}
}

func (d *AvailabilityFailureDetector) Name() string {
	return "AvailabilityFailure"
}

func (d *AvailabilityFailureDetector) Detect(snapshot *domain.Snapshot) []domain.Finding {
	var findings []domain.Finding

	// Synthetic availability tests
	for _, a := range snapshot.Availability {
		if a.FailedTests > 0 || a.SuccessRate < 99.0 {
			scope := domain.Scope{Role: snapshot.Scope.Role, Target: a.TestName}
			findings = append(findings, domain.Finding{
				Kind:     domain.FindingAvailabilityFailure,
				Scope:    scope,
				Summary:  fmt.Sprintf("Availability test '%s' degraded (%.1f%% success rate, %d failures)", a.TestName, a.SuccessRate, a.FailedTests),
				Severity: "CRITICAL",
				Evidence: []domain.Evidence{
					{
						Signal:  "availability success rate",
						Current: domain.Value{Val: a.SuccessRate, Unit: "%", Text: fmt.Sprintf("%.1f%%", a.SuccessRate)},
						Scope:   scope,
					},
				},
			})
		}
	}

	// Severe HTTP 5xx / 503 degradation
	curr := snapshot.CurrentOverall
	if curr.TotalCalls >= d.cfg.MinSampleCalls && curr.HTTP5xx > 0 {
		var base5xxRate float64
		if snapshot.BaselineOverall != nil && snapshot.BaselineOverall.TotalCalls > 0 {
			base5xxRate = (float64(snapshot.BaselineOverall.HTTP5xx) / float64(snapshot.BaselineOverall.TotalCalls)) * 100.0
		}
		curr5xxRate := (float64(curr.HTTP5xx) / float64(curr.TotalCalls)) * 100.0
		delta5xx := curr5xxRate - base5xxRate

		if delta5xx >= d.cfg.ErrorRateWarnDelta || curr5xxRate >= 5.0 {
			scope := domain.Scope{Role: snapshot.Scope.Role}
			findings = append(findings, domain.Finding{
				Kind:     domain.FindingAvailabilityFailure,
				Scope:    scope,
				Summary:  fmt.Sprintf("HTTP 5xx error rate spiked to %.1f%% (+%.1fpp)", curr5xxRate, delta5xx),
				Severity: "CRITICAL",
				Evidence: []domain.Evidence{
					{
						Signal:   "HTTP 5xx rate",
						Current:  domain.Value{Val: curr5xxRate, Unit: "%", Text: fmt.Sprintf("%.1f%%", curr5xxRate)},
						Baseline: &domain.Value{Val: base5xxRate, Unit: "%", Text: fmt.Sprintf("%.1f%%", base5xxRate)},
						Change:   &domain.Change{Delta: delta5xx},
						Scope:    scope,
					},
				},
			})
		}
	}

	return findings
}
