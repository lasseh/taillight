package postgres

import (
	"strings"
	"testing"
	"time"

	"github.com/lasseh/taillight/internal/model"
)

func buildSyslogSQL(t *testing.T, f syslogFilterClause) (string, []any) {
	t.Helper()
	qb := applySyslogFilter(psq.Select("id").From("srvlog_events"), f)
	sql, args, err := qb.ToSql()
	if err != nil {
		t.Fatalf("ToSql() error = %v", err)
	}
	return sql, args
}

func TestApplySyslogFilter(t *testing.T) {
	sev, sevMax, fac := 3, 5, 23
	from := time.Date(2025, 1, 1, 0, 0, 0, 0, time.UTC)

	t.Run("empty filter has no WHERE", func(t *testing.T) {
		sql, args := buildSyslogSQL(t, syslogFilterClause{})
		if strings.Contains(sql, "WHERE") {
			t.Fatalf("expected no WHERE, got %q", sql)
		}
		if len(args) != 0 {
			t.Fatalf("expected no args, got %v", args)
		}
	})

	t.Run("plain hostname is equality", func(t *testing.T) {
		sql, args := buildSyslogSQL(t, syslogFilterClause{Hostname: "router1"})
		if !strings.Contains(sql, "hostname = $1") || args[0] != "router1" {
			t.Fatalf("sql=%q args=%v", sql, args)
		}
	})

	t.Run("wildcard hostname becomes escaped ILIKE", func(t *testing.T) {
		sql, args := buildSyslogSQL(t, syslogFilterClause{Hostname: "ro*_x"})
		if !strings.Contains(sql, "hostname ILIKE $1") {
			t.Fatalf("expected ILIKE, sql=%q", sql)
		}
		// _ is a LIKE metachar and must be escaped; * becomes %.
		if args[0] != `ro%\_x` {
			t.Fatalf("pattern = %q, want %q", args[0], `ro%\_x`)
		}
	})

	t.Run("fromhost_ip casts to inet", func(t *testing.T) {
		sql, args := buildSyslogSQL(t, syslogFilterClause{FromhostIP: "10.0.0.1"})
		if !strings.Contains(sql, "fromhost_ip = $1::inet") || args[0] != "10.0.0.1" {
			t.Fatalf("sql=%q args=%v", sql, args)
		}
	})

	t.Run("severity range", func(t *testing.T) {
		sql, _ := buildSyslogSQL(t, syslogFilterClause{Severity: &sev, SeverityMax: &sevMax, Facility: &fac})
		for _, want := range []string{"severity = $", "severity <= $", "facility = $"} {
			if !strings.Contains(sql, want) {
				t.Fatalf("missing %q in %q", want, sql)
			}
		}
	})

	t.Run("search is escaped and wrapped", func(t *testing.T) {
		sql, args := buildSyslogSQL(t, syslogFilterClause{Search: "50%_off"})
		if !strings.Contains(sql, "message ILIKE $1") {
			t.Fatalf("expected message ILIKE, sql=%q", sql)
		}
		if args[0] != `%50\%\_off%` {
			t.Fatalf("search pattern = %q, want %q", args[0], `%50\%\_off%`)
		}
	})

	t.Run("time bounds", func(t *testing.T) {
		sql, _ := buildSyslogSQL(t, syslogFilterClause{From: &from, To: &from})
		if !strings.Contains(sql, "received_at >= $") || !strings.Contains(sql, "received_at <= $") {
			t.Fatalf("missing time bounds in %q", sql)
		}
	})
}

// The whole point of the extraction: srvlog and netlog must produce identical
// SQL for equivalent filters.
func TestSrvlogNetlogFilterParity(t *testing.T) {
	sev := 4
	srv := model.SrvlogFilter{Hostname: "fw*", FromhostIP: "10.0.0.2", Severity: &sev, Search: "deny"}
	net := model.NetlogFilter{Hostname: "fw*", FromhostIP: "10.0.0.2", Severity: &sev, Search: "deny"}

	srvSQL, srvArgs, err := applySrvlogFilter(psq.Select("id").From("t"), srv).ToSql()
	if err != nil {
		t.Fatalf("srvlog ToSql: %v", err)
	}
	netSQL, netArgs, err := applyNetlogFilter(psq.Select("id").From("t"), net).ToSql()
	if err != nil {
		t.Fatalf("netlog ToSql: %v", err)
	}
	if srvSQL != netSQL {
		t.Fatalf("SQL diverged:\n srvlog=%q\n netlog=%q", srvSQL, netSQL)
	}
	if len(srvArgs) != len(netArgs) {
		t.Fatalf("arg count diverged: %v vs %v", srvArgs, netArgs)
	}
	for i := range srvArgs {
		if srvArgs[i] != netArgs[i] {
			t.Fatalf("arg %d diverged: %v vs %v", i, srvArgs[i], netArgs[i])
		}
	}
}
