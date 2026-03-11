package notification

import (
	"testing"
	"time"

	"github.com/lasseh/taillight/internal/model"
)

func TestRule_MatchesSyslog(t *testing.T) {
	event := model.SyslogEvent{
		ID:            1,
		ReceivedAt:    time.Now(),
		Hostname:      "router1.example.com",
		Programname:   "rpd",
		Severity:      3,
		SeverityLabel: "err",
		Facility:      1,
		SyslogTag:     "rpd[1234]",
		MsgID:         "BGP_PREFIX_THRESH_EXCEEDED",
		Message:       "BGP peer 10.0.0.1 connection lost",
	}

	tests := []struct {
		name string
		rule Rule
		want bool
	}{
		{
			name: "empty rule matches everything",
			rule: Rule{EventKind: EventKindSyslog},
			want: true,
		},
		{
			name: "hostname exact match",
			rule: Rule{EventKind: EventKindSyslog, Hostname: "router1.example.com"},
			want: true,
		},
		{
			name: "hostname wildcard match",
			rule: Rule{EventKind: EventKindSyslog, Hostname: "router*"},
			want: true,
		},
		{
			name: "hostname mismatch",
			rule: Rule{EventKind: EventKindSyslog, Hostname: "switch1"},
			want: false,
		},
		{
			name: "programname match",
			rule: Rule{EventKind: EventKindSyslog, Programname: "rpd"},
			want: true,
		},
		{
			name: "programname mismatch",
			rule: Rule{EventKind: EventKindSyslog, Programname: "sshd"},
			want: false,
		},
		{
			name: "severity exact match",
			rule: Rule{EventKind: EventKindSyslog, Severity: new(3)},
			want: true,
		},
		{
			name: "severity mismatch",
			rule: Rule{EventKind: EventKindSyslog, Severity: new(0)},
			want: false,
		},
		{
			name: "severity_max match",
			rule: Rule{EventKind: EventKindSyslog, SeverityMax: new(4)},
			want: true,
		},
		{
			name: "severity_max excludes",
			rule: Rule{EventKind: EventKindSyslog, SeverityMax: new(2)},
			want: false,
		},
		{
			name: "search match",
			rule: Rule{EventKind: EventKindSyslog, Search: "connection lost"},
			want: true,
		},
		{
			name: "search mismatch",
			rule: Rule{EventKind: EventKindSyslog, Search: "authentication failed"},
			want: false,
		},
		{
			name: "combined filters match",
			rule: Rule{EventKind: EventKindSyslog, Hostname: "router*", Programname: "rpd", SeverityMax: new(3)},
			want: true,
		},
		{
			name: "combined filters one fails",
			rule: Rule{EventKind: EventKindSyslog, Hostname: "router*", Programname: "sshd"},
			want: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := tt.rule.MatchesSyslog(event)
			if got != tt.want {
				t.Errorf("MatchesSyslog() = %v, want %v", got, tt.want)
			}
		})
	}
}

func TestRule_MatchesAppLog(t *testing.T) {
	event := model.AppLogEvent{
		ID:        1,
		Timestamp: time.Now(),
		Level:     "ERROR",
		Service:   "api-gateway",
		Component: "auth",
		Host:      "web1.example.com",
		Msg:       "failed to validate token",
	}

	tests := []struct {
		name string
		rule Rule
		want bool
	}{
		{
			name: "empty rule matches everything",
			rule: Rule{EventKind: EventKindAppLog},
			want: true,
		},
		{
			name: "service match",
			rule: Rule{EventKind: EventKindAppLog, Service: "api-gateway"},
			want: true,
		},
		{
			name: "service mismatch",
			rule: Rule{EventKind: EventKindAppLog, Service: "backend"},
			want: false,
		},
		{
			name: "level WARN matches ERROR",
			rule: Rule{EventKind: EventKindAppLog, Level: "WARN"},
			want: true,
		},
		{
			name: "level FATAL excludes ERROR",
			rule: Rule{EventKind: EventKindAppLog, Level: "FATAL"},
			want: false,
		},
		{
			name: "component match",
			rule: Rule{EventKind: EventKindAppLog, Component: "auth"},
			want: true,
		},
		{
			name: "search match",
			rule: Rule{EventKind: EventKindAppLog, Search: "validate token"},
			want: true,
		},
		{
			name: "combined match",
			rule: Rule{EventKind: EventKindAppLog, Service: "api-gateway", Level: "ERROR", Search: "token"},
			want: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := tt.rule.MatchesAppLog(event)
			if got != tt.want {
				t.Errorf("MatchesAppLog() = %v, want %v", got, tt.want)
			}
		})
	}
}
