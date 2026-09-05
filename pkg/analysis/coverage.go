package analysis

import (
	"fmt"
	"time"

	"github.com/denjamio/azlens/pkg/domain"
)

// CapabilityEvaluator assesses the 5 observability capabilities and computes
// overall environment health (Section 7 & 9).
type CapabilityEvaluator struct{}

func NewCapabilityEvaluator() *CapabilityEvaluator {
	return &CapabilityEvaluator{}
}

// EvaluateCoverage determines status for each capability.
func (c *CapabilityEvaluator) EvaluateCoverage(snapshot *domain.Snapshot) []domain.CapabilityStatus {
	allCaps := []domain.CapabilityType{
		domain.CapabilityRequests,
		domain.CapabilityDependencies,
		domain.CapabilityExceptions,
		domain.CapabilityDatabaseSlowLogs,
	}

	var statuses []domain.CapabilityStatus

	for _, capType := range allCaps {
		status := c.evaluateCapability(snapshot, capType)
		statuses = append(statuses, status)
	}

	return statuses
}

func (c *CapabilityEvaluator) evaluateCapability(snapshot *domain.Snapshot, capType domain.CapabilityType) domain.CapabilityStatus {
	// If explicit state was pre-populated
	if state, ok := snapshot.CapabilityStates[capType]; ok {
		return domain.CapabilityStatus{
			Capability: capType,
			State:      state,
		}
	}

	// Check for query errors
	if qErr, hasErr := snapshot.QueryErrors[capType]; hasErr && qErr != nil {
		return domain.CapabilityStatus{
			Capability:  capType,
			State:       domain.CapabilityStateUnavailable,
			Reason:      qErr.Error(),
			Consequence: "Telemetry query failed",
		}
	}

	now := snapshot.Timestamp
	if now.IsZero() {
		now = time.Now()
	}

	switch capType {
	case domain.CapabilityRequests:
		if snapshot.Freshness.RequestsLastSeen != nil {
			last := *snapshot.Freshness.RequestsLastSeen
			if !last.IsZero() && now.Sub(last) > 15*time.Minute {
				return domain.CapabilityStatus{
					Capability:  capType,
					State:       domain.CapabilityStateStale,
					LastSeen:    &last,
					Reason:      fmt.Sprintf("last seen %s ago", now.Sub(last).Round(time.Minute)),
					Consequence: "Application health cannot be confirmed",
				}
			}
		}
		if snapshot.CurrentOverall.TotalCalls > 0 || len(snapshot.CurrentEndpoints) > 0 {
			return domain.CapabilityStatus{
				Capability: capType,
				State:      domain.CapabilityStateAvailable,
			}
		}
		// If baseline had calls but current has 0
		if snapshot.BaselineOverall != nil && snapshot.BaselineOverall.TotalCalls > 0 && snapshot.CurrentOverall.TotalCalls == 0 {
			return domain.CapabilityStatus{
				Capability:  capType,
				State:       domain.CapabilityStateStale,
				Reason:      "traffic stopped in current window",
				Consequence: "Application health cannot be determined",
			}
		}
		return domain.CapabilityStatus{
			Capability: capType,
			State:      domain.CapabilityStateAvailable,
		}

	case domain.CapabilityDependencies:
		if len(snapshot.CurrentDependencies) > 0 {
			return domain.CapabilityStatus{Capability: capType, State: domain.CapabilityStateAvailable}
		}
		return domain.CapabilityStatus{Capability: capType, State: domain.CapabilityStateAvailable}

	case domain.CapabilityExceptions:
		if len(snapshot.CurrentExceptions) > 0 {
			return domain.CapabilityStatus{Capability: capType, State: domain.CapabilityStateAvailable}
		}
		return domain.CapabilityStatus{Capability: capType, State: domain.CapabilityStateAvailable}

	case domain.CapabilityDatabaseSlowLogs:
		if len(snapshot.SlowLogs) > 0 {
			return domain.CapabilityStatus{Capability: capType, State: domain.CapabilityStateAvailable}
		}
		return domain.CapabilityStatus{Capability: capType, State: domain.CapabilityStateNotConfigured}
	}

	return domain.CapabilityStatus{Capability: capType, State: domain.CapabilityStateNotConfigured}
}

// DetermineHealthState determines overall environment state (healthy, degraded, unknown).
func (c *CapabilityEvaluator) DetermineHealthState(
	coverage []domain.CapabilityStatus,
	problems []domain.Problem,
	findings []domain.Finding,
) (domain.HealthState, string) {
	// 1. Check if critical capability is stale or unavailable
	for _, stat := range coverage {
		if stat.Capability == domain.CapabilityRequests {
			if stat.State == domain.CapabilityStateStale {
				msg := "Application telemetry stopped."
				if stat.LastSeen != nil {
					diff := time.Since(*stat.LastSeen).Round(time.Minute)
					msg = fmt.Sprintf("Application telemetry stopped %s ago.\n\nAzLens cannot determine current application health.", diff)
				}
				return domain.HealthStateUnknown, msg
			}
			if stat.State == domain.CapabilityStateUnavailable {
				msg := "Application telemetry query failed. AzLens cannot determine current application health."
				if stat.Reason != "" {
					msg = fmt.Sprintf("Application telemetry query failed: %s\n\nAzLens cannot determine current application health.", stat.Reason)
				}
				return domain.HealthStateUnknown, msg
			}
		}
	}

	// Check if any finding is TelemetryStale
	for _, f := range findings {
		if f.Kind == domain.FindingTelemetryStale {
			return domain.HealthStateUnknown, f.Summary + ".\n\nAzLens cannot determine current application health."
		}
	}

	// 2. Actionable problems exist -> Degraded (Section 7)
	if len(problems) > 0 {
		return domain.HealthStateDegraded, ""
	}

	// 3. Otherwise normal -> Healthy (Section 7)
	return domain.HealthStateHealthy, "Everything looks normal."
}
