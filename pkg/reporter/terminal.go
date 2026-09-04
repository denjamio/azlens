package reporter

import (
	"fmt"
	"io"
	"os"
	"strconv"
	"strings"

	"github.com/fatih/color"
	"github.com/olekukonko/tablewriter"

	"github.com/denjamio/azlens/pkg/model"
)

var (
	colorGreen  = color.New(color.FgGreen, color.Bold)
	colorYellow = color.New(color.FgYellow, color.Bold)
	colorRed    = color.New(color.FgRed, color.Bold)
	colorCyan   = color.New(color.FgCyan, color.Bold)
	colorGray   = color.New(color.FgHiBlack)
)

// PrintDiffTerminal renders the full pre-vs-post deploy regression report to terminal
func PrintDiffTerminal(w io.Writer, report model.DiffReport) {
	if w == nil {
		w = os.Stdout
	}

	fmt.Fprintln(w)
	colorCyan.Fprintf(w, "═══════════════════════════════════════════════════════════════════════\n")
	colorCyan.Fprintf(w, " 🚀 AZLENS DEPLOY REGRESSION REPORT: %s\n", report.AppName)
	colorCyan.Fprintf(w, "═══════════════════════════════════════════════════════════════════════\n")
	fmt.Fprintf(w, " Baseline Window: %s to %s\n", report.BaselineWindow.Start.Format("15:04:05"), report.BaselineWindow.End.Format("15:04:05"))
	fmt.Fprintf(w, " Post-Deploy Window: %s to %s\n\n", report.CurrentWindow.Start.Format("15:04:05"), report.CurrentWindow.End.Format("15:04:05"))

	// 1. Overall Summary Verdict
	fmt.Fprintf(w, " Overall Status: ")
	switch report.OverallVerdict {
	case model.SeverityCritical:
		colorRed.Fprintf(w, "❌ REGRESSION DETECTED (Critical breaches detected)\n\n")
	case model.SeverityWarning:
		colorYellow.Fprintf(w, "⚠️  WARNING (Degradations detected)\n\n")
	case model.SeverityImprove:
		colorGreen.Fprintf(w, "🚀 IMPROVED (Performance improved after deploy)\n\n")
	default:
		colorGreen.Fprintf(w, "✅ HEALTHY (No significant regressions detected)\n\n")
	}

	// 2. High-Level Metrics Table
	table := tablewriter.NewWriter(w)
	table.SetHeader([]string{"Metric", "Baseline", "Post-Deploy", "Delta", "Status"})
	table.SetBorder(true)
	table.SetAutoWrapText(false)

	for _, d := range report.SummaryDeltas {
		statusStr := formatStatus(d.Severity)
		deltaStr := fmt.Sprintf("%+.2f%s (%+.1f%%)", d.Delta, d.Unit, d.Percentage)
		table.Append([]string{
			d.MetricName,
			fmt.Sprintf("%.2f%s", d.Baseline, d.Unit),
			fmt.Sprintf("%.2f%s", d.Current, d.Unit),
			deltaStr,
			statusStr,
		})
	}
	table.Render()
	fmt.Fprintln(w)

	// 3. Endpoint Breakdown
	if len(report.EndpointDeltas) > 0 {
		colorCyan.Fprintf(w, "📌 Per-Endpoint Latency & Error Rate Diff:\n")
		epTable := tablewriter.NewWriter(w)
		epTable.SetHeader([]string{"Endpoint", "Base P95", "Post P95", "P95 Δ%", "Base Err%", "Post Err%", "Status"})
		epTable.SetBorder(true)

		for _, ep := range report.EndpointDeltas {
			epTable.Append([]string{
				ep.Name,
				fmt.Sprintf("%.1fms", ep.Baseline.Latency.P95),
				fmt.Sprintf("%.1fms", ep.Current.Latency.P95),
				fmt.Sprintf("%+.1f%%", ep.P95DeltaPct),
				fmt.Sprintf("%.2f%%", ep.Baseline.ErrorRate),
				fmt.Sprintf("%.2f%%", ep.Current.ErrorRate),
				formatStatus(ep.Severity),
			})
		}
		epTable.Render()
		fmt.Fprintln(w)
	}

	// 4. Regressed Dependencies (Slow Queries)
	if len(report.RegressedDeps) > 0 {
		colorYellow.Fprintf(w, "⚠️  Regressed Dependencies / Slow Queries:\n")
		depTable := tablewriter.NewWriter(w)
		depTable.SetHeader([]string{"Type", "Target", "Operation / Query", "Base P95", "Post P95", "Δ%"})
		depTable.SetBorder(true)

		for _, dep := range report.RegressedDeps {
			depTable.Append([]string{
				dep.Type,
				dep.Target,
				dep.Name,
				fmt.Sprintf("%.1fms", dep.Baseline.Latency.P95),
				fmt.Sprintf("%.1fms", dep.Current.Latency.P95),
				fmt.Sprintf("%+.1f%%", dep.P95DeltaPct),
			})
		}
		depTable.Render()
		fmt.Fprintln(w)
	}

	// 5. N+1 SQL Regressions
	if len(report.FanoutDeltas) > 0 {
		colorRed.Fprintf(w, "🔍 N+1 SQL Regressions Detected Post-Deploy:\n")
		fanoutTable := tablewriter.NewWriter(w)
		fanoutTable.SetHeader([]string{"Endpoint", "Baseline SQL Calls/Req", "Post-Deploy SQL Calls/Req", "Spike Δ%"})
		fanoutTable.SetBorder(true)
		for _, f := range report.FanoutDeltas {
			fanoutTable.Append([]string{
				f.Endpoint,
				fmt.Sprintf("%.1f", f.BaselineCalls),
				fmt.Sprintf("%.1f", f.CurrentCalls),
				fmt.Sprintf("%+.1f%%", f.DeltaPct),
			})
		}
		fanoutTable.Render()
		fmt.Fprintln(w)
	}

	// 6. New Errors
	if len(report.NewErrors) > 0 {
		colorRed.Fprintf(w, "🚨 New Exceptions & Errors Detected Post-Deploy:\n")
		for i, err := range report.NewErrors {
			colorRed.Fprintf(w, " [%d] %s (Count: %d)\n", i+1, err.Type, err.Count)
			colorGray.Fprintf(w, "     Message: %s\n", err.Message)
			if len(err.AffectedPaths) > 0 {
				colorGray.Fprintf(w, "     Endpoints: %v\n", err.AffectedPaths)
			}
		}
		fmt.Fprintln(w)
	}

	// 6. Intelligent Root Cause Correlation Hints
	if len(report.RootCauseHints) > 0 {
		colorCyan.Fprintf(w, "💡 Intelligent Root-Cause & Correlation Insights:\n")
		for i, hint := range report.RootCauseHints {
			fmt.Fprintf(w, "  %d. %s\n", i+1, hint)
		}
		fmt.Fprintln(w)
	}
}

