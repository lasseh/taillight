package notification

import (
	"strings"
	"testing"
)

func intPtr(v int) *int { return &v }

// TestRuleValidate_Bounds asserts Validate rejects the values the DB CHECK would
// reject — with a clear message — rather than letting them through to an opaque
// 500 at insert time (audit N6).
func TestRuleValidate_Bounds(t *testing.T) {
	tests := []struct {
		name    string
		rule    Rule
		wantErr string // substring; "" means expect success
	}{
		{
			name: "applog alias level rejected",
			rule: Rule{Name: "r", EventKind: EventKindAppLog, Level: "warning"},
			wantErr: "level must be one of",
		},
		{
			name: "applog canonical level ok",
			rule: Rule{Name: "r", EventKind: EventKindAppLog, Level: "WARN"},
		},
		{
			name:    "srvlog severity out of range",
			rule:    Rule{Name: "r", EventKind: EventKindSrvlog, Severity: intPtr(9)},
			wantErr: "severity must be between 0 and 7",
		},
		{
			name:    "srvlog severity_max out of range",
			rule:    Rule{Name: "r", EventKind: EventKindSrvlog, SeverityMax: intPtr(-1)},
			wantErr: "severity_max must be between 0 and 7",
		},
		{
			name:    "netlog facility out of range",
			rule:    Rule{Name: "r", EventKind: EventKindNetlog, Facility: intPtr(30)},
			wantErr: "facility must be between 0 and 23",
		},
		{
			name: "srvlog valid bounds ok",
			rule: Rule{Name: "r", EventKind: EventKindSrvlog, Severity: intPtr(0), SeverityMax: intPtr(7), Facility: intPtr(23)},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := tt.rule.Validate()
			if tt.wantErr == "" {
				if err != nil {
					t.Fatalf("Validate() = %v, want nil", err)
				}
				return
			}
			if err == nil || !strings.Contains(err.Error(), tt.wantErr) {
				t.Fatalf("Validate() = %v, want error containing %q", err, tt.wantErr)
			}
		})
	}
}
