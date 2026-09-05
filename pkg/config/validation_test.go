package config

import (
	"testing"
)

func TestProfileValidation(t *testing.T) {
	// Profile with missing values and invalid threshold
	prof := Profile{
		Name: "Test",
		Target: TargetConfig{
			InsightsName:  "",
			Logs:          LogsConfig{Database: ""},
			Service:       "",
			RoleName:      "",
			ExcludeProbes: BoolPtr(false),
		},
		Thresholds: ProfileThresholds{
			LatencyWarnPct: 30.0,
			LatencyCritPct: 20.0,
		},
	}

	issues := prof.Validate()
	if len(issues) == 0 {
		t.Fatalf("expected validation issues, got 0")
	}

	hasAppErr := false
	hasDatabaseErr := false
	hasServiceErr := false
	hasProbesWarn := false
	hasThresholdErr := false

	for _, iss := range issues {
		if iss.Field == "target.insights_name" && iss.Severity == SeverityError {
			hasAppErr = true
		}
		if iss.Field == "shared.logs.database" && iss.Severity == SeverityError {
			hasDatabaseErr = true
		}
		if iss.Field == "target.service" && iss.Severity == SeverityError {
			hasServiceErr = true
		}
		if iss.Field == "target.exclude_probes" && iss.Severity == SeverityWarning {
			hasProbesWarn = true
		}
		if iss.Field == "thresholds.p95_latency" && iss.Severity == SeverityError {
			hasThresholdErr = true
		}
	}

	if !hasAppErr {
		t.Errorf("expected error for unconfigured App Insights")
	}
	if !hasDatabaseErr {
		t.Errorf("expected error for unconfigured logs database")
	}
	if !hasServiceErr {
		t.Errorf("expected error for unconfigured Service")
	}
	if !hasProbesWarn {
		t.Errorf("expected warning for disabled exclude_probes")
	}
	if !hasThresholdErr {
		t.Errorf("expected error for invalid latency threshold")
	}

	// Backwards compatibility check: legacy Insights.Name and Role satisfy requirements
	legacyProf := Profile{
		Name: "Legacy",
		Target: TargetConfig{
			Insights:      InsightsConfig{Name: "legacy-app"},
			Logs:          LogsConfig{Database: "legacy-db"},
			Role:          "legacy-role",
			ExcludeProbes: BoolPtr(true),
		},
	}
	legacyIssues := legacyProf.Validate()
	for _, iss := range legacyIssues {
		if iss.Severity == SeverityError {
			t.Errorf("unexpected validation error for legacy config: %+v", iss)
		}
	}
}
