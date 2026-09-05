package reporter

import (
	"fmt"
	"io"
	"os"
	"strings"

	"github.com/denjamio/azlens/pkg/model"
)

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
		if target, ok := e.InstrumentationTarget(); ok {
			fmt.Fprintf(w, "   - ℹ️ *Instrumentation noise:* emitted by the auto-instrumentation SDK while hooking `%s` — "+
				"a module is missing or not importable in the runtime image (verify the instrumentation package is installed), "+
				"not an application error\n", target)
		}
	}
}

// PrintGenericMarkdown formats arbitrary tabular query results as Markdown with
// humanized headers and normalized cell values
func PrintGenericMarkdown(w io.Writer, res model.GenericQueryResult) {
	if w == nil {
		w = os.Stdout
	}
	if len(res.Columns) == 0 || len(res.Rows) == 0 {
		fmt.Fprintln(w, "*No data returned for this query.*")
		return
	}

	headers := make([]string, len(res.Columns))
	for i, c := range res.Columns {
		headers[i] = humanizeHeader(c)
	}

	// Header
	fmt.Fprintf(w, "| %s |\n", strings.Join(headers, " | "))
	fmt.Fprint(w, "|")
	for range headers {
		fmt.Fprint(w, " :--- |")
	}
	fmt.Fprintln(w)

	// Rows
	for _, row := range res.Rows {
		rowStrs := make([]string, len(headers))
		for i := range headers {
			var v interface{}
			if i < len(row) {
				v = row[i]
			}
			rowStrs[i] = markdownCell(res.Columns[i], v)
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

// PrintSlowLogsGroupMarkdown renders slow query logs aggregated by normalized
// SQL fingerprint in Markdown
func PrintSlowLogsGroupMarkdown(w io.Writer, groups []model.SlowLogGroup) {
	if w == nil {
		w = os.Stdout
	}
	if len(groups) == 0 {
		fmt.Fprintln(w, "*No slow queries recorded in this time window.*")
		return
	}

	fmt.Fprintln(w, "### 🐢 MySQL Slow Query Logs (Grouped by SQL Fingerprint)")
	fmt.Fprintln(w, "| SQL Fingerprint | Executions | Avg | Max | Total Time | Rows Examined (avg) | Last Seen |")
	fmt.Fprintln(w, "| :--- | :--- | :--- | :--- | :--- | :--- | :--- |")

	for _, g := range groups {
		lastSeenStr := "-"
		if !g.LastSeen.IsZero() {
			lastSeenStr = g.LastSeen.UTC().Format("2006-01-02 15:04:05 UTC")
		}
		fp := strings.ReplaceAll(g.Fingerprint, "|", "\\|")
		fp = strings.ReplaceAll(fp, "\n", " ")
		fmt.Fprintf(w, "| `%s` | **%s** | %s | %s | **%s** | %s | %s |\n",
			fp, formatNumber(g.Executions), formatLatencyHuman(g.AvgMs), formatLatencyHuman(g.MaxMs),
			formatLatencyHuman(g.TotalMs), formatNumber(int64(g.AvgRowsExamined)), lastSeenStr)
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
	fmt.Fprintln(w, "| Timestamp | Duration | Rows Examined | Rows Returned | SQL Query |")
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
