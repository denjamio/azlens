package azure

import (
	"testing"
	"time"
)

func TestParseSlowLogsGroupTable(t *testing.T) {
	table := &AzQueryTable{
		Columns: []AzTableColumn{
			{Name: "SqlFingerprint", Type: "string"},
			{Name: "Executions", Type: "long"},
			{Name: "AvgMs", Type: "real"},
			{Name: "MaxMs", Type: "real"},
			{Name: "TotalMs", Type: "real"},
			{Name: "AvgRowsExamined", Type: "real"},
			{Name: "LastSeen", Type: "datetime"},
		},
		Rows: [][]interface{}{
			{"SELECT * FROM orders WHERE status = '?'", float64(47), float64(3210.4), float64(9840.2), float64(150889.0), float64(1184210), "2026-09-02T20:15:22Z"},
			{nil, float64(3), float64(120.0), float64(200.0), float64(360.0), nil, nil},
		},
	}

	groups := parseSlowLogsGroupTable(table)
	if len(groups) != 2 {
		t.Fatalf("expected 2 groups, got %d", len(groups))
	}

	first := groups[0]
	if first.Fingerprint != "SELECT * FROM orders WHERE status = '?'" {
		t.Errorf("unexpected fingerprint: %q", first.Fingerprint)
	}
	if first.Executions != 47 {
		t.Errorf("expected 47 executions, got %d", first.Executions)
	}
	if first.AvgMs != 3210.4 || first.MaxMs != 9840.2 || first.TotalMs != 150889.0 {
		t.Errorf("unexpected duration stats: avg=%v max=%v total=%v", first.AvgMs, first.MaxMs, first.TotalMs)
	}
	if first.AvgRowsExamined != 1184210 {
		t.Errorf("unexpected avg rows examined: %v", first.AvgRowsExamined)
	}
	wantLastSeen, _ := time.Parse(time.RFC3339, "2026-09-02T20:15:22Z")
	if !first.LastSeen.Equal(wantLastSeen) {
		t.Errorf("unexpected last seen: %v, want %v", first.LastSeen, wantLastSeen)
	}

	// NULL cells must not zero out silently tracked fields nor panic
	second := groups[1]
	if second.Executions != 3 || second.AvgMs != 120.0 {
		t.Errorf("unexpected second group: %+v", second)
	}
	if !second.LastSeen.IsZero() {
		t.Errorf("expected zero timestamp for NULL LastSeen, got %v", second.LastSeen)
	}
}

func TestParseSlowLogsGroupTableEmpty(t *testing.T) {
	table := &AzQueryTable{Columns: []AzTableColumn{{Name: "SqlFingerprint", Type: "string"}}}
	if groups := parseSlowLogsGroupTable(table); len(groups) != 0 {
		t.Errorf("expected no groups for empty table, got %d", len(groups))
	}
	if groups := parseSlowLogsGroupTable(&AzQueryTable{}); len(groups) != 0 {
		t.Errorf("expected no groups for table without rows, got %d", len(groups))
	}
}
