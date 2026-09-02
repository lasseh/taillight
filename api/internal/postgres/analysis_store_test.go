package postgres

import (
	"regexp"
	"strings"
	"testing"

	"github.com/lasseh/taillight/internal/model"
)

// cteDefinition matches a CTE being declared: `WITH name AS (` or `, name AS (`.
// The trailing paren is what keeps it from matching output-column aliases like
// `tm.event_key AS top_msgid` or the subquery alias `(SELECT ...) AS scoped`.
var cteDefinition = regexp.MustCompile(`(?i)(?:WITH|,)\s+([a-z_][a-z0-9_]*)\s+AS\s*\(`)

// cteReferenceCounts reports, for every CTE declared in the query, how many
// times the rest of the query reads from it.
//
// Postgres inlines a CTE only when it is referenced exactly once; a second
// reference makes it MATERIALIZED, which fences the time predicate off from
// the scan and costs the query its chunk exclusion. See the note above
// topErrorHostsQuery for what that did in production.
func cteReferenceCounts(sql string) map[string]int {
	counts := make(map[string]int)
	for _, m := range cteDefinition.FindAllStringSubmatch(sql, -1) {
		counts[strings.ToLower(m[1])] = 0
	}
	for name := range counts {
		ref := regexp.MustCompile(`(?i)\b(?:FROM|JOIN)\s+` + name + `\b`)
		counts[name] = len(ref.FindAllString(sql, -1))
	}
	return counts
}

// analysisScopes covers both the scheduled path (all hosts) and the manual
// path (an explicit host list), which is the one that embeds $3 in the source
// and so ends up repeating that parameter across scans.
func analysisScopes() []struct {
	name  string
	scope model.AnalysisScope
} {
	return []struct {
		name  string
		scope model.AnalysisScope
	}{
		{"all hosts", model.AnalysisScope{Feed: "netlog"}},
		{"scoped", model.AnalysisScope{Feed: "netlog", Hosts: []string{"edge01", "edge02"}}},
	}
}

// TestSyslogSourcesRejectOtherFeeds pins the loud failure: an applog (or
// unknown) scope handed to a syslog query must not quietly read srvlog.
func TestSyslogSourcesRejectOtherFeeds(t *testing.T) {
	for _, feed := range []string{"applog", "all", ""} {
		if got := analysisTableName(feed); got != "" {
			t.Errorf("analysisTableName(%q) = %q, want empty", feed, got)
		}
		if got := analysisAggregateSource(feed); got != "" {
			t.Errorf("analysisAggregateSource(%q) = %q, want empty", feed, got)
		}
	}
}

func TestAnalysisQueriesHaveNoMaterializedCTE(t *testing.T) {
	builders := map[string]func(model.AnalysisScope) string{
		"topErrorHostsQuery": topErrorHostsQuery,
		"newMsgIDsQuery":     newMsgIDsQuery,
	}

	for name, build := range builders {
		for _, tc := range analysisScopes() {
			t.Run(name+"/"+tc.name, func(t *testing.T) {
				sql := build(tc.scope)
				for cte, refs := range cteReferenceCounts(sql) {
					if refs > 1 {
						t.Errorf("CTE %q is referenced %d times; Postgres will materialize it "+
							"and the query loses chunk exclusion. Repeat the source in each "+
							"scan instead.\n%s", cte, refs, sql)
					}
				}
			})
		}
	}
}

func TestTopErrorHostsQueryFiltersEveryScan(t *testing.T) {
	for _, tc := range analysisScopes() {
		t.Run(tc.name, func(t *testing.T) {
			sql := topErrorHostsQuery(tc.scope)

			// Both the host aggregation and the LATERAL must carry the time
			// bound themselves — that is what reaches idx_*_severity_received.
			if got := strings.Count(sql, "received_at >= $1"); got != 2 {
				t.Errorf("got %d occurrences of `received_at >= $1`, want 2 (host counts + LATERAL)\n%s", got, sql)
			}
			if got := strings.Count(sql, "severity <= 3"); got != 2 {
				t.Errorf("got %d occurrences of `severity <= 3`, want 2\n%s", got, sql)
			}
		})
	}
}

func TestNewMsgIDsQueryUsesExcept(t *testing.T) {
	for _, tc := range analysisScopes() {
		t.Run(tc.name, func(t *testing.T) {
			sql := newMsgIDsQuery(tc.scope)

			// EXCEPT lets each window aggregate independently. The old
			// correlated NOT EXISTS matched on a computed event_key that no
			// index can serve, so it degraded into a self-join.
			if !strings.Contains(sql, "EXCEPT") {
				t.Errorf("expected an EXCEPT set difference\n%s", sql)
			}
			if strings.Contains(sql, "NOT EXISTS") {
				t.Errorf("correlated NOT EXISTS reintroduced\n%s", sql)
			}
			// Current window and baseline window, each bounded on its own side.
			for _, want := range []string{"received_at >= $1", "received_at >= $2", "received_at < $1"} {
				if !strings.Contains(sql, want) {
					t.Errorf("missing predicate %q\n%s", want, sql)
				}
			}
		})
	}
}

// TestCTEReferenceCountsIgnoresAliases guards the detector itself: column and
// subquery aliases must not be mistaken for CTE declarations, or the check
// above would pass vacuously.
func TestCTEReferenceCountsIgnoresAliases(t *testing.T) {
	counts := cteReferenceCounts(`
		WITH host_counts AS (SELECT 1 AS cnt)
		SELECT hc.cnt AS total
		FROM host_counts hc
		JOIN (SELECT 2) AS scoped ON true`)

	if len(counts) != 1 {
		t.Fatalf("detected %d CTEs (%v), want exactly host_counts", len(counts), counts)
	}
	if counts["host_counts"] != 1 {
		t.Errorf("host_counts referenced %d times, want 1", counts["host_counts"])
	}
}

// TestCTEReferenceCountsDetectsFence is the negative case: the exact shape
// that broke production must be reported as a fence.
func TestCTEReferenceCountsDetectsFence(t *testing.T) {
	counts := cteReferenceCounts(`
		WITH events AS (SELECT * FROM netlog_events), host_counts AS (
			SELECT hostname FROM events
		)
		SELECT * FROM host_counts hc
		LEFT JOIN LATERAL (SELECT 1 FROM events) tm ON true`)

	if counts["events"] != 2 {
		t.Errorf("events referenced %d times, want 2 (the detector missed the fence)", counts["events"])
	}
}