// PrintRequestsTable renders list of request metrics
func PrintRequestsTable(w io.Writer, requests []model.RequestMetric) {
	if w == nil {
		w = os.Stdout
	}
	if len(requests) == 0 {
		colorGreen.Fprintln(w, "✓ No slow endpoints recorded matching active scope in this time window.")
		return
	}
	table := tablewriter.NewWriter(w)
	table.SetHeader([]string{"Endpoint", "Calls", "Err%", "Avg", "P50", "P90", "P95", "P99"})
	table.SetBorder(true)

	for _, r := range requests {
		table.Append([]string{
			r.Name,
			fmt.Sprintf("%d", r.TotalCalls),
			fmt.Sprintf("%.2f%%", r.ErrorRate),
			fmt.Sprintf("%.1fms", r.Latency.Avg),
			fmt.Sprintf("%.1fms", r.Latency.P50),
			fmt.Sprintf("%.1fms", r.Latency.P90),
			fmt.Sprintf("%.1fms", r.Latency.P95),
			fmt.Sprintf("%.1fms", r.Latency.P99),
		})
	}
	table.Render()
}

// PrintDependenciesTable renders list of slow queries/dependencies
func PrintDependenciesTable(w io.Writer, deps []model.DependencyMetric) {
	if w == nil {
		w = os.Stdout
	}
	if len(deps) == 0 {
		colorGreen.Fprintln(w, "✓ No slow queries or dependencies recorded in this time window.")
		return
	}
	table := tablewriter.NewWriter(w)
	table.SetHeader([]string{"Type", "Target", "Query / Command", "Calls", "Err%", "Avg", "P95", "P99"})
	table.SetBorder(true)

	for _, d := range deps {
		table.Append([]string{
			d.Type,
			d.Target,
			truncate(d.Name, 60),
			fmt.Sprintf("%d", d.TotalCalls),
			fmt.Sprintf("%.2f%%", d.ErrorRate),
			fmt.Sprintf("%.1fms", d.Latency.Avg),
			fmt.Sprintf("%.1fms", d.Latency.P95),
			fmt.Sprintf("%.1fms", d.Latency.P99),
		})
	}
	table.Render()
}

