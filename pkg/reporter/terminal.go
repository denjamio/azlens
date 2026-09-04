package reporter

import (
	"fmt"
	"io"
	"os"
	"strconv"
	"strings"

	"github.com/fatih/color"

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
	table := NewTable(w, []string{"Metric", "Baseline", "Post-Deploy", "Delta", "Status"},
		[]int{AlignLeft, AlignRight, AlignRight, AlignRight, AlignLeft})
	table.SetCellColor(func(row, col int, cell string) *color.Color {
		if row >= len(report.SummaryDeltas) {
			return nil
		}
		if col == 3 || col == 4 {
			return severityColor(report.SummaryDeltas[row].Severity)
		}
		return nil
	})

	for _, d := range report.SummaryDeltas {
		table.Append([]string{
			d.MetricName,
			formatMetricAmount(d.Baseline, d.Unit),
			formatMetricAmount(d.Current, d.Unit),
			formatMetricDelta(d),
			formatStatus(d.Severity),
		})
	}
	table.Render()
	fmt.Fprintln(w)

	// 3. Endpoint Breakdown
	if len(report.EndpointDeltas) > 0 {
		colorCyan.Fprintf(w, "📌 Per-Endpoint Latency & Error Rate Diff:\n")
		epTable := NewTable(w, []string{"Endpoint", "Base P95", "Post P95", "P95 Δ%", "Base Err%", "Post Err%", "Status"},
			[]int{AlignLeft, AlignRight, AlignRight, AlignRight, AlignRight, AlignRight, AlignLeft})
		epTable.SetCellColor(func(row, col int, cell string) *color.Color {
			if row >= len(report.EndpointDeltas) {
				return nil
			}
			ep := report.EndpointDeltas[row]
			switch col {
			case 1:
				return apiLatencyColor(ep.Baseline.Latency.P95)
			case 2:
				return apiLatencyColor(ep.Current.Latency.P95)
			case 3:
				return deltaPctColor(ep.P95DeltaPct)
			case 4:
				return errorRateColor(ep.Baseline.ErrorRate)
			case 5:
				return errorRateColor(ep.Current.ErrorRate)
			case 6:
				return severityColor(ep.Severity)
			}
			return nil
		})

		for _, ep := range report.EndpointDeltas {
			epTable.Append([]string{
				ep.Name,
				formatLatencyHuman(ep.Baseline.Latency.P95),
				formatLatencyHuman(ep.Current.Latency.P95),
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
		depTable := NewTable(w, []string{"Type", "Target", "Operation / Query", "Base P95", "Post P95", "Δ%"},
			[]int{AlignLeft, AlignLeft, AlignLeft, AlignRight, AlignRight, AlignRight})
		depTable.SetCellColor(func(row, col int, cell string) *color.Color {
			if row >= len(report.RegressedDeps) {
				return nil
			}
			if col == 5 {
				return deltaPctColor(report.RegressedDeps[row].P95DeltaPct)
			}
			return nil
		})

		for _, dep := range report.RegressedDeps {
			depTable.Append([]string{
				dep.Type,
				truncate(dep.Target, 24),
				truncate(dep.Name, 40),
				formatLatencyHuman(dep.Baseline.Latency.P95),
				formatLatencyHuman(dep.Current.Latency.P95),
				fmt.Sprintf("%+.1f%%", dep.P95DeltaPct),
			})
		}
		depTable.Render()
		fmt.Fprintln(w)
	}

	// 5. N+1 SQL Regressions
	if len(report.FanoutDeltas) > 0 {
		colorRed.Fprintf(w, "🔍 N+1 SQL Regressions Detected Post-Deploy:\n")
		fanoutTable := NewTable(w, []string{"Endpoint", "Baseline SQL Calls/Req", "Post-Deploy SQL Calls/Req", "Spike Δ%"},
			[]int{AlignLeft, AlignRight, AlignRight, AlignRight})
		fanoutTable.SetCellColor(func(row, col int, cell string) *color.Color {
			if row >= len(report.FanoutDeltas) {
				return nil
			}
			if col == 3 {
				return bandColor(report.FanoutDeltas[row].DeltaPct, FanoutSpikeWarnPct, FanoutSpikeCritPct)
			}
			return nil
		})
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
			colorRed.Fprintf(w, " [%d] %s (Count: %s)\n", i+1, err.Type, formatNumber(err.Count))
			colorGray.Fprintf(w, "     Message: %s\n", truncate(err.Message, 120))
			if len(err.AffectedPaths) > 0 {
				colorGray.Fprintf(w, "     Endpoints: %s\n", strings.Join(err.AffectedPaths, ", "))
			}
			if target, ok := err.InstrumentationTarget(); ok {
				colorCyan.Fprintf(w, "     ℹ️  %s\n", instrumentationNoiseHint(target))
			}
		}
		fmt.Fprintln(w)
	}

	// 7. Intelligent Root Cause Correlation Hints
	if len(report.RootCauseHints) > 0 {
		colorCyan.Fprintf(w, "💡 Intelligent Root-Cause & Correlation Insights:\n")
		for i, hint := range report.RootCauseHints {
			fmt.Fprintf(w, "  %d. %s\n", i+1, hint)
		}
		fmt.Fprintln(w)
	}
}

// formatMetricAmount renders a summary metric value in its natural unit:
// latencies humanized ("180ms"), request counts separated ("45,200 reqs")
func formatMetricAmount(v float64, unit string) string {
	switch unit {
	case "ms":
		return formatLatencyHuman(v)
	case "reqs":
		return formatNumber(int64(v)) + " reqs"
	case "%":
		return fmt.Sprintf("%.2f%%", v)
	default:
		return fmt.Sprintf("%.2f%s", v, unit)
	}
}

// formatMetricDelta renders a summary delta with signed human units: "+12ms (+6.7%)"
func formatMetricDelta(d model.MetricDelta) string {
	sign := "+"
	abs := d.Delta
	if d.Delta < 0 {
		sign = "-"
		abs = -d.Delta
	}
	var amount string
	switch d.Unit {
	case "ms":
		amount = sign + formatLatencyHuman(abs)
	case "reqs":
		amount = sign + formatNumber(int64(abs)) + " reqs"
	default:
		amount = fmt.Sprintf("%s%.2f%s", sign, abs, d.Unit)
	}
	return fmt.Sprintf("%s (%+.1f%%)", amount, d.Percentage)
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
	table := NewTable(w, []string{"Endpoint", "Calls", "Err%", "Avg", "P50", "P90", "P95", "P99"},
		[]int{AlignLeft, AlignRight, AlignRight, AlignRight, AlignRight, AlignRight, AlignRight, AlignRight})
	table.SetCellColor(func(row, col int, cell string) *color.Color {
		if row >= len(requests) {
			return nil
		}
		r := requests[row]
		switch col {
		case 2:
			return errorRateColor(r.ErrorRate)
		case 3:
			return apiLatencyColor(r.Latency.Avg)
		case 4:
			return apiLatencyColor(r.Latency.P50)
		case 5:
			return apiLatencyColor(r.Latency.P90)
		case 6:
			return apiLatencyColor(r.Latency.P95)
		case 7:
			return apiLatencyColor(r.Latency.P99)
		}
		return nil
	})

	for _, r := range requests {
		table.Append([]string{
			truncate(r.Name, 45),
			formatNumber(r.TotalCalls),
			fmt.Sprintf("%.2f%%", r.ErrorRate),
			formatLatencyHuman(r.Latency.Avg),
			formatLatencyHuman(r.Latency.P50),
			formatLatencyHuman(r.Latency.P90),
			formatLatencyHuman(r.Latency.P95),
			formatLatencyHuman(r.Latency.P99),
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
	table := NewTable(w, []string{"Type", "Target", "Query / Command", "Calls", "Err%", "Avg", "P95", "P99"},
		[]int{AlignLeft, AlignLeft, AlignLeft, AlignRight, AlignRight, AlignRight, AlignRight, AlignRight})
	table.SetCellColor(func(row, col int, cell string) *color.Color {
		if row >= len(deps) {
			return nil
		}
		d := deps[row]
		switch col {
		case 4:
			return errorRateColor(d.ErrorRate)
		case 5:
			return latencyColorForDepType(d.Type, d.Latency.Avg)
		case 6:
			return latencyColorForDepType(d.Type, d.Latency.P95)
		case 7:
			return latencyColorForDepType(d.Type, d.Latency.P99)
		}
		return nil
	})

	for _, d := range deps {
		table.Append([]string{
			d.Type,
			truncate(d.Target, 20),
			truncate(d.Name, 50),
			formatNumber(d.TotalCalls),
			fmt.Sprintf("%.2f%%", d.ErrorRate),
			formatLatencyHuman(d.Latency.Avg),
			formatLatencyHuman(d.Latency.P95),
			formatLatencyHuman(d.Latency.P99),
		})
	}
	table.Render()
}

// PrintErrorsTable renders list of error summaries, annotating auto-instrumentation noise
func PrintErrorsTable(w io.Writer, errors []model.ErrorSummary) {
	if w == nil {
		w = os.Stdout
	}
	if len(errors) == 0 {
		colorGreen.Fprintln(w, "🎉 No exceptions or HTTP 5xx errors detected in this time window!")
		return
	}
	for i, e := range errors {
		colorRed.Fprintf(w, "[%d] %s (Count: %s)\n", i+1, e.Type, formatNumber(e.Count))
		fmt.Fprintf(w, "    Message:   %s\n", truncate(e.Message, 120))
		if len(e.AffectedPaths) > 0 {
			colorGray.Fprintf(w, "    Endpoints: %s\n", strings.Join(e.AffectedPaths, ", "))
		}
		if !e.LastSeen.IsZero() {
			colorGray.Fprintf(w, "    Last Seen: %s\n", e.LastSeen.Local().Format("2006-01-02 15:04:05"))
		}
		if target, ok := e.InstrumentationTarget(); ok {
			colorCyan.Fprintf(w, "    ℹ️  %s\n", instrumentationNoiseHint(target))
		}
		fmt.Fprintln(w)
	}
}

// PrintGenericTable renders arbitrary tabular query results with humanized
// headers and normalized cell values (local timestamps, separated numbers,
// human durations, joined arrays)
func PrintGenericTable(w io.Writer, res model.GenericQueryResult) {
	if w == nil {
		w = os.Stdout
	}
	if len(res.Columns) == 0 || len(res.Rows) == 0 {
		colorYellow.Fprintln(w, "No data returned for this query.")
		return
	}

	headers := make([]string, len(res.Columns))
	for i, c := range res.Columns {
		headers[i] = humanizeHeader(c)
	}
	table := NewTable(w, headers, nil).SetCellCap(100)

	for _, row := range res.Rows {
		cells := make([]string, len(headers))
		for i := range headers {
			var v interface{}
			if i < len(row) {
				v = row[i]
			}
			cells[i] = normalizeCell(res.Columns[i], v)
		}
		table.Append(cells)
	}
	table.Render()
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
	table := NewTable(w, []string{"Endpoint", "Requests", "Avg SQL Calls", "Max SQL Calls", "Avg SQL", "Avg Total"},
		[]int{AlignLeft, AlignRight, AlignRight, AlignRight, AlignRight, AlignRight})
	table.SetCellColor(func(row, col int, cell string) *color.Color {
		if row >= len(fanout) {
			return nil
		}
		f := fanout[row]
		switch col {
		case 2:
			return nPlusOneColor(f.AvgSQLCalls)
		case 3:
			return nPlusOneColor(float64(f.MaxSQLCalls))
		}
		return nil
	})

	for _, f := range fanout {
		table.Append([]string{
			truncate(f.Endpoint, 45),
			formatNumber(f.TotalRequests),
			fmt.Sprintf("%.1f", f.AvgSQLCalls),
			formatNumber(f.MaxSQLCalls),
			formatLatencyHuman(f.AvgSQLDurationMs),
			formatLatencyHuman(f.AvgEndpointDurationMs),
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
	table := NewTable(w, []string{"Endpoint", "Avg Total", "% Database", "% Ext APIs", "% Cache", "% App Code"},
		[]int{AlignLeft, AlignRight, AlignRight, AlignRight, AlignRight, AlignRight})

	for _, a := range attrs {
		table.Append([]string{
			truncate(a.Endpoint, 45),
			formatLatencyHuman(a.AvgDurationMs),
			fmt.Sprintf("%.1f%%", a.PctDatabase),
			fmt.Sprintf("%.1f%%", a.PctExternalAPI),
			fmt.Sprintf("%.1f%%", a.PctCache),
			fmt.Sprintf("%.1f%%", a.PctAppCode),
		})
	}
	table.Render()
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
	table := NewTable(w, []string{"Deprecation Warning", "Count", "Last Seen", "Affected Endpoints"},
		[]int{AlignLeft, AlignRight, AlignLeft, AlignLeft}).SetCellCap(75)

	for _, d := range deps {
		lastSeenStr := "-"
		if !d.LastSeen.IsZero() {
			lastSeenStr = d.LastSeen.Local().Format("2006-01-02 15:04")
		}
		eps := "-"
		if len(d.AffectedEndpoints) > 0 {
			eps = truncate(strings.Join(d.AffectedEndpoints, ", "), 30)
		}
		table.Append([]string{
			d.Message,
			formatNumber(d.Count),
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
	table := NewTable(w, []string{"Timestamp", "Duration", "Rows Examined", "Rows Returned", "SQL Query"},
		[]int{AlignLeft, AlignRight, AlignRight, AlignRight, AlignLeft}).SetCellCap(80)
	table.SetCellColor(func(row, col int, cell string) *color.Color {
		if row >= len(logs) {
			return nil
		}
		l := logs[row]
		switch col {
		case 1:
			ms := l.DurationMs
			if ms <= 0 {
				ms = l.DurationSec * 1000.0
			}
			return dbLatencyColor(ms)
		case 2:
			// Missing-index signal: rows scanned per row returned
			// (EXPLAIN / Percona heuristic), not the absolute count
			return scanRatioColor(l.RowsExamined, l.RowsSent)
		}
		return nil
	})

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
			l.SQLText,
		})
	}
	table.Render()
}

// instrumentationNoiseHint explains errors emitted by the auto-instrumentation
// SDK itself so they are not mistaken for application exceptions
func instrumentationNoiseHint(target string) string {
	if target == "" {
		target = "the target framework"
	}
	return fmt.Sprintf("Instrumentation noise: raised by the auto-instrumentation SDK itself while hooking '%s' — "+
		"the module failed to import in the runtime image (verify the instrumentation package is installed and importable), "+
		"not an error from your API code", target)
}

// PrintSlowLogsGroupTable renders slow query logs aggregated by normalized SQL
// fingerprint: execution count, duration statistics, and rows examined,
// ordered by total accumulated duration (highest overall impact first)
func PrintSlowLogsGroupTable(w io.Writer, groups []model.SlowLogGroup) {
	if w == nil {
		w = os.Stdout
	}
	if len(groups) == 0 {
		colorGreen.Fprintln(w, "✓ No slow queries recorded in this time window.")
		return
	}
	table := NewTable(w, []string{"SQL Fingerprint", "Executions", "Avg", "Max", "Total Time", "Rows Examined (avg)", "Last Seen"},
		[]int{AlignLeft, AlignRight, AlignRight, AlignRight, AlignRight, AlignRight, AlignLeft}).SetCellCap(80)
	// Only industry-anchored bands are colored here (DB statement latency).
	// Execution counts and totals are workload-dependent: coloring them would
	// signal severity without a standard behind it.
	table.SetCellColor(func(row, col int, cell string) *color.Color {
		if row >= len(groups) {
			return nil
		}
		g := groups[row]
		switch col {
		case 2:
			return dbLatencyColor(g.AvgMs)
		case 3:
			return dbLatencyColor(g.MaxMs)
		}
		return nil
	})

	for _, g := range groups {
		lastSeen := "-"
		if !g.LastSeen.IsZero() {
			lastSeen = g.LastSeen.Local().Format("2006-01-02 15:04:05")
		}
		table.Append([]string{
			g.Fingerprint,
			formatNumber(g.Executions),
			formatLatencyHuman(g.AvgMs),
			formatLatencyHuman(g.MaxMs),
			formatLatencyHuman(g.TotalMs),
			formatNumber(int64(g.AvgRowsExamined)),
			lastSeen,
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
