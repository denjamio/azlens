package config

import (
	"fmt"
	"strings"
)

// Severity indicates the importance of a validation issue
type Severity string

const (
	SeverityError   Severity = "ERROR"
	SeverityWarning Severity = "WARNING"
	SeverityInfo    Severity = "INFO"
)

// ValidationIssue represents a configuration finding with an actionable resolution hint
type ValidationIssue struct {
	Field    string
	Severity Severity
	Message  string
	Hint     string
}

// Validate inspects a profile and returns diagnostic issues with actionable hints
func (p *Profile) Validate() []ValidationIssue {
	var issues []ValidationIssue

	// 1. App Insights check
	if strings.TrimSpace(p.Target.Insights.Name) == "" {
		issues = append(issues, ValidationIssue{
			Field:    "target.insights.name",
			Severity: SeverityError,
			Message:  "Application Insights component is not configured.",
			Hint:     "Configure 'target.insights.name' with your Azure App Insights resource name or App ID.",
		})
	}

	// 2. RoleName check (microservice isolation)
	if len(p.Target.Roles) == 0 {
		issues = append(issues, ValidationIssue{
			Field:    "target.roles",
			Severity: SeverityWarning,
			Message:  "cloud_RoleName (`target.roles`) is not configured. Queries will scan telemetry from ALL services in this App Insights resource.",
			Hint:     "Set 'target.roles: <service-name>' to isolate your microservice telemetry.",
		})
	}

	// 3. Workspace check (Log Analytics, optional)
	if strings.TrimSpace(p.Target.Logs.WorkspaceID) == "" {
		issues = append(issues, ValidationIssue{
			Field:    "target.logs.workspace_id",
			Severity: SeverityInfo,
			Message:  "Log Analytics workspace Customer ID is not set.",
			Hint:     "Set 'target.logs.workspace_id' to your Log Analytics workspace GUID to inspect database slow logs (MySqlSlowLogs) and container logs.",
		})
	}

	// 4. Database check (optional)
	if strings.TrimSpace(p.Target.Logs.Database) == "" {
		issues = append(issues, ValidationIssue{
			Field:    "target.logs.database",
			Severity: SeverityInfo,
			Message:  "Database filter ('target.logs.database') is not configured.",
			Hint:     "Set 'target.logs.database' (e.g. 'ecommerce_db') to filter MySqlSlowLogs by default.",
		})
	}

	// 5. Probes exclusion check
	if !p.Target.ExcludesProbes() {
		issues = append(issues, ValidationIssue{
			Field:    "target.exclude_probes",
			Severity: SeverityWarning,
			Message:  "exclude_probes is false; Kubernetes liveness/readiness probes will be included in telemetry.",
			Hint:     "Frequent /healthz or kube-probe requests can skew P95 latency and call volume. Set 'target.exclude_probes: true'.",
		})
	}

	// 6. Thresholds sanity check
	if p.Thresholds.LatencyWarnPct >= p.Thresholds.LatencyCritPct && p.Thresholds.LatencyCritPct > 0 {
		issues = append(issues, ValidationIssue{
			Field:    "thresholds.p95_latency",
			Severity: SeverityError,
			Message:  fmt.Sprintf("Warning threshold (%.1f%%) >= Critical threshold (%.1f%%).", p.Thresholds.LatencyWarnPct, p.Thresholds.LatencyCritPct),
			Hint:     "Ensure p95_latency_warn_pct is strictly less than p95_latency_crit_pct.",
		})
	}
	if p.Thresholds.ErrorRateWarnDelta >= p.Thresholds.ErrorRateCritDelta && p.Thresholds.ErrorRateCritDelta > 0 {
		issues = append(issues, ValidationIssue{
			Field:    "thresholds.error_rate",
			Severity: SeverityError,
			Message:  fmt.Sprintf("Error rate warning delta (%.1f%%) >= Critical delta (%.1f%%).", p.Thresholds.ErrorRateWarnDelta, p.Thresholds.ErrorRateCritDelta),
			Hint:     "Ensure error_rate_warn_delta is strictly less than error_rate_crit_delta.",
		})
	}

	// 7. Defaults sanity check
	if p.Defaults.Output != "" {
		outLower := strings.ToLower(p.Defaults.Output)
		if outLower != "table" && outLower != "markdown" && outLower != "json" {
			issues = append(issues, ValidationIssue{
				Field:    "defaults.output",
				Severity: SeverityError,
				Message:  fmt.Sprintf("Invalid output format: '%s'", p.Defaults.Output),
				Hint:     "Allowed output formats are 'table', 'markdown', or 'json'.",
			})
		}
	}
	if p.Defaults.Limit < 0 {
		issues = append(issues, ValidationIssue{
			Field:    "defaults.limit",
			Severity: SeverityError,
			Message:  "defaults.limit must be greater than zero.",
			Hint:     "Set 'defaults.limit' to a positive number like 10 or 15.",
		})
	}

	return issues
}
