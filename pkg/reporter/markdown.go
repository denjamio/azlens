package reporter

import (
	"fmt"
	"io"
	"os"
	"strings"

	"github.com/denjamio/azlens/pkg/model"
)

// PrintDiffMarkdown generates a GitHub/GitLab markdown report
func PrintDiffMarkdown(w io.Writer, report model.DiffReport) {
	if w == nil {
		w = os.Stdout
	}

	statusBadge := "🟢 **HEALTHY**"
	switch report.OverallVerdict {
	case model.SeverityCritical:
		statusBadge = "🔴 **REGRESSION DETECTED**"
	case model.SeverityWarning:
		statusBadge = "🟡 **WARNING / DEGRADATIONS**"
	case model.SeverityImprove:
		statusBadge = "🚀 **PERFORMANCE IMPROVED**"
	}

	fmt.Fprintf(w, "## 🚀 AzLens Deployment Regression Report: `%s`\n\n", report.AppName)
	fmt.Fprintf(w, "**Status:** %s\n\n", statusBadge)
	fmt.Fprintf(w, "- **Baseline:** `%s` to `%s`\n", report.BaselineWindow.Start.Format("2006-01-02 15:04:05 UTC"), report.BaselineWindow.End.Format("15:04:05 UTC"))
	fmt.Fprintf(w, "- **Post-Deploy:** `%s` to `%s`\n\n", report.CurrentWindow.Start.Format("2006-01-02 15:04:05 UTC"), report.CurrentWindow.End.Format("15:04:05 UTC"))

	// High-level metrics
	fmt.Fprintln(w, "### 📊 Overall Performance Metrics")
	fmt.Fprintln(w, "| Metric | Baseline | Post-Deploy | Delta | Status |")
	fmt.Fprintln(w, "| :--- | :--- | :--- | :--- | :--- |")

	for _, d := range report.SummaryDeltas {
		badge := "🟢 OK"
		if d.Severity == model.SeverityCritical {
			badge = "🔴 Critical"
		} else if d.Severity == model.SeverityWarning {
			badge = "🟡 Warning"
		} else if d.Severity == model.SeverityImprove {
			badge = "🚀 Improved"
		}
		fmt.Fprintf(w, "| **%s** | %.2f%s | %.2f%s | %+.2f%s (%+.1f%%) | %s |\n",
			d.MetricName, d.Baseline, d.Unit, d.Current, d.Unit, d.Delta, d.Unit, d.Percentage, badge)
	}
	fmt.Fprintln(w)

	// Endpoint Diffs
	if len(report.EndpointDeltas) > 0 {
		fmt.Fprintln(w, "### 📌 Endpoints Breakdown")
		fmt.Fprintln(w, "| Endpoint | Baseline P95 | Post P95 | P95 Δ% | Baseline Err% | Post Err% | Status |")
		fmt.Fprintln(w, "| :--- | :--- | :--- | :--- | :--- | :--- | :--- |")

		for _, ep := range report.EndpointDeltas {
			badge := "🟢"
			if ep.Severity == model.SeverityCritical {
				badge = "🔴"
			} else if ep.Severity == model.SeverityWarning {
				badge = "🟡"
			} else if ep.Severity == model.SeverityImprove {
				badge = "🚀"
			}
			fmt.Fprintf(w, "| `%s` | %.1fms | %.1fms | %+.1f%% | %.2f%% | %.2f%% | %s |\n",
				ep.Name, ep.Baseline.Latency.P95, ep.Current.Latency.P95, ep.P95DeltaPct, ep.Baseline.ErrorRate, ep.Current.ErrorRate, badge)
		}
		fmt.Fprintln(w)
	}

	// Regressed dependencies
	if len(report.RegressedDeps) > 0 {
		fmt.Fprintln(w, "### ⚠️ Regressed Dependencies / Slow Queries")
		fmt.Fprintln(w, "| Type | Target | Query / Command | Baseline P95 | Post P95 | Δ% |")
		fmt.Fprintln(w, "| :--- | :--- | :--- | :--- | :--- | :--- |")

		for _, dep := range report.RegressedDeps {
			fmt.Fprintf(w, "| **%s** | `%s` | `%s` | %.1fms | %.1fms | %+.1f%% |\n",
				dep.Type, dep.Target, dep.Name, dep.Baseline.Latency.P95, dep.Current.Latency.P95, dep.P95DeltaPct)
		}
		fmt.Fprintln(w)
	}

	// N+1 SQL Regressions
	if len(report.FanoutDeltas) > 0 {
		fmt.Fprintln(w, "### 🔍 N+1 SQL Regressions Detected")
		fmt.Fprintln(w, "| Endpoint | Baseline SQL Calls/Req | Post-Deploy SQL Calls/Req | Spike Δ% |")
		fmt.Fprintln(w, "| :--- | :--- | :--- | :--- |")
		for _, f := range report.FanoutDeltas {
			fmt.Fprintf(w, "| `%s` | %.1f | **%.1f** | **%+.1f%%** |\n",
				f.Endpoint, f.BaselineCalls, f.CurrentCalls, f.DeltaPct)
		}
		fmt.Fprintln(w)
	}

	// New Errors
	if len(report.NewErrors) > 0 {
		fmt.Fprintln(w, "### 🚨 New Errors & Exceptions")
		for i, err := range report.NewErrors {
			fmt.Fprintf(w, "%d. **%s** (Count: `%d`)\n", i+1, err.Type, err.Count)
			fmt.Fprintf(w, "   - *Message:* `%s`\n", err.Message)
			if len(err.AffectedPaths) > 0 {
				fmt.Fprintf(w, "   - *Affected Endpoints:* `%v`\n", err.AffectedPaths)
			}
		}
		fmt.Fprintln(w)
	}

	// Root Cause Hints
	if len(report.RootCauseHints) > 0 {
		fmt.Fprintln(w, "### 💡 Root-Cause & Correlation Insights")
		for i, hint := range report.RootCauseHints {
			fmt.Fprintf(w, "%d. %s\n", i+1, hint)
		}
		fmt.Fprintln(w)
	}
}

