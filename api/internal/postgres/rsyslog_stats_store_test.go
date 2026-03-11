package postgres

import "testing"

func TestAllowedStatsFields(t *testing.T) {
	known := []string{
		"submitted", "enqueued", "size", "processed", "failed",
		"suspended", "discarded.full", "discarded.nf", "maxqsize",
	}
	for _, f := range known {
		if _, ok := allowedStatsFields[f]; !ok {
			t.Errorf("expected %q to be in allowedStatsFields", f)
		}
	}

	unknown := []string{"dropped", "queued", "total", "bytes", ""}
	for _, f := range unknown {
		if _, ok := allowedStatsFields[f]; ok {
			t.Errorf("expected %q to NOT be in allowedStatsFields", f)
		}
	}
}

func TestWorkerRegexp(t *testing.T) {
	tests := []struct {
		name  string
		input string
		match bool
	}{
		{name: "imudp worker", input: "imudp(w0)", match: true},
		{name: "imtcp worker", input: "imtcp(w3)", match: true},
		{name: "worker prefix", input: "w0/imtcp", match: true},
		{name: "worker prefix high", input: "w12/imudp", match: true},
		{name: "main queue no match", input: "main Q", match: false},
		{name: "plain imudp no match", input: "imudp", match: false},
		{name: "syslog_to_pgsql no match", input: "syslog_to_pgsql", match: false},
		{name: "action ompgsql no match", input: "action-1-builtin:ompgsql", match: false},
		{name: "empty no match", input: "", match: false},
		{name: "parenthesized non-worker no match", input: "imudp(cfg)", match: false},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := workerRe.MatchString(tt.input)
			if got != tt.match {
				t.Errorf("workerRe.MatchString(%q) = %v, want %v", tt.input, got, tt.match)
			}
		})
	}
}