// PrintErrorsTable renders list of error summaries
func PrintErrorsTable(w io.Writer, errors []model.ErrorSummary) {
	if w == nil {
		w = os.Stdout
	}
	if len(errors) == 0 {
		colorGreen.Fprintln(w, "🎉 No exceptions or HTTP 5xx errors detected in this time window!")
		return
	}
	for i, e := range errors {
		colorRed.Fprintf(w, "[%d] %s (Count: %d)\n", i+1, e.Type, e.Count)
		fmt.Fprintf(w, "    Message: %s\n", e.Message)
		if len(e.AffectedPaths) > 0 {
			colorGray.Fprintf(w, "    Endpoints: %v\n", e.AffectedPaths)
		}
		fmt.Fprintln(w)
	}
}

// PrintGenericTable renders arbitrary tabular query results
func PrintGenericTable(w io.Writer, res model.GenericQueryResult) {
	if w == nil {
		w = os.Stdout
	}
	if len(res.Columns) == 0 || len(res.Rows) == 0 {
		colorYellow.Fprintln(w, "No data returned for this query.")
		return
	}

	table := tablewriter.NewWriter(w)
	table.SetHeader(res.Columns)
	table.SetBorder(true)
	table.SetAutoWrapText(true)

	for _, row := range res.Rows {
		rowStrs := make([]string, len(row))
		for i, v := range row {
			rowStrs[i] = truncate(fmt.Sprintf("%v", v), 70)
		}
		table.Append(rowStrs)
	}
	table.Render()
}

func truncate(s string, maxLen int) string {
	s = strings.ReplaceAll(s, "\n", " ")
	s = strings.ReplaceAll(s, "\r", "")
	s = strings.TrimSpace(s)
	if len(s) <= maxLen {
		return s
	}
	return s[:maxLen-3] + "..."
}

// PrintFanoutTable renders N+1 and SQL fanout metrics
func PrintFanoutTable(w io.Writer, fanout []model.FanoutMetric) {
	if w == nil {
		w = os.Stdout
	}
	if len(fanout) == 0 {
		colorGreen.Fprintln(w, "✓ No N+1 queries detected matching active scope in this time window.")
		return
	}
	table := tablewriter.NewWriter(w)
	table.SetHeader([]string{"Endpoint", "Requests", "Avg SQL Calls", "Max SQL Calls", "Avg SQL Ms", "Avg Total Ms"})
	table.SetBorder(true)

	for _, f := range fanout {
		table.Append([]string{
			f.Endpoint,
			fmt.Sprintf("%d", f.TotalRequests),
			fmt.Sprintf("%.1f", f.AvgSQLCalls),
			fmt.Sprintf("%d", f.MaxSQLCalls),
			fmt.Sprintf("%.1fms", f.AvgSQLDurationMs),
			fmt.Sprintf("%.1fms", f.AvgEndpointDurationMs),
		})
	}
	table.Render()
}

