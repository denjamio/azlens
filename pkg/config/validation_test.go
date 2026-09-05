package config

import (
	"testing"
)

func TestProfileValidation(t *testing.T) {
	// Profile with missing values and invalid threshold
	prof := Profile{
		Name: "Test",
		Target: TargetConfig{
			Insights:      InsightsConfig{Name: ""},
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
		if iss.Field == "insights.name" && iss.Severity == SeverityError {
			hasAppErr = true
		}
		if iss.Field == "shared.logs.database" && iss.Severity == SeverityError {
			hasDatabaseErr = true
		}
		if iss.Field == "service" && iss.Severity == SeverityError {
			hasServiceErr = true
		}
		if iss.Field == "shared.exclude_probes" && iss.Severity == SeverityWarning {
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
}