// PrintRequestsMarkdown formats endpoint metrics as Markdown table
func PrintRequestsMarkdown(w io.Writer, requests []model.RequestMetric) {
	if w == nil {
		w = os.Stdout
	}
	if len(requests) == 0 {
		fmt.Fprintln(w, "*No endpoint telemetry data found for the specified window.*")
		return
	}

	fmt.Fprintln(w, "### ⚡ Slow Endpoints & Requests (Sorted by P95 Latency)")
	fmt.Fprintln(w, "| Endpoint | Calls | Error % | Avg | P50 | P90 | P95 | P99 |")
	fmt.Fprintln(w, "| :--- | :--- | :--- | :--- | :--- | :--- | :--- | :--- |")

	for _, r := range requests {
		fmt.Fprintf(w, "| `%s` | %d | %.2f%% | %.1fms | %.1fms | %.1fms | %.1fms | %.1fms |\n",
			r.Name, r.TotalCalls, r.ErrorRate, r.Latency.Avg, r.Latency.P50, r.Latency.P90, r.Latency.P95, r.Latency.P99)
	}
}

// PrintDependenciesMarkdown formats slow queries / external dependencies as Markdown table
func PrintDependenciesMarkdown(w io.Writer, deps []model.DependencyMetric) {
	if w == nil {
		w = os.Stdout
	}
	if len(deps) == 0 {
		fmt.Fprintln(w, "*No dependency data found for the specified window.*")
		return
	}

	fmt.Fprintln(w, "### 🗄️ Slow Database Queries & Remote Dependencies")
	fmt.Fprintln(w, "| Type | Target | Query / Command | Calls | Error % | Avg | P95 | P99 |")
	fmt.Fprintln(w, "| :--- | :--- | :--- | :--- | :--- | :--- | :--- | :--- |")

	for _, d := range deps {
		fmt.Fprintf(w, "| **%s** | `%s` | `%s` | %d | %.2f%% | %.1fms | %.1fms | %.1fms |\n",
			d.Type, d.Target, d.Name, d.TotalCalls, d.ErrorRate, d.Latency.Avg, d.Latency.P95, d.Latency.P99)
	}
}

// PrintErrorsMarkdown formats error and exception summaries as Markdown
func PrintErrorsMarkdown(w io.Writer, errors []model.ErrorSummary) {
	if w == nil {
		w = os.Stdout
	}
	if len(errors) == 0 {
		fmt.Fprintln(w, "✅ *No exceptions or errors detected in the specified window.*")
		return
	}

	fmt.Fprintln(w, "### 🚨 Exceptions & Error Signatures Breakdown")
	for i, e := range errors {
		fmt.Fprintf(w, "%d. **%s** (`%d` occurrences)\n", i+1, e.Type, e.Count)
		fmt.Fprintf(w, "   - **Message:** `%s`\n", e.Message)
		if len(e.AffectedPaths) > 0 {
			fmt.Fprintf(w, "   - **Affected Endpoints:** `%v`\n", e.AffectedPaths)
		}
	}
}

// PrintGenericMarkdown formats arbitrary tabular query results as Markdown
func PrintGenericMarkdown(w io.Writer, res model.GenericQueryResult) {
	if w == nil {
		w = os.Stdout
	}
	if len(res.Columns) == 0 || len(res.Rows) == 0 {
		fmt.Fprintln(w, "*No data returned for this query.*")
		return
	}

	// Header
	fmt.Fprintf(w, "| %s |\n", strings.Join(res.Columns, " | "))
	fmt.Fprint(w, "|")
	for range res.Columns {
		fmt.Fprint(w, " :--- |")
	}
	fmt.Fprintln(w)

	// Rows
	for _, row := range res.Rows {
		rowStrs := make([]string, len(row))
		for i, v := range row {
			rowStrs[i] = fmt.Sprintf("%v", v)
		}
		fmt.Fprintf(w, "| %s |\n", strings.Join(rowStrs, " | "))
	}
}

