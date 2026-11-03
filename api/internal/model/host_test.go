package model

import (
	"strings"
	"testing"
	"time"
)

func TestParseHostQuery(t *testing.T) {
	tests := []struct {
		name    string
		query   string
		want    HostQuery
		wantErr string
	}{
		{
			name:  "defaults",
			query: "",
			want:  HostQuery{RangeDur: 24 * time.Hour, Sort: "errors", Limit: HostsDefaultLimit},
		},
		{
			name:  "all parameters",
			query: "range=7d&search=web&status=critical&feed=both&sort=last_seen&limit=50&offset=100",
			want: HostQuery{
				RangeDur: 7 * 24 * time.Hour, Search: "web", Status: HostStatusCritical,
				Feed: "both", Sort: "last_seen", Limit: 50, Offset: 100,
			},
		},
		{
			name:  "limit above the cap is clamped",
			query: "limit=100000",
			want:  HostQuery{RangeDur: 24 * time.Hour, Sort: "errors", Limit: HostsMaxLimit},
		},
		{name: "unknown range", query: "range=2y", wantErr: "invalid range"},
		{name: "unknown status", query: "status=down", wantErr: "status: must be one of"},
		{name: "unknown feed", query: "feed=applog", wantErr: "feed: must be one of"},
		{name: "unknown sort", query: "sort=random", wantErr: "sort: must be one of"},
		{name: "negative offset", query: "offset=-1", wantErr: "offset: must be an integer"},
		{name: "search too long", query: "search=" + strings.Repeat("a", maxFilterStringLen+1), wantErr: "search: exceeds max length"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := ParseHostQuery(req(tt.query))
			if tt.wantErr != "" {
				if err == nil || !strings.Contains(err.Error(), tt.wantErr) {
					t.Fatalf("err = %v, want it to contain %q", err, tt.wantErr)
				}
				return
			}
			if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}
			if got != tt.want {
				t.Fatalf("ParseHostQuery = %+v, want %+v", got, tt.want)
			}
		})
	}
}

func TestParseVolumeParamsTotalsOnly(t *testing.T) {
	tests := []struct {
		query   string
		want    bool
		wantErr bool
	}{
		{"", false, false},
		{"totals_only=true", true, false},
		{"totals_only=false", false, false},
		{"totals_only=maybe", false, true},
	}
	for _, tt := range tests {
		t.Run(tt.query, func(t *testing.T) {
			got, err := ParseVolumeParams(req(tt.query))
			if (err != nil) != tt.wantErr {
				t.Fatalf("err = %v, want error %v", err, tt.wantErr)
			}
			if err == nil && got.TotalsOnly != tt.want {
				t.Fatalf("TotalsOnly = %v, want %v", got.TotalsOnly, tt.want)
			}
		})
	}
}
