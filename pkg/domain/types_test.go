package domain_test

import (
	"encoding/json"
	"testing"
	"time"

	"github.com/denjamio/azlens/pkg/domain"
)

func TestAnalysisResultJSONSchema(t *testing.T) {
	now := time.Now()
	res := domain.AnalysisResult{
		SchemaVersion: "1",
		Profile: domain.ProfileContext{
			Name:        "prod",
			DisplayName: "Production",
		},
		Scope: domain.ScopeContext{
			Roles: []string{"checkout"},
		},
		Window: domain.WindowContext{
			Label:    "last 60m",
			Duration: "60m",
			Start:    now.Add(-60 * time.Minute),
			End:      now,
		},
		State: domain.HealthStateDegraded,
		Coverage: []domain.CapabilityStatus{
			{
				Capability: domain.CapabilityRequests,
				State:      domain.CapabilityStateAvailable,
			},
		},
		Problems: []domain.Problem{
			{
				Kind:     domain.ProblemKindDegradation,
				Priority: 1,
				Scope:    domain.Scope{Role: "checkout"},
				Summary:  "Checkout is degraded",
				Impact: domain.Impact{
					P95Current:   "1.2s",
					P95Baseline:  "380ms",
					ErrorCurrent: "3.7%",
					TrafficPct:   24.0,
				},
				Cause: &domain.Cause{
					Summary:  "payments-api",
					Strength: domain.EvidenceStrengthStrong,
					Evidence: []domain.Evidence{
						{
							Signal:  "dependency p95 increased 186%",
							Current: domain.Value{Val: 910, Unit: "ms"},
						},
					},
				},
				Action: &domain.Action{
					Summary: "Inspect payments-api calls",
					Command: &domain.Command{
						Display: "azlens explain checkout",
					},
				},
			},
		},
		Watching: []domain.WatchingItem{},
	}

	data, err := json.Marshal(res)
	if err != nil {
		t.Fatalf("failed to marshal AnalysisResult: %v", err)
	}

	var raw map[string]interface{}
	if err := json.Unmarshal(data, &raw); err != nil {
		t.Fatalf("failed to unmarshal JSON: %v", err)
	}

	requiredFields := []string{"schema_version", "profile", "scope", "window", "state", "coverage", "problems", "watching"}
	for _, field := range requiredFields {
		if _, exists := raw[field]; !exists {
			t.Errorf("missing required JSON field %q in serialized AnalysisResult", field)
		}
	}

	if raw["schema_version"] != "1" {
		t.Errorf("expected schema_version '1', got %v", raw["schema_version"])
	}
	if raw["state"] != "degraded" {
		t.Errorf("expected state 'degraded', got %v", raw["state"])
	}
}

func TestScopeString(t *testing.T) {
	cases := []struct {
		scope    domain.Scope
		expected string
	}{
		{scope: domain.Scope{Endpoint: "POST /checkout"}, expected: "POST /checkout"},
		{scope: domain.Scope{Role: "order-service"}, expected: "order-service"},
		{scope: domain.Scope{Workload: "backend"}, expected: "backend"},
		{scope: domain.Scope{Target: "payments-api"}, expected: "payments-api"},
		{scope: domain.Scope{Pod: "checkout-abc-123"}, expected: "checkout-abc-123"},
		{scope: domain.Scope{}, expected: ""},
	}

	for _, tc := range cases {
		if got := tc.scope.String(); got != tc.expected {
			t.Errorf("Scope.String() = %q, expected %q", got, tc.expected)
		}
	}
}
