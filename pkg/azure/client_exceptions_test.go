package azure

import (
	"testing"
)

func TestParseExceptionsTableParsesAffectedPaths(t *testing.T) {
	table := &AzQueryTable{
		Columns: []AzTableColumn{
			{Name: "type", Type: "string"},
			{Name: "CleanMessage", Type: "string"},
			{Name: "Count", Type: "long"},
			{Name: "FirstSeen", Type: "datetime"},
			{Name: "LastSeen", Type: "datetime"},
			{Name: "AffectedPaths", Type: "dynamic"},
		},
		Rows: [][]interface{}{
			{"NpgsqlException", "connection refused", float64(52), "2026-09-04T14:00:00Z", "2026-09-04T15:00:00Z",
				[]interface{}{"GET /api/v1/catalog/search", "GET /api/v1/catalog/item"}},
			{"TimeoutException", "operation timed out", float64(3), nil, nil, nil},
		},
	}

	errs := parseExceptionsTable(table)
	if len(errs) != 2 {
		t.Fatalf("expected 2 error summaries, got %d", len(errs))
	}

	first := errs[0]
	if len(first.AffectedPaths) != 2 || first.AffectedPaths[0] != "GET /api/v1/catalog/search" {
		t.Errorf("expected affected paths parsed from dynamic column, got: %v", first.AffectedPaths)
	}

	// NULL dynamic cell must degrade to an empty path list, not a parse failure
	if second := errs[1]; len(second.AffectedPaths) != 0 {
		t.Errorf("expected no affected paths for NULL cell, got: %v", second.AffectedPaths)
	}
}