// PrintLatencyBreakdownTable renders time breakdown across dependencies and app code
func PrintLatencyBreakdownTable(w io.Writer, attrs []model.LatencyBreakdown) {
	if w == nil {
		w = os.Stdout
	}
	if len(attrs) == 0 {
		colorYellow.Fprintln(w, "ℹ️  No request/dependency correlation data recorded in this time window.")
		return
	}
	table := tablewriter.NewWriter(w)
	table.SetHeader([]string{"Endpoint", "Avg Total", "% Database", "% Ext APIs", "% Cache", "% App Code"})
	table.SetBorder(true)

	for _, a := range attrs {
		table.Append([]string{
			a.Endpoint,
			fmt.Sprintf("%.1fms", a.AvgDurationMs),
			fmt.Sprintf("%.1f%%", a.PctDatabase),
			fmt.Sprintf("%.1f%%", a.PctExternalAPI),
			fmt.Sprintf("%.1f%%", a.PctCache),
			fmt.Sprintf("%.1f%%", a.PctAppCode),
		})
	}
	table.Render()
}

func formatStatus(sev model.RegressionSeverity) string {
	switch sev {
	case model.SeverityCritical:
		return "CRITICAL"
	case model.SeverityWarning:
		return "WARNING"
	case model.SeverityImprove:
		return "IMPROVED"
	default:
		return "OK"
	}
}

// PrintDeprecationsTable renders grouped framework and library deprecation warnings
func PrintDeprecationsTable(w io.Writer, deps []model.DeprecationSummary) {
	if w == nil {
		w = os.Stdout
	}
	if len(deps) == 0 {
		colorGreen.Fprintln(w, "🎉 No framework or library deprecations detected in this time window!")
		return
	}
	table := tablewriter.NewWriter(w)
	table.SetHeader([]string{"Deprecation Warning", "Count", "Last Seen", "Affected Endpoints"})
	table.SetBorder(true)
	table.SetAutoWrapText(true)

	for _, d := range deps {
		lastSeenStr := "-"
		if !d.LastSeen.IsZero() {
			lastSeenStr = d.LastSeen.Format("2006-01-02 15:04")
		}
		eps := "-"
		if len(d.AffectedEndpoints) > 0 {
			eps = truncate(strings.Join(d.AffectedEndpoints, ", "), 30)
		}
		table.Append([]string{
			truncate(d.Message, 75),
			fmt.Sprintf("%d", d.Count),
			lastSeenStr,
			eps,
		})
	}
	table.Render()
}

// PrintSlowLogsTable renders list of MySQL slow query logs
func PrintSlowLogsTable(w io.Writer, logs []model.SlowLogEntry) {
	if w == nil {
		w = os.Stdout
	}
	if len(logs) == 0 {
		colorGreen.Fprintln(w, "✓ No slow queries recorded in this time window.")
		return
	}
	table := tablewriter.NewWriter(w)
	table.SetHeader([]string{"Timestamp", "Duration", "Examined", "Sent", "SQL Query"})
	table.SetBorder(true)
	table.SetAutoWrapText(true)

	for _, l := range logs {
		tsStr := "-"
		if !l.Timestamp.IsZero() {
			tsStr = l.Timestamp.Local().Format("2006-01-02 15:04:05")
		}
		table.Append([]string{
			tsStr,
			formatDurationHuman(l.DurationSec, l.DurationMs),
			formatNumber(l.RowsExamined),
			formatNumber(l.RowsSent),
			truncate(l.SQLText, 80),
		})
	}
	table.Render()
}

func formatNumber(n int64) string {
	if n < 0 {
		return "-" + formatNumber(-n)
	}
	in := strconv.FormatInt(n, 10)
	if len(in) <= 3 {
		return in
	}
	var out []byte
	rem := len(in) % 3
	if rem > 0 {
		out = append(out, in[:rem]...)
		if len(in) > rem {
			out = append(out, ',')
		}
	}
	for i := rem; i < len(in); i += 3 {
		out = append(out, in[i:i+3]...)
		if i+3 < len(in) {
			out = append(out, ',')
		}
	}
	return string(out)
}

func formatDurationHuman(durSec, durMs float64) string {
	if durSec >= 1.0 {
		return fmt.Sprintf("%.2fs", durSec)
	}
	if durMs > 0 {
		return fmt.Sprintf("%.1fms", durMs)
	}
	if durSec > 0 {
		return fmt.Sprintf("%.1fms", durSec*1000.0)
	}
	return "0ms"
}
