package detectors

import (
	"fmt"
	"time"

	"github.com/denjamio/azlens/pkg/domain"
)

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
