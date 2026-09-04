package reporter

import (
	"bytes"
	"strings"
	"testing"
	"time"

	"github.com/denjamio/azlens/pkg/model"
)

func TestHumanizeHeader(t *testing.T) {
	cases := map[string]string{
		"TotalCalls":      "Total Calls",
		"TimeGenerated":   "Time",
		"timestamp":       "Time",
		"QueryDurationMs": "Query Duration (ms)",
		"operation_Name":  "Operation",
		"SqlText":         "SQL Text",
		"RowsExamined":    "Rows Examined",
		"cloud_RoleName":  "Role",
		"P95":             "P95",
		"ErrorRate":       "Error Rate",
		"success":         "Success",
		"id":              "ID",
		"URL":             "URL",
	}
	for input, want := range cases {
		if got := humanizeHeader(input); got != want {
			t.Errorf("humanizeHeader(%q) = %q, want %q", input, got, want)
		}
	}
}

func TestNormalizeCell(t *testing.T) {
	cases := []struct {
		name   string
		header string
		value  interface{}
		want   string
	}{
		{"nil renders dash", "Count", nil, "-"},
		{"integer separated", "Count", float64(1234567), "1,234,567"},
		{"count header separated", "TotalCalls", float64(812), "812"},
		{"duration header humanized", "AvgDuration", float64(950), "950ms"},
		{"ms header humanized", "QueryDurationMs", float64(14520), "14.52s"},
		{"seconds header humanized", "Duration_s", float64(0.85), "850ms"},
		{"rate header percent", "ErrorRate", float64(0.53), "0.53%"},
		{"pct header percent", "PctDatabase", float64(42.0), "42%"},
		{"bool rendered", "success", true, "true"},
		{"array joined", "AffectedPaths", []interface{}{"GET /a", "GET /b"}, "GET /a, GET /b"},
		{"empty array dash", "AffectedPaths", []interface{}{}, "-"},
		{"float trimmed", "Value", float64(2.50), "2.5"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if got := normalizeCell(tc.header, tc.value); got != tc.want {
				t.Errorf("normalizeCell(%q, %v) = %q, want %q", tc.header, tc.value, got, tc.want)
			}
		})
	}
}

func TestNormalizeCellTimestamp(t *testing.T) {
	want := parseTimeForTest(t, "2026-09-04T10:15:30Z")
	if got := normalizeCell("TimeGenerated", "2026-09-04T10:15:30Z"); got != want {
		t.Errorf("normalizeCell timestamp = %q, want %q", got, want)
	}
}

func TestFormatLatencyHuman(t *testing.T) {
	cases := map[float64]string{
		0.5:     "0.50ms",
		812:     "812ms",
		14520:   "14.52s",
		125000:  "2m05s",
		3720000: "1h02m",
		0:       "0ms",
	}
	for in, want := range cases {
		if got := formatLatencyHuman(in); got != want {
			t.Errorf("formatLatencyHuman(%v) = %q, want %q", in, got, want)
		}
	}
}

func TestModernTableWidthBudget(t *testing.T) {
	forceTerminalWidth = 44
	defer func() { forceTerminalWidth = 0 }()

	var buf bytes.Buffer
	table := NewTable(&buf, []string{"Endpoint", "Calls", "SQL Query"},
		[]int{AlignLeft, AlignRight, AlignLeft})
	for i := 0; i < 5; i++ {
		table.Append([]string{
			"GET /api/v1/orders/checkout/very/long/path/segment",
			formatNumber(12345),
			"SELECT * FROM orders JOIN customers ON orders.customer_id = customers.id WHERE orders.status = 'pending'",
		})
	}
	table.Render()

	for _, line := range strings.Split(strings.TrimRight(buf.String(), "\n"), "\n") {
		if w := displayWidth(line); w > 44 {
			t.Errorf("line exceeds terminal budget (width %d): %s", w, line)
		}
	}
	if !strings.Contains(buf.String(), "…") {
		t.Errorf("expected long cells to be truncated with an ellipsis, got:\n%s", buf.String())
	}
}

func TestModernTableUnlimitedWhenNotATerminal(t *testing.T) {
	// forceTerminalWidth stays 0 and COLUMNS is unset in test envs: no truncation
	var buf bytes.Buffer
	table := NewTable(&buf, []string{"Query"}, nil)
	table.Append([]string{strings.Repeat("SELECT ", 30)})
	table.Render()
	if !strings.Contains(buf.String(), strings.Repeat("SELECT ", 30)) {
		t.Errorf("expected full content when rendering without a terminal budget, got:\n%s", buf.String())
	}
}

