package postgres

import "testing"

func TestQueryOp(t *testing.T) {
	tests := []struct {
		sql  string
		want string
	}{
		{"SELECT * FROM srvlog_events", "SELECT"},
		{"  select id from x", "SELECT"},
		{"INSERT INTO applog_events (id) VALUES ($1)", "INSERT"},
		{"UPDATE sessions SET last_seen_at = now()", "UPDATE"},
		{"DELETE FROM sessions WHERE id = $1", "DELETE"},
		{"WITH t AS (SELECT 1) SELECT * FROM t", "WITH"},
		{"REFRESH MATERIALIZED VIEW x", "REFRESH"},
		{"(SELECT 1)", "other"}, // leading paren — not a bare verb
		{"EXPLAIN ANALYZE SELECT 1", "other"},
		{"", "other"},
	}
	for _, tt := range tests {
		if got := queryOp(tt.sql); got != tt.want {
			t.Errorf("queryOp(%q) = %q, want %q", tt.sql, got, tt.want)
		}
	}
}
