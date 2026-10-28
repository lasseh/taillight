package postgres

import (
	"reflect"
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

// fullNetlogFilter sets every field the builder reads.
func fullNetlogFilter() model.NetlogFilter {
	sev, sevMax, fac := 3, 5, 23
	from := time.Date(2025, 1, 1, 0, 0, 0, 0, time.UTC)
	to := from.Add(time.Hour)
	return model.NetlogFilter{
		Hostname: "edge*", FromhostIP: "10.0.0.1", Programname: "rpd",
		Severity: &sev, SeverityMax: &sevMax, Facility: &fac,
		SyslogTag: "rpd[1]:", MsgID: "M", Search: "ae30", From: &from, To: &to,
	}
}

// The SQL for a filter without exclusions is pinned as a literal so that
// hide_muted support cannot change the query every other caller runs.
func TestApplyNetlogFilterSQLIsPinned(t *testing.T) {
	f := fullNetlogFilter()
	sql, args, err := applyNetlogFilter(psq.Select("id").From("netlog_events"), f).ToSql()
	if err != nil {
		t.Fatalf("ToSql: %v", err)
	}
	const wantSQL = "SELECT id FROM netlog_events WHERE hostname ILIKE $1 AND fromhost_ip = $2::inet" +
		" AND programname = $3 AND severity = $4 AND severity <= $5 AND facility = $6" +
		" AND syslogtag = $7 AND msgid = $8 AND message ILIKE $9" +
		" AND received_at >= $10 AND received_at <= $11"
	if sql != wantSQL {
		t.Fatalf("sql:\n got %q\nwant %q", sql, wantSQL)
	}
	wantArgs := []any{"edge%", "10.0.0.1", "rpd", 3, 5, 23, "rpd[1]:", "M", "%ae30%", *f.From, *f.To}
	if !reflect.DeepEqual(args, wantArgs) {
		t.Fatalf("args:\n got %#v\nwant %#v", args, wantArgs)
	}
}

func TestApplyNetlogFilterExclusions(t *testing.T) {
	sev := 3
	tests := []struct {
		name     string
		filter   model.NetlogFilter
		wantSQL  string
		wantArgs []any
	}{
		{
			name:     "no exclusions leaves the query unchanged",
			filter:   model.NetlogFilter{Severity: &sev},
			wantSQL:  "SELECT id FROM t WHERE severity = $1",
			wantArgs: []any{3},
		},
		{
			name: "one exclusion is a NOT of its conjunction, LIKE-escaped",
			filter: model.NetlogFilter{Exclude: []model.NetlogFilter{
				{Hostname: "leaf-02", Search: `50%_a\b`},
			}},
			wantSQL:  "SELECT id FROM t WHERE NOT (hostname = $1 AND message ILIKE $2)",
			wantArgs: []any{"leaf-02", `%50\%\_a\\b%`},
		},
		{
			name: "a wildcard-host exclusion is NOT (hostname ILIKE ...)",
			filter: model.NetlogFilter{Exclude: []model.NetlogFilter{
				{Hostname: "edge*_1"},
			}},
			wantSQL:  "SELECT id FROM t WHERE NOT (hostname ILIKE $1)",
			wantArgs: []any{`edge%\_1`},
		},
		{
			name: "exclusions follow the positive filter, one NOT per rule",
			filter: model.NetlogFilter{
				Severity: &sev,
				Exclude: []model.NetlogFilter{
					{Programname: "rpd"},
					{Hostname: "leaf-02", Search: "ae30"},
				},
			},
			wantSQL:  "SELECT id FROM t WHERE severity = $1 AND NOT (programname = $2) AND NOT (hostname = $3 AND message ILIKE $4)",
			wantArgs: []any{3, "rpd", "leaf-02", "%ae30%"},
		},
		{
			name:     "an exclusion with no conditions emits nothing",
			filter:   model.NetlogFilter{Severity: &sev, Exclude: []model.NetlogFilter{{}}},
			wantSQL:  "SELECT id FROM t WHERE severity = $1",
			wantArgs: []any{3},
		},
		{
			name: "a nested exclusion is ignored",
			filter: model.NetlogFilter{Exclude: []model.NetlogFilter{
				{Programname: "rpd", Exclude: []model.NetlogFilter{{Hostname: "x"}}},
			}},
			wantSQL:  "SELECT id FROM t WHERE NOT (programname = $1)",
			wantArgs: []any{"rpd"},
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			sql, args, err := applyNetlogFilter(psq.Select("id").From("t"), tt.filter).ToSql()
			if err != nil {
				t.Fatalf("ToSql: %v", err)
			}
			if sql != tt.wantSQL {
				t.Fatalf("sql:\n got %q\nwant %q", sql, tt.wantSQL)
			}
			if !reflect.DeepEqual(args, tt.wantArgs) {
				t.Fatalf("args:\n got %#v\nwant %#v", args, tt.wantArgs)
			}
		})
	}
}

// The whole point of the extraction: srvlog and netlog must produce identical
// SQL for equivalent filters.
func TestSrvlogNetlogFilterParity(t *testing.T) {
	// Every field, so a slip in either field map shows up here.
	net := fullNetlogFilter()
	net.Exclude = []model.NetlogFilter{{Hostname: "fw-0*", Programname: "pf"}, {Search: "noise"}}
	srv := model.SrvlogFilter{
		Hostname: net.Hostname, FromhostIP: net.FromhostIP, Programname: net.Programname,
		Severity: net.Severity, SeverityMax: net.SeverityMax, Facility: net.Facility,
		SyslogTag: net.SyslogTag, MsgID: net.MsgID, Search: net.Search, From: net.From, To: net.To,
		Exclude: []model.SrvlogFilter{{Hostname: "fw-0*", Programname: "pf"}, {Search: "noise"}},
	}

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
