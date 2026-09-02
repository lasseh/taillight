package postgres

import (
	"strings"
	"testing"

	"github.com/lasseh/taillight/internal/model"
)

func applogScopes() []struct {
	name  string
	scope model.AnalysisScope
} {
	return []struct {
		name  string
		scope model.AnalysisScope
	}{
		{"all services", model.AnalysisScope{Feed: "applog"}},
		{"scoped", model.AnalysisScope{Feed: "applog", Services: []string{"api", "worker"}}},
	}
}

// applogQueryBuilders are the applog analysis queries whose shape matters
// for chunk exclusion, in the same form the syslog guard tests use.
var applogQueryBuilders = map[string]func(model.AnalysisScope) string{
	"applogTopTemplatesQuery":      func(model.AnalysisScope) string { return applogTopTemplatesQuery() },
	"applogNewTemplatesQuery":      applogNewTemplatesQuery,
	"applogDominantTemplatesQuery": applogDominantTemplatesQuery,
}

// TestAppLogAnalysisQueriesHaveNoMaterializedCTE extends the outage guard to
// the applog queries: none of them may reference a CTE more than once.
func TestAppLogAnalysisQueriesHaveNoMaterializedCTE(t *testing.T) {
	for name, build := range applogQueryBuilders {
		for _, tc := range applogScopes() {
			t.Run(name+"/"+tc.name, func(t *testing.T) {
				sql := build(tc.scope)
				for cte, refs := range cteReferenceCounts(sql) {
					if refs > 1 {
						t.Errorf("CTE %q is referenced %d times; Postgres will materialize it "+
							"and the query loses chunk exclusion.\n%s", cte, refs, sql)
					}
				}
			})
		}
	}
}

// TestAppLogNewTemplatesQueryScopesBothSides pins the service filter onto
// both derived tables: a baseline read without it would compare a scoped
// window against the whole fleet and call everything new.
func TestAppLogNewTemplatesQueryScopesBothSides(t *testing.T) {
	for _, tc := range applogScopes() {
		t.Run(tc.name, func(t *testing.T) {
			sql := applogNewTemplatesQuery(tc.scope)
			want := 0
			if !tc.scope.IsAllServices() {
				want = 2
			}
			if got := strings.Count(sql, "service = ANY($6)"); got != want {
				t.Errorf("got %d service filters, want %d\n%s", got, want, sql)
			}
			for _, pred := range []string{"received_at >= $1", "received_at >= $2", "received_at < $1"} {
				if !strings.Contains(sql, pred) {
					t.Errorf("missing predicate %q\n%s", pred, sql)
				}
			}
			if strings.Contains(sql, "NOT EXISTS") {
				t.Errorf("correlated NOT EXISTS reintroduced\n%s", sql)
			}
		})
	}
}

func TestApplogServiceFilterArgs(t *testing.T) {
	scoped := model.AnalysisScope{Feed: "applog", Services: []string{"api"}}
	if got := applogServiceFilter(scoped, 4); got != " AND service = ANY($4)" {
		t.Errorf("applogServiceFilter(scoped) = %q", got)
	}
	if got := applogServiceFilter(model.AnalysisScope{Feed: "applog"}, 4); got != "" {
		t.Errorf("applogServiceFilter(unscoped) = %q, want empty", got)
	}
	args := appendServicesArg([]any{1, 2}, scoped)
	if len(args) != 3 {
		t.Errorf("appendServicesArg(scoped) len = %d, want 3", len(args))
	}
	if args := appendServicesArg([]any{1, 2}, model.AnalysisScope{}); len(args) != 2 {
		t.Errorf("appendServicesArg(unscoped) len = %d, want 2", len(args))
	}
}
