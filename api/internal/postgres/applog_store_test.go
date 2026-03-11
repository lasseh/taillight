package postgres

import (
	"sort"
	"strings"
	"testing"
	"time"

	"github.com/lasseh/taillight/internal/model"
)

func TestApplyAppLogFilter(t *testing.T) {
	base := psq.Select("id").From("applog_events")
	now := time.Date(2025, 6, 1, 12, 0, 0, 0, time.UTC)

	tests := []struct {
		name     string
		filter   model.AppLogFilter
		wantSQL  []string
		wantArgs int
	}{
		{
			name:     "empty filter",
			filter:   model.AppLogFilter{},
			wantArgs: 0,
		},
		{
			name:     "service",
			filter:   model.AppLogFilter{Service: "api-gateway"},
			wantSQL:  []string{"service"},
			wantArgs: 1,
		},
		{
			name:     "component",
			filter:   model.AppLogFilter{Component: "auth"},
			wantSQL:  []string{"component"},
			wantArgs: 1,
		},
		{
			name:     "host exact",
			filter:   model.AppLogFilter{Host: "web01"},
			wantSQL:  []string{"host"},
			wantArgs: 1,
		},
		{
			name:     "host wildcard",
			filter:   model.AppLogFilter{Host: "web*"},
			wantSQL:  []string{"host ILIKE"},
			wantArgs: 1,
		},
		{
			name:     "level WARN includes WARN ERROR FATAL",
			filter:   model.AppLogFilter{Level: "WARN"},
			wantSQL:  []string{"level"},
			wantArgs: 3, // WARN, ERROR, FATAL
		},
		{
			name:     "level ERROR includes ERROR FATAL",
			filter:   model.AppLogFilter{Level: "ERROR"},
			wantSQL:  []string{"level"},
			wantArgs: 2, // ERROR, FATAL
		},
		{
			name:     "search uses FTS",
			filter:   model.AppLogFilter{Search: "connection refused"},
			wantSQL:  []string{"search_vector @@ plainto_tsquery"},
			wantArgs: 1,
		},
		{
			name:     "from time",
			filter:   model.AppLogFilter{From: &now},
			wantSQL:  []string{"received_at"},
			wantArgs: 1,
		},
		{
			name:     "to time",
			filter:   model.AppLogFilter{To: &now},
			wantSQL:  []string{"received_at"},
			wantArgs: 1,
		},
		{
			name: "combined",
			filter: model.AppLogFilter{
				Service: "api",
				Host:    "prod*",
				Level:   "ERROR",
				From:    &now,
			},
			wantSQL:  []string{"service", "host ILIKE", "level", "received_at"},
			wantArgs: 5, // service, host pattern, ERROR, FATAL, from time
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			qb := applyAppLogFilter(base, tt.filter)
			sql, args, err := qb.ToSql()
			if err != nil {
				t.Fatalf("ToSql() error: %v", err)
			}

			for _, want := range tt.wantSQL {
				if !strings.Contains(sql, want) {
					t.Errorf("SQL %q does not contain %q", sql, want)
				}
			}
			if len(args) != tt.wantArgs {
				t.Errorf("got %d args, want %d; args: %v; sql: %s", len(args), tt.wantArgs, args, sql)
			}
		})
	}
}

func TestAppLogLevelsAtOrAbove(t *testing.T) {
	tests := []struct {
		name    string
		minRank int
		want    []string
	}{
		{
			name:    "rank 0 returns all levels",
			minRank: 0,
			want:    []string{"DEBUG", "ERROR", "FATAL", "INFO", "WARN"},
		},
		{
			name:    "rank 1 returns INFO and above",
			minRank: 1,
			want:    []string{"ERROR", "FATAL", "INFO", "WARN"},
		},
		{
			name:    "rank 2 returns WARN ERROR FATAL",
			minRank: 2,
			want:    []string{"ERROR", "FATAL", "WARN"},
		},
		{
			name:    "rank 3 returns ERROR FATAL",
			minRank: 3,
			want:    []string{"ERROR", "FATAL"},
		},
		{
			name:    "rank 4 returns only FATAL",
			minRank: 4,
			want:    []string{"FATAL"},
		},
		{
			name:    "rank 5 returns nothing",
			minRank: 5,
			want:    nil,
		},
		{
			name:    "negative rank returns all levels",
			minRank: -1,
			want:    []string{"DEBUG", "ERROR", "FATAL", "INFO", "WARN"},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := appLogLevelsAtOrAbove(tt.minRank)
			sort.Strings(got)
			sort.Strings(tt.want)

			if len(got) != len(tt.want) {
				t.Fatalf("got %v, want %v", got, tt.want)
			}
			for i := range got {
				if got[i] != tt.want[i] {
					t.Errorf("got[%d] = %q, want %q", i, got[i], tt.want[i])
				}
			}
		})
	}
}

func TestApplyAppLogFilter_HostWildcardPattern(t *testing.T) {
	base := psq.Select("id").From("applog_events")
	f := model.AppLogFilter{Host: "prod-*.example.com"}
	qb := applyAppLogFilter(base, f)
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
	if !strings.Contains(arg, "%") {
		t.Errorf("host wildcard arg %q should contain %%", arg)
	}
	if strings.Contains(arg, "*") {
		t.Errorf("host wildcard arg %q should not contain *", arg)
	}
}