// PrintFanoutMarkdown renders N+1 query metrics in Markdown
func PrintFanoutMarkdown(w io.Writer, fanout []model.FanoutMetric) {
	if w == nil {
		w = os.Stdout
	}
	fmt.Fprintln(w, "### 🔍 N+1 Query Detection (SQL Calls per Request)")
	fmt.Fprintln(w, "| Endpoint | Requests | Avg SQL Calls | Max SQL Calls | Avg SQL Ms | Avg Total Ms |")
	fmt.Fprintln(w, "| :--- | :--- | :--- | :--- | :--- | :--- |")

	for _, f := range fanout {
		fmt.Fprintf(w, "| `%s` | %d | **%.1f** | %d | %.1fms | %.1fms |\n",
			f.Endpoint, f.TotalRequests, f.AvgSQLCalls, f.MaxSQLCalls, f.AvgSQLDurationMs, f.AvgEndpointDurationMs)
	}
}

// PrintLatencyBreakdownMarkdown renders latency breakdown across dependencies in Markdown
func PrintLatencyBreakdownMarkdown(w io.Writer, attrs []model.LatencyBreakdown) {
	if w == nil {
		w = os.Stdout
	}
	fmt.Fprintln(w, "### ⏱️ Latency Breakdown")
	fmt.Fprintln(w, "| Endpoint | Avg Total | % Database | % Ext APIs | % Cache | % App Code |")
	fmt.Fprintln(w, "| :--- | :--- | :--- | :--- | :--- | :--- |")

	for _, a := range attrs {
		fmt.Fprintf(w, "| `%s` | %.1fms | %.1f%% | %.1f%% | %.1f%% | %.1f%% |\n",
			a.Endpoint, a.AvgDurationMs, a.PctDatabase, a.PctExternalAPI, a.PctCache, a.PctAppCode)
	}
}

// PrintDeprecationsMarkdown renders framework and library deprecation warnings in Markdown
func PrintDeprecationsMarkdown(w io.Writer, deps []model.DeprecationSummary) {
	if w == nil {
		w = os.Stdout
	}
	if len(deps) == 0 {
		fmt.Fprintln(w, "*No deprecation warnings detected.*")
		return
	}

	fmt.Fprintln(w, "### ⚠️ Framework & Library Deprecations")
	fmt.Fprintln(w, "| Count | Last Seen | Deprecation Warning | Affected Endpoints |")
	fmt.Fprintln(w, "| :--- | :--- | :--- | :--- |")

	for _, d := range deps {
		lastSeenStr := "-"
		if !d.LastSeen.IsZero() {
			lastSeenStr = d.LastSeen.Format("2006-01-02 15:04:05 UTC")
		}
		eps := "-"
		if len(d.AffectedEndpoints) > 0 {
			eps = fmt.Sprintf("`%s`", strings.Join(d.AffectedEndpoints, "`, `"))
		}
		cleanMsg := strings.ReplaceAll(d.Message, "|", "\\|")
		cleanMsg = strings.ReplaceAll(cleanMsg, "\n", " ")
		fmt.Fprintf(w, "| **%d** | %s | %s | %s |\n", d.Count, lastSeenStr, cleanMsg, eps)
	}
}

// PrintSlowLogsMarkdown renders list of MySQL slow query logs in Markdown
func PrintSlowLogsMarkdown(w io.Writer, logs []model.SlowLogEntry) {
	if w == nil {
		w = os.Stdout
	}
	if len(logs) == 0 {
		fmt.Fprintln(w, "*No slow queries recorded in this time window.*")
		return
	}

	fmt.Fprintln(w, "### 🐢 MySQL Slow Query Logs")
	fmt.Fprintln(w, "| Timestamp | Duration | Rows Examined | Rows Sent | SQL Query |")
	fmt.Fprintln(w, "| :--- | :--- | :--- | :--- | :--- |")

	for _, l := range logs {
		tsStr := "-"
		if !l.Timestamp.IsZero() {
			tsStr = l.Timestamp.UTC().Format("2006-01-02 15:04:05 UTC")
		}
		cleanSQL := strings.ReplaceAll(l.SQLText, "|", "\\|")
		cleanSQL = strings.ReplaceAll(cleanSQL, "\n", " ")
		cleanSQL = truncate(cleanSQL, 100)
		fmt.Fprintf(w, "| `%s` | **%.2fs** | %s | %s | `%s` |\n",
			tsStr, l.DurationSec, formatNumber(l.RowsExamined), formatNumber(l.RowsSent), cleanSQL)
	}
}
