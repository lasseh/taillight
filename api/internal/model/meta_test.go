package model

import (
	"net/url"
	"strings"
	"testing"
)

func TestParseMetaSearch(t *testing.T) {
	tests := []struct {
		name    string
		query   string
		want    string
		wantErr string
	}{
		{name: "absent", query: "", want: ""},
		{name: "substring", query: "search=core", want: "core"},
		{name: "at the max length", query: "search=" + strings.Repeat("a", maxFilterStringLen), want: strings.Repeat("a", maxFilterStringLen)},
		{name: "too long", query: "search=" + strings.Repeat("a", maxFilterStringLen+1), wantErr: "search: exceeds max length"},
		{name: "like metacharacters kept as typed", query: "search=" + url.QueryEscape("a_b%"), want: "a_b%"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := ParseMetaSearch(req(tt.query))
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
				t.Fatalf("ParseMetaSearch = %q, want %q", got, tt.want)
			}
		})
	}
}
