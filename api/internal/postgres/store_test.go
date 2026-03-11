package postgres

import (
	"strings"
	"testing"
	"time"

	"github.com/lasseh/taillight/internal/model"
)

func TestEscapeLike(t *testing.T) {
	tests := []struct {
		name  string
		input string
		want  string
	}{
		{name: "no metacharacters", input: "hello", want: "hello"},
		{name: "percent", input: "50%", want: `50\%`},
		{name: "underscore", input: "a_b", want: `a\_b`},
		{name: "backslash", input: `a\b`, want: `a\\b`},
		{name: "mixed", input: `100%_foo\bar`, want: `100\%\_foo\\bar`},
		{name: "empty", input: "", want: ""},
		{name: "all metacharacters", input: `%_\`, want: `\%\_\\`},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := escapeLike(tt.input)
			if got != tt.want {
				t.Errorf("escapeLike(%q) = %q, want %q", tt.input, got, tt.want)
			}
		})
	}
}

func TestApplySyslogFilter(t *testing.T) {
	base := psq.Select("id").From("syslog_events")
	now := time.Date(2025, 6, 1, 12, 0, 0, 0, time.UTC)

	tests := []struct {
		name       string
		filter     model.SyslogFilter
		wantSQL    []string // substrings that must appear in generated SQL
		wantNotSQL []string // substrings that must NOT appear
		wantArgs   int      // expected number of args
	}{
		{
			name:     "empty filter",
			filter:   model.SyslogFilter{},
			wantArgs: 0,
		},
		{
			name:     "hostname exact",
			filter:   model.SyslogFilter{Hostname: "web01"},
			wantSQL:  []string{"hostname"},
			wantArgs: 1,
		},
		{
			name:     "hostname wildcard",
			filter:   model.SyslogFilter{Hostname: "web*"},
			wantSQL:  []string{"hostname ILIKE"},
			wantArgs: 1,
		},
		{
			name:     "fromhost_ip",
			filter:   model.SyslogFilter{FromhostIP: "192.168.1.1"},
			wantSQL:  []string{"fromhost_ip", "::inet"},
			wantArgs: 1,
		},
		{
			name:     "programname",
			filter:   model.SyslogFilter{Programname: "sshd"},
			wantSQL:  []string{"programname"},
			wantArgs: 1,
		},
		{
			name:     "severity exact",
			filter:   model.SyslogFilter{Severity: new(3)},
			wantSQL:  []string{"severity"},
			wantArgs: 1,
		},
		{
			name:     "severity_max",
			filter:   model.SyslogFilter{SeverityMax: new(4)},
			wantSQL:  []string{"severity"},
			wantArgs: 1,
		},
		{
			name:     "facility",
			filter:   model.SyslogFilter{Facility: new(1)},
			wantSQL:  []string{"facility"},
			wantArgs: 1,
		},
		{
			name:     "syslogtag",
			filter:   model.SyslogFilter{SyslogTag: "kernel:"},
			wantSQL:  []string{"syslogtag"},
			wantArgs: 1,
		},
		{
			name:     "msgid",
			filter:   model.SyslogFilter{MsgID: "OSPF_NBR_UP"},
			wantSQL:  []string{"msgid"},
			wantArgs: 1,
		},
		{
			name:     "search",
			filter:   model.SyslogFilter{Search: "error"},
			wantSQL:  []string{"message ILIKE"},
			wantArgs: 1,
		},
		{
			name:     "from time",
			filter:   model.SyslogFilter{From: &now},
			wantSQL:  []string{"received_at"},
			wantArgs: 1,
		},
		{
			name:     "to time",
			filter:   model.SyslogFilter{To: &now},
			wantSQL:  []string{"received_at"},
			wantArgs: 1,
		},
		{
			name: "combined filters",
			filter: model.SyslogFilter{
				Hostname:    "web*",
				Programname: "nginx",
				SeverityMax: new(4),
				Search:      "timeout",
				From:        &now,
			},
			wantSQL:  []string{"hostname ILIKE", "programname", "severity", "message ILIKE", "received_at"},
			wantArgs: 5,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			qb := applySyslogFilter(base, tt.filter)
			sql, args, err := qb.ToSql()
			if err != nil {
				t.Fatalf("ToSql() error: %v", err)
			}

			for _, want := range tt.wantSQL {
				if !strings.Contains(sql, want) {
					t.Errorf("SQL %q does not contain %q", sql, want)
				}
			}
			for _, notWant := range tt.wantNotSQL {
				if strings.Contains(sql, notWant) {
					t.Errorf("SQL %q should not contain %q", sql, notWant)
				}
			}
			if len(args) != tt.wantArgs {
				t.Errorf("got %d args, want %d; args: %v", len(args), tt.wantArgs, args)
			}
		})
	}
}

func TestApplySyslogFilter_SearchEscapesLike(t *testing.T) {
	base := psq.Select("id").From("syslog_events")
	f := model.SyslogFilter{Search: "50%_off"}
	qb := applySyslogFilter(base, f)
	_, args, err := qb.ToSql()
	if err != nil {
		t.Fatalf("ToSql() error: %v", err)
	}
	if len(args) != 1 {
		t.Fatalf("expected 1 arg, got %d", len(args))
	}
	arg, ok := args[0].(string)
	if !ok {
		t.Fatalf("expected string arg, got %T", args[0])
	}
	// The LIKE pattern should have escaped metacharacters wrapped in %...%.
	if !strings.Contains(arg, `\%`) || !strings.Contains(arg, `\_`) {
		t.Errorf("search arg %q should contain escaped %% and _", arg)
	}
}

func TestApplySyslogFilter_HostnameWildcardPattern(t *testing.T) {
	base := psq.Select("id").From("syslog_events")
	f := model.SyslogFilter{Hostname: "web*.example.com"}
	qb := applySyslogFilter(base, f)
	_, args, err := qb.ToSql()
	if err != nil {
		t.Fatalf("ToSql() error: %v", err)
	}
	if len(args) != 1 {
		t.Fatalf("expected 1 arg, got %d", len(args))
	}
	arg, ok := args[0].(string)
	if !ok {
		t.Fatalf("expected string arg, got %T", args[0])
	}
	// The wildcard * should be converted to % for ILIKE.
	if !strings.Contains(arg, "%") {
		t.Errorf("hostname wildcard arg %q should contain %%", arg)
	}
	if strings.Contains(arg, "*") {
		t.Errorf("hostname wildcard arg %q should not contain *", arg)
	}
}
