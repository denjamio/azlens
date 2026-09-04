package config

import (
	"testing"
)

func TestProfileValidation(t *testing.T) {
	// Profile with missing values and invalid threshold
	prof := Profile{
		Name: "Test",
		Target: TargetConfig{
			Insights:      InsightsConfig{Name: ""}, // Missing -> Error
			Logs:          LogsConfig{Database: ""}, // Missing -> Error
			Service:       "",                       // Missing -> Error
			Role:          "",
			ExcludeProbes: BoolPtr(false), // Explicit false -> Warning
		},
		Thresholds: ProfileThresholds{
			LatencyWarnPct: 30.0,
			LatencyCritPct: 20.0, // Invalid: warn > crit -> Error
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
		if iss.Field == "target.insights.name" && iss.Severity == SeverityError {
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
}
