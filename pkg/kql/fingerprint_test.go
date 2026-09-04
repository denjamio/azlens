package kql

import (
	"strconv"
	"strings"
	"testing"
	"time"
)

func TestSQLFingerprintGroupsVariantsTogether(t *testing.T) {
	groups := [][]string{
		{
			"SELECT * FROM users WHERE email = 'alice@example.com'",
			"select * from USERS where email = 'bob@test.io'",
			"SELECT  *\tFROM users\n WHERE email = 'carol@x.co'",
		},
		{
			"SELECT * FROM orders WHERE id IN (1, 2, 3)",
			"select * from orders where id in (42)",
			"SELECT * FROM orders WHERE id IN (7,8,9,10)",
		},
		{
			"/* trace_id='abc-123' span=7 */ SELECT * FROM audit_logs WHERE created_at < NOW() - INTERVAL 90 DAY",
			"SELECT * FROM audit_logs WHERE created_at < NOW() - INTERVAL 30 DAY",
		},
		{
			"UPDATE inventory SET reserved_qty = reserved_qty + 1 WHERE sku = 'PROD-9981'",
			"update inventory set reserved_qty = reserved_qty + 2 where sku = 'PROD-1234'",
		},
		{
			"SELECT * FROM payment_methods WHERE user_id = 0x1F",
			"SELECT * FROM payment_methods WHERE user_id = 31",
			"SELECT * FROM payment_methods WHERE user_id = -7",
		},
	}

	for _, group := range groups {
		base := SQLFingerprint(group[0])
		if base == "" {
			t.Fatalf("fingerprint must not be empty for %q", group[0])
		}
		for _, variant := range group[1:] {
			if got := SQLFingerprint(variant); got != base {
				t.Errorf("variants must share one fingerprint:\n  %q -> %q\n  %q -> %q", group[0], base, variant, got)
			}
		}
	}
}

func TestSQLFingerprintKeepsDistinctShapesApart(t *testing.T) {
	distinct := []string{
		"SELECT * FROM orders WHERE status = 'pending'",
		"SELECT * FROM order_items WHERE order_id = 5",
		"DELETE FROM sessions WHERE expires_at < NOW()",
		"INSERT INTO inventory (sku, qty) VALUES (?, ?, ?, ?)", // tuple arity survives IN-collapse
	}
	seen := make(map[string]string, len(distinct))
	for _, sql := range distinct {
		fp := SQLFingerprint(sql)
		if prev, ok := seen[fp]; ok {
			t.Errorf("distinct shapes must not collide: %q and %q both -> %q", prev, sql, fp)
		}
		seen[fp] = sql
	}
}

func TestSQLFingerprintEscapedQuotesAndComments(t *testing.T) {
	a := SQLFingerprint("SELECT * FROM notes WHERE body = 'it\\'s a test'")
	b := SQLFingerprint("SELECT * FROM notes WHERE body = 'other text'")
	if a != b {
		t.Errorf("escaped-quote literals should mask cleanly:\n  %q\n  %q", a, b)
	}
	if strings.Contains(a, "'s") || strings.Contains(a, "''") {
		t.Errorf("fingerprint leaked unmasked literal fragments: %q", a)
	}
}

func TestBuildMySQLSlowLogsGroupedQueryUsesSharedPipeline(t *testing.T) {
	start := time.Date(2026, 9, 2, 12, 0, 0, 0, time.UTC)
	tq := BuildMySQLSlowLogsGroupedQuery(start, start.Add(time.Hour), "backend_ror", 15)
	if tq.Err != nil {
		t.Fatalf("unexpected error: %v", tq.Err)
	}
	q := tq.Query

	// One replace_regex extend per shared pipeline step, seeded from F0
	if got := strings.Count(q, "replace_regex(F"); got != len(sqlFingerprintSteps) {
		t.Errorf("expected %d replace_regex steps, found %d", len(sqlFingerprintSteps), got)
	}
	if !strings.Contains(q, "extend F0 = tostring(SqlText)") {
		t.Errorf("expected F0 seed from SqlText, got: %s", q)
	}
	if !strings.Contains(q, "SqlFingerprint = tolower(F"+strconv.Itoa(len(sqlFingerprintSteps))+")") {
		t.Errorf("expected final lowercase normalization, got: %s", q)
	}
	// Every step must inject exactly one pattern and one replacement as
	// KQL verbatim literals (doubled-quote escapes are legal inside them)
	if got := strings.Count(q, `@"`); got != 2*len(sqlFingerprintSteps) {
		t.Errorf("expected %d verbatim literals, found %d", 2*len(sqlFingerprintSteps), got)
	}
}

func TestKQLVerbatimEscapesQuotes(t *testing.T) {
	got := kqlVerbatim(`"[^"]*"`)
	if got != `@"""[^""]*"""` {
		t.Errorf("kqlVerbatim quote escaping = %q", got)
	}
}
