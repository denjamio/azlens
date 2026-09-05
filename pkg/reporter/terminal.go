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
