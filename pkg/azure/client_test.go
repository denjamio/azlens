package azure

import (
	"context"
	"testing"
	"time"

	"github.com/denjamio/azlens/pkg/config"
)

func TestMockClientOperations(t *testing.T) {
	ctx := context.Background()
	prof := config.Profile{
		Name: "Test Profile",
	}

	client := NewClient(ClientOptions{
		Profile: prof,
		IsMock:  true,
	})

	now := time.Now()
	start := now.Add(-1 * time.Hour)
	end := now

	// 1. Window Metrics
	wm, err := client.QueryWindowMetrics(ctx, start, end, 10)
	if err != nil {
		t.Fatalf("failed querying window metrics: %v", err)
	}
	if wm.Overall.TotalCalls == 0 {
		t.Errorf("expected non-zero total calls")
	}

	// 2. Endpoints
	endpoints, err := client.QueryEndpoints(ctx, start, end, 10)
	if err != nil {
		t.Fatalf("failed querying endpoints: %v", err)
	}
	if len(endpoints) == 0 {
		t.Errorf("expected endpoints, got none")
	}

	// 3. Dependencies
	deps, err := client.QuerySlowDependencies(ctx, start, end, "SQL", 10)
	if err != nil {
		t.Fatalf("failed querying dependencies: %v", err)
	}
	if len(deps) == 0 {
		t.Errorf("expected dependencies, got none")
	}

	// 4. Exceptions
	errs, err := client.QueryExceptions(ctx, start, end, 10)
	if err != nil {
		t.Fatalf("failed querying exceptions: %v", err)
	}
	if len(errs) == 0 {
		t.Errorf("expected exceptions, got none")
	}

	// 5. MySQL Slow Logs
	mysqlLogs, err := client.QueryMySQLSlowLogs(ctx, start, end, "backend_ror", 10)
	if err != nil {
		t.Fatalf("failed querying mysql slow logs: %v", err)
	}
	if len(mysqlLogs.Columns) == 0 || len(mysqlLogs.Rows) == 0 {
		t.Errorf("expected mysql slow logs rows and columns")
	}
}

func TestParseAzQueryOutput(t *testing.T) {
	tests := []struct {
		name        string
		input       string
		wantErr     bool
		checkResult func(t *testing.T, res *AzQueryResult)
	}{
		{
			name:    "empty input",
			input:   "  \n\t  ",
			wantErr: false,
			checkResult: func(t *testing.T, res *AzQueryResult) {
				if len(res.Tables) != 0 {
					t.Fatalf("expected 0 tables, got %d", len(res.Tables))
				}
			},
		},
		{
			name: "standard tables object",
			input: `{
				"tables": [
					{
						"name": "PrimaryResult",
						"columns": [{"name": "col1", "type": "string"}, {"name": "col2", "type": "int"}],
						"rows": [["test", 42]]
					}
				]
			}`,
			wantErr: false,
			checkResult: func(t *testing.T, res *AzQueryResult) {
				if len(res.Tables) != 1 {
					t.Fatalf("expected 1 table, got %d", len(res.Tables))
				}
				if res.Tables[0].Name != "PrimaryResult" {
					t.Errorf("expected PrimaryResult, got %s", res.Tables[0].Name)
				}
				if len(res.Tables[0].Columns) != 2 || len(res.Tables[0].Rows) != 1 {
					t.Fatalf("unexpected columns or rows: %+v", res.Tables[0])
				}
				if res.Tables[0].Rows[0][0] != "test" {
					t.Errorf("expected 'test', got %v", res.Tables[0].Rows[0][0])
				}
			},
		},
		{
			name: "direct array of tables",
			input: `[
				{
					"name": "PrimaryResult",
					"columns": [{"name": "OperationName", "type": "string"}],
					"rows": [["GET /health"]]
				}
			]`,
			wantErr: false,
			checkResult: func(t *testing.T, res *AzQueryResult) {
				if len(res.Tables) != 1 {
					t.Fatalf("expected 1 table, got %d", len(res.Tables))
				}
				if res.Tables[0].Rows[0][0] != "GET /health" {
					t.Errorf("expected GET /health, got %v", res.Tables[0].Rows[0][0])
				}
			},
		},
		{
			name:    "empty array",
			input:   "[]",
			wantErr: false,
			checkResult: func(t *testing.T, res *AzQueryResult) {
				if len(res.Tables) != 0 {
					t.Fatalf("expected 0 tables, got %d", len(res.Tables))
				}
			},
		},
		{
			name: "array of key-value maps",
			input: `[
				{"name": "service-a", "count": 100},
				{"name": "service-b", "count": 200, "extra": true}
			]`,
			wantErr: false,
			checkResult: func(t *testing.T, res *AzQueryResult) {
				if len(res.Tables) != 1 {
					t.Fatalf("expected 1 table, got %d", len(res.Tables))
				}
				tbl := res.Tables[0]
				if tbl.Name != "PrimaryResult" {
					t.Errorf("expected PrimaryResult, got %s", tbl.Name)
				}
				// Columns are sorted: count, extra, name
				if len(tbl.Columns) != 3 {
					t.Fatalf("expected 3 columns, got %d: %+v", len(tbl.Columns), tbl.Columns)
				}
				if tbl.Columns[0].Name != "count" || tbl.Columns[1].Name != "extra" || tbl.Columns[2].Name != "name" {
					t.Errorf("columns order mismatch: %+v", tbl.Columns)
				}
				if len(tbl.Rows) != 2 {
					t.Fatalf("expected 2 rows, got %d", len(tbl.Rows))
				}
				// Row 1: count=100, extra=nil, name=service-a
				if tbl.Rows[0][1] != nil {
					t.Errorf("expected nil for missing extra in row 1, got %v", tbl.Rows[0][1])
				}
			},
		},
		{
			name:    "invalid json syntax",
			input:   "{invalid: json}",
			wantErr: true,
			checkResult: func(t *testing.T, res *AzQueryResult) {
				if res != nil {
					t.Errorf("expected nil result on error")
				}
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			res, err := parseAzQueryOutput([]byte(tt.input))
			if (err != nil) != tt.wantErr {
				t.Fatalf("parseAzQueryOutput() error = %v, wantErr %v", err, tt.wantErr)
			}
			if tt.checkResult != nil {
				tt.checkResult(t, res)
			}
		})
	}
}
