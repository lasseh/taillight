package mute

import (
	"strings"
	"testing"
	"time"
)

func TestExpiresAt(t *testing.T) {
	now := time.Date(2026, 10, 28, 10, 0, 0, 0, time.UTC)
	tests := []struct {
		name    string
		ms      int64
		want    time.Time
		wantErr bool
	}{
		{name: "zero means 24h default", ms: 0, want: now.Add(24 * time.Hour)},
		{name: "one millisecond", ms: 1, want: now.Add(time.Millisecond)},
		{name: "exactly 7 days", ms: 604800000, want: now.Add(7 * 24 * time.Hour)},
		{name: "7 days plus 1ms", ms: 604800001, wantErr: true},
		{name: "negative", ms: -1, wantErr: true},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := ExpiresAt(now, tt.ms)
			if tt.wantErr {
				if err == nil {
					t.Fatalf("ExpiresAt(now, %d) = %v, want error", tt.ms, got)
				}
				return
			}
			if err != nil {
				t.Fatalf("ExpiresAt(now, %d) error: %v", tt.ms, err)
			}
			if !got.Equal(tt.want) {
				t.Errorf("ExpiresAt(now, %d) = %v, want %v", tt.ms, got, tt.want)
			}
		})
	}
}

func TestRuleValidate(t *testing.T) {
	valid := Rule{Feed: FeedNetlog, Hostname: "edge1", Reason: "known flap"}
	long := strings.Repeat("a", 501)
	tests := []struct {
		name    string
		mutate  func(r *Rule)
		wantErr bool
	}{
		{name: "valid hostname rule", mutate: func(*Rule) {}},
		{name: "srvlog feed", mutate: func(r *Rule) { r.Feed = FeedSrvlog }},
		{name: "programname only", mutate: func(r *Rule) { r.Hostname = ""; r.Programname = "rpd" }},
		{name: "search only", mutate: func(r *Rule) { r.Hostname = ""; r.Search = "bundle ae30" }},
		{name: "hostname *a is a real pattern", mutate: func(r *Rule) { r.Hostname = "*a" }},
		{name: "fields at 500 bytes", mutate: func(r *Rule) {
			r.Hostname = long[:500]
			r.Programname = long[:500]
			r.Search = long[:500]
			r.Reason = long[:500]
		}},
		{name: "empty match", mutate: func(r *Rule) { r.Hostname = "" }, wantErr: true},
		{name: "whitespace-only match", mutate: func(r *Rule) { r.Hostname = " "; r.Programname = "  "; r.Search = "\t" }, wantErr: true},
		{name: "hostname *", mutate: func(r *Rule) { r.Hostname = "*" }, wantErr: true},
		{name: "hostname **", mutate: func(r *Rule) { r.Hostname = "**" }, wantErr: true},
		{name: "hostname space-star-space", mutate: func(r *Rule) { r.Hostname = " * " }, wantErr: true},
		{name: "blank reason", mutate: func(r *Rule) { r.Reason = "   " }, wantErr: true},
		{name: "empty reason", mutate: func(r *Rule) { r.Reason = "" }, wantErr: true},
		{name: "hostname over 500 bytes", mutate: func(r *Rule) { r.Hostname = long }, wantErr: true},
		{name: "programname over 500 bytes", mutate: func(r *Rule) { r.Programname = long }, wantErr: true},
		{name: "search over 500 bytes", mutate: func(r *Rule) { r.Search = long }, wantErr: true},
		{name: "reason over 500 bytes", mutate: func(r *Rule) { r.Reason = long }, wantErr: true},
		{name: "multibyte search over 500 bytes", mutate: func(r *Rule) { r.Search = strings.Repeat("æ", 251) }, wantErr: true},
		{name: "bad feed", mutate: func(r *Rule) { r.Feed = "applog" }, wantErr: true},
		{name: "empty feed", mutate: func(r *Rule) { r.Feed = "" }, wantErr: true},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			r := valid
			tt.mutate(&r)
			err := r.Validate()
			if tt.wantErr && err == nil {
				t.Fatalf("Validate() = nil, want error for %+v", r)
			}
			if !tt.wantErr && err != nil {
				t.Fatalf("Validate() = %v, want nil", err)
			}
		})
	}
}
