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
	if strings.TrimSpace(p.Target.InsightsName) == "" {
		issues = append(issues, ValidationIssue{
			Field:    "target.insights_name",
			Severity: SeverityError,
			Message:  "Application Insights component is not configured.",
			Hint:     "Configure 'target.insights_name' with your Azure App Insights resource name or App ID.",
		})
	}

	// 2. Logs database check (mandatory multi-tenancy)
	if strings.TrimSpace(p.Target.Logs.Database) == "" {
		issues = append(issues, ValidationIssue{
			Field:    "shared.logs.database",
			Severity: SeverityError,
			Message:  "Logs database (`logs.database`) is not configured. Database is mandatory to ensure tenant isolation.",
			Hint:     "Set 'shared.logs.database: <dbname>' (e.g. 'backend_ror') in your configuration.",
		})
	}

	// 3. Service targeting check (mandatory microservice isolation)
	if strings.TrimSpace(p.Target.Service) == "" && strings.TrimSpace(p.Target.RoleName) == "" {
		issues = append(issues, ValidationIssue{
			Field:    "target.service",
			Severity: SeverityError,
			Message:  "Target service is not configured. Service targeting is mandatory to isolate microservice telemetry and prevent unbounded scans.",
			Hint:     "Configure 'defaults.service: <service-name>', declare services under 'shared.services', or specify '--service <name>' / '-s <name>'.",
		})
	}

	// 4. Workspace check (Log Analytics, optional)
	if strings.TrimSpace(p.Target.Logs.WorkspaceID) == "" {
		issues = append(issues, ValidationIssue{
			Field:    "target.logs.workspace_id",
			Severity: SeverityInfo,
			Message:  "Log Analytics workspace Customer ID is not set.",
			Hint:     "Set 'target.logs.workspace_id' to your Log Analytics workspace GUID to inspect database slow logs (MySqlSlowLogs).",
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