func TestModernTableAutoNumericAlignment(t *testing.T) {
	var buf bytes.Buffer
	table := NewTable(&buf, []string{"Name", "Value"}, nil)
	table.Append([]string{"alpha", "1,234"})
	table.Append([]string{"beta", "567"})
	table.Render()

	out := buf.String()
	if !strings.Contains(out, " 567 │") {
		t.Errorf("expected numeric column right-aligned, got:\n%s", out)
	}
	if !strings.Contains(out, "alpha") {
		t.Errorf("expected text column content, got:\n%s", out)
	}
}

func TestSlowLogsTableHeadersClarified(t *testing.T) {
	logs := []model.SlowLogEntry{{
		DurationSec:  14.52,
		RowsExamined: 1250000,
		RowsSent:     10,
		SQLText:      "SELECT 1",
	}}
	var buf bytes.Buffer
	PrintSlowLogsTable(&buf, logs)
	out := buf.String()
	if !strings.Contains(out, "Rows Examined") || !strings.Contains(out, "Rows Returned") {
		t.Errorf("expected clarified headers, got:\n%s", out)
	}
	if strings.Contains(out, "Rows Sent") {
		t.Errorf("did not expect legacy 'Rows Sent' header, got:\n%s", out)
	}
}

func TestErrorsTableInstrumentationNoiseHint(t *testing.T) {
	errs := []model.ErrorSummary{{
		Type:    "ModuleNotFoundError",
		Message: "Exception occurred when instrumenting: fastapi.",
		Count:   24,
	}}
	var buf bytes.Buffer
	PrintErrorsTable(&buf, errs)
	out := buf.String()
	if !strings.Contains(out, "Instrumentation noise") || !strings.Contains(out, "'fastapi'") {
		t.Errorf("expected instrumentation noise hint in table output, got:\n%s", out)
	}

	var mdBuf bytes.Buffer
	PrintErrorsMarkdown(&mdBuf, errs)
	if !strings.Contains(mdBuf.String(), "Instrumentation noise") {
		t.Errorf("expected instrumentation noise note in markdown output, got:\n%s", mdBuf.String())
	}
}

func TestSlowLogsGroupReporters(t *testing.T) {
	ts, _ := time.Parse(time.RFC3339, "2026-09-04T10:15:30Z")
	groups := []model.SlowLogGroup{
		{
			Fingerprint:     "select * from orders o join order_items i on o.id = i.order_id where o.status = '?' for update",
			Executions:      47,
			AvgMs:           3210.4,
			MaxMs:           9840.2,
			TotalMs:         150889.0,
			AvgRowsExamined: 1184210,
			LastSeen:        ts,
		},
	}

	var tableBuf bytes.Buffer
	PrintSlowLogsGroupTable(&tableBuf, groups)
	outTable := tableBuf.String()
	for _, want := range []string{"SQL Fingerprint", "Executions", "Rows Examined (avg)", "47", "3.21s", "9.84s", "2m30s", "1,184,210", "select * from orders", "o.status ="} {
		if !strings.Contains(outTable, want) {
			t.Errorf("expected %q in grouped table output, got:\n%s", want, outTable)
		}
	}

	var mdBuf bytes.Buffer
	PrintSlowLogsGroupMarkdown(&mdBuf, groups)
	outMd := mdBuf.String()
	for _, want := range []string{"Grouped by SQL Fingerprint", "**47**", "3.21s", "2m30s", "for update"} {
		if !strings.Contains(outMd, want) {
			t.Errorf("expected %q in grouped markdown output, got:\n%s", want, outMd)
		}
	}
}

func TestInstrumentationTargetClassification(t *testing.T) {
	noise := model.ErrorSummary{Message: "Exception occurred when instrumenting: fastapi."}
	if target, ok := noise.InstrumentationTarget(); !ok || target != "fastapi" {
		t.Errorf("expected target 'fastapi', got %q (ok=%v)", target, ok)
	}

	appError := model.ErrorSummary{Type: "ValueError", Message: "invalid quantity: -1"}
	if _, ok := appError.InstrumentationTarget(); ok {
		t.Errorf("expected plain application error not to be classified as instrumentation noise")
	}
}
