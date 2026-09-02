package postgres

import (
	"context"
	"fmt"
	"time"

	sq "github.com/Masterminds/squirrel"
	"github.com/jackc/pgx/v5"

	"github.com/lasseh/taillight/internal/model"
)

const (
	feedNetlog = model.AnalysisFeedNetlog
	feedSrvlog = model.AnalysisFeedSrvlog
)

// analysisTableName returns the events table for the given syslog feed.
// Any other feed, applog included, returns "" so the query fails on invalid
// SQL instead of silently reading srvlog_events under the wrong label; the
// applog feed has its own queries in analysis_applog_store.go.
func analysisTableName(feed string) string {
	switch feed {
	case feedNetlog:
		return "netlog_events"
	case feedSrvlog:
		return "srvlog_events"
	default:
		return ""
	}
}

// scopedSource returns the SQL FROM expression for the analyzer's source,
// pre-filtered by hostname when the scope carries a host filter. When the
// scope is unrestricted the feed's events table is returned as-is; when it
// carries hostnames, the table is wrapped in a subquery that applies
// `WHERE hostname = ANY($hostsParam)`.
//
// The caller is responsible for appending scope.Hosts to its query args at
// position hostsParam — the helper only owns the SQL fragment, not the args
// list. Param-number bookkeeping stays with each query so adding the host
// filter never shifts any existing $N indices in WHERE/ORDER/LIMIT.
//
// cols must include "hostname" (the wrapping subquery references it) when
// the scope is non-empty. Existing call sites already project hostname for
// the scoped tables; nothing new is required of the schema.
func scopedSource(scope model.AnalysisScope, cols string, hostsParam int) string {
	base := analysisTableName(scope.Feed)
	if scope.IsAllHosts() {
		return base
	}
	return fmt.Sprintf("(SELECT %s FROM %s WHERE hostname = ANY($%d)) AS scoped", cols, base, hostsParam)
}

// appendHostsArg returns args extended with the scope's host list when the
// scope is non-empty. Pairs with scopedSource so the WHERE-clause parameter
// always lines up with what scopedSource embedded.
func appendHostsArg(args []any, scope model.AnalysisScope) []any {
	if scope.IsAllHosts() {
		return args
	}
	return append(args, scope.Hosts)
}

// eventKeyExpr returns the SQL expression that produces a stable grouping
// key for events.
//
// msgid is the RFC 5424 MSGID field. Juniper netlog rows usually carry a
// named code (RTPERF_CPU_THRESHOLD_EXCEEDED, RPD_MPLS_LSP_CHANGE, …), but
// senders that omit MSGID emit the RFC 5424 NILVALUE "-" on the wire and
// rsyslog stores that literal "-". The double NULLIF treats both the
// empty string and "-" as missing so those rows fall through to
// msg_pattern, a trigger-computed template with numbers/IPs replaced
// (see trg_*_msg_pattern in migrations 2 and 3), instead of all
// collapsing into a single "-" bucket that the LLM then narrates as
// "generic syslog messages".
//
// RFC 3164 srvlog rows always have an empty msgid and rely on the same
// fallback; using one expression for every feed keeps SQL generation
// predictable.
func eventKeyExpr(_ string) string {
	return "COALESCE(NULLIF(NULLIF(msgid, ''), '-'), msg_pattern)"
}

// GetTopMsgIDs returns the top event signatures by count since the given
// time, with per-severity breakdowns. The "event signature" is msgid when
// present and msg_pattern (a normalized message template) otherwise — see
// eventKeyExpr for the rationale. The scope parameter selects which feed
// and (optionally) which hosts to query.
func (s *Store) GetTopMsgIDs(ctx context.Context, scope model.AnalysisScope, since time.Time, limit int) ([]model.MsgIDCount, error) {
	// Three sub-queries below: top, severity breakdown, host distribution.
	// Each tracks its own param indices because host scope is wrapped at the
	// source level (a subquery around the FROM), not added to each WHERE.
	source := scopedSource(scope, "received_at, hostname, msgid, msg_pattern, severity", 3)
	keyExpr := eventKeyExpr(scope.Feed)

	// Top by total count. Filter out empty keys defensively — message is
	// NOT NULL on both event tables so msg_pattern is almost always
	// populated, but a whitespace-only message could yield "".
	topQuery := fmt.Sprintf(`
		SELECT %s AS event_key, count(*) AS cnt
		FROM %s
		WHERE received_at >= $1 AND %s <> ''
		GROUP BY event_key
		ORDER BY cnt DESC
		LIMIT $2`, keyExpr, source, keyExpr)

	rows, err := s.pool.Query(ctx, topQuery, appendHostsArg([]any{since, limit}, scope)...)
	if err != nil {
		return nil, fmt.Errorf("top msgids query: %w", err)
	}
	defer rows.Close()

	var results []model.MsgIDCount
	keyIndex := make(map[string]int)
	for rows.Next() {
		var mc model.MsgIDCount
		if err := rows.Scan(&mc.MsgID, &mc.Count); err != nil {
			return nil, fmt.Errorf("scan top msgid: %w", err)
		}
		mc.SeverityCounts = make(map[int]int64)
		keyIndex[mc.MsgID] = len(results)
		results = append(results, mc)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("top msgids rows: %w", err)
	}

	if len(results) == 0 {
		return results, nil
	}

	// Per-severity breakdown for the same keys. We re-derive the key inline
	// rather than passing back a list, so the filter is just a single
	// comparison against the precomputed top list via ANY($3).
	keys := make([]string, len(results))
	for i, mc := range results {
		keys[i] = mc.MsgID
	}

	sevQuery := fmt.Sprintf(`
		SELECT %s AS event_key, severity, count(*) AS cnt
		FROM %s
		WHERE received_at >= $1 AND %s = ANY($2)
		GROUP BY event_key, severity`, keyExpr, source, keyExpr)

	sevRows, err := s.pool.Query(ctx, sevQuery, appendHostsArg([]any{since, keys}, scope)...)
	if err != nil {
		return nil, fmt.Errorf("severity breakdown query: %w", err)
	}
	defer sevRows.Close()

	for sevRows.Next() {
		var key string
		var sev int
		var cnt int64
		if err := sevRows.Scan(&key, &sev, &cnt); err != nil {
			return nil, fmt.Errorf("scan severity breakdown: %w", err)
		}
		if idx, ok := keyIndex[key]; ok {
			results[idx].SeverityCounts[sev] = cnt
		}
	}
	if err := sevRows.Err(); err != nil {
		return nil, fmt.Errorf("severity breakdown rows: %w", err)
	}

	// Host distribution per key: distinct host count + the top
	// topHostsPerMsgID contributors. The window function gives us both
	// in one pass so we don't issue two more round trips per signature.
	// Hosts param is $4 here (existing args: since, keys, topHostsPerMsgID).
	hostSource := scopedSource(scope, "received_at, hostname, msgid, msg_pattern", 4)
	hostQuery := fmt.Sprintf(`
		WITH per_host AS (
			SELECT %s AS event_key, hostname, count(*) AS cnt
			FROM %s
			WHERE received_at >= $1 AND %s = ANY($2)
			GROUP BY event_key, hostname
		), ranked AS (
			SELECT event_key, hostname, cnt,
			       ROW_NUMBER() OVER (PARTITION BY event_key ORDER BY cnt DESC) AS rn,
			       COUNT(*) OVER (PARTITION BY event_key) AS host_count
			FROM per_host
		)
		SELECT event_key, hostname, cnt, host_count
		FROM ranked
		WHERE rn <= $3
		ORDER BY event_key, rn`, keyExpr, hostSource, keyExpr)

	hostRows, err := s.pool.Query(ctx, hostQuery, appendHostsArg([]any{since, keys, topHostsPerMsgID}, scope)...)
	if err != nil {
		return nil, fmt.Errorf("msgid host distribution query: %w", err)
	}
	defer hostRows.Close()

	for hostRows.Next() {
		var key, hostname string
		var cnt int64
		var hostCount int
		if err := hostRows.Scan(&key, &hostname, &cnt, &hostCount); err != nil {
			return nil, fmt.Errorf("scan msgid host distribution: %w", err)
		}
		if idx, ok := keyIndex[key]; ok {
			results[idx].HostCount = hostCount
			results[idx].TopHosts = append(results[idx].TopHosts, model.HostCount{
				Hostname: hostname,
				Count:    cnt,
			})
		}
	}
	if err := hostRows.Err(); err != nil {
		return nil, fmt.Errorf("msgid host distribution rows: %w", err)
	}

	return results, nil
}

// topHostsPerMsgID is the number of top contributing hosts attached to
// each top event signature. Three is enough to distinguish single-host /
// pair / cluster patterns without bloating the prompt.
const topHostsPerMsgID = 3

// eventClusterLimit bounds how many cross-host clusters we surface to the
// model. Eight is plenty to anchor a Correlations section; sending many
// more invites the model to pad the section with low-signal entries (and
// occasionally repeat them verbatim).
const eventClusterLimit = 8

// GetSeverityComparison compares current period severity counts against baseline daily average.
// The scope parameter selects which feed and (optionally) which hosts to query.
// The baseline is filtered by the same host scope as the current window so
// percentage comparisons stay like-vs-like under a narrow scope.
func (s *Store) GetSeverityComparison(ctx context.Context, scope model.AnalysisScope, currentSince, baselineSince time.Time) (model.SeverityComparison, error) {
	table := analysisTableName(scope.Feed)

	// Current period counts.
	curBuilder := psq.
		Select("severity", "count(*) AS cnt").
		From(table).
		Where(sq.GtOrEq{"received_at": currentSince}).
		GroupBy("severity").
		OrderBy("severity")
	if !scope.IsAllHosts() {
		curBuilder = curBuilder.Where(sq.Eq{"hostname": scope.Hosts})
	}
	curQuery, curArgs, err := curBuilder.ToSql()
	if err != nil {
		return model.SeverityComparison{}, fmt.Errorf("build current severity query: %w", err)
	}

	curRows, err := s.pool.Query(ctx, curQuery, curArgs...)
	if err != nil {
		return model.SeverityComparison{}, fmt.Errorf("current severity query: %w", err)
	}
	defer curRows.Close()

	currentCounts := make(map[int]int64)
	for curRows.Next() {
		var sev int
		var cnt int64
		if err := curRows.Scan(&sev, &cnt); err != nil {
			return model.SeverityComparison{}, fmt.Errorf("scan current severity: %w", err)
		}
		currentCounts[sev] = cnt
	}
	if err := curRows.Err(); err != nil {
		return model.SeverityComparison{}, fmt.Errorf("current severity rows: %w", err)
	}

	// Baseline: daily average over 7 days before current period.
	baseBuilder := psq.
		Select("severity", "count(*) AS cnt").
		From(table).
		Where(sq.GtOrEq{"received_at": baselineSince}).
		Where(sq.Lt{"received_at": currentSince}).
		GroupBy("severity").
		OrderBy("severity")
	if !scope.IsAllHosts() {
		baseBuilder = baseBuilder.Where(sq.Eq{"hostname": scope.Hosts})
	}
	baseQuery, baseArgs, err := baseBuilder.ToSql()
	if err != nil {
		return model.SeverityComparison{}, fmt.Errorf("build baseline severity query: %w", err)
	}

	baseRows, err := s.pool.Query(ctx, baseQuery, baseArgs...)
	if err != nil {
		return model.SeverityComparison{}, fmt.Errorf("baseline severity query: %w", err)
	}
	defer baseRows.Close()

	baselineDays := currentSince.Sub(baselineSince).Hours() / 24
	if baselineDays < 1 {
		baselineDays = 1
	}

	baselineCounts := make(map[int]int64)
	for baseRows.Next() {
		var sev int
		var cnt int64
		if err := baseRows.Scan(&sev, &cnt); err != nil {
			return model.SeverityComparison{}, fmt.Errorf("scan baseline severity: %w", err)
		}
		baselineCounts[sev] = cnt
	}
	if err := baseRows.Err(); err != nil {
		return model.SeverityComparison{}, fmt.Errorf("baseline severity rows: %w", err)
	}

	// Build comparison for all observed severities.
	sevSet := make(map[int]struct{})
	for sev := range currentCounts {
		sevSet[sev] = struct{}{}
	}
	for sev := range baselineCounts {
		sevSet[sev] = struct{}{}
	}

	var levels []model.SeverityLevelComparison
	for sev := range sevSet {
		cur := currentCounts[sev]
		avg := float64(baselineCounts[sev]) / baselineDays

		var changePct float64
		if avg > 0 {
			changePct = (float64(cur) - avg) / avg * 100
		}

		levels = append(levels, model.SeverityLevelComparison{
			Severity:    sev,
			Label:       model.SeverityLabel(sev),
			Current:     float64(cur),
			BaselineAvg: avg,
			ChangePct:   changePct,
		})
	}

	return model.SeverityComparison{Levels: levels}, nil
}

// Never wrap an event table in a CTE that more than one part of the query
// reads from. Postgres inlines a CTE only when it is referenced exactly once;
// a second reference makes it MATERIALIZED, which is an optimization fence.
// The `received_at >= $1` predicate then never reaches the scan, so the
// planner loses chunk exclusion and reads — and decompresses — every chunk in
// the hypertable instead of the two or three the window covers.
//
// That is not a micro-optimization. It took netlog's daily analysis from 4ms
// to over the 60s statement_timeout once the table had grown, and every run
// failed for 46 days straight. Repeat the source in each scan, with its own
// time predicate, so both reach idx_*_severity_received. The queries below
// are built by dedicated functions purely so tests can assert this without a
// database — see TestTopErrorHostsQueryHasNoCTEFence.

// topErrorHostsQuery builds the GetTopErrorHosts SQL. host_counts stays a CTE
// because it is read exactly once (and so gets inlined); the event source is
// repeated in the LATERAL rather than shared, per the note above.
func topErrorHostsQuery(scope model.AnalysisScope) string {
	// Hosts param is $3 (existing args: since, limit). The source embeds $3
	// under a host scope, so it appears twice in the SQL while
	// appendHostsArg still supplies it once — reusing a parameter across
	// scans is fine.
	source := scopedSource(scope, "received_at, hostname, severity, msgid, msg_pattern", 3)
	keyExpr := eventKeyExpr(scope.Feed)

	return fmt.Sprintf(`
		WITH host_counts AS (
			SELECT hostname, count(*) AS cnt
			FROM %s
			WHERE received_at >= $1 AND severity <= 3
			GROUP BY hostname
			ORDER BY cnt DESC
			LIMIT $2
		)
		SELECT hc.hostname, hc.cnt, tm.event_key AS top_msgid
		FROM host_counts hc
		LEFT JOIN LATERAL (
			SELECT %s AS event_key
			FROM %s
			WHERE hostname = hc.hostname AND received_at >= $1 AND severity <= 3 AND %s <> ''
			GROUP BY event_key
			ORDER BY count(*) DESC
			LIMIT 1
		) tm ON true
		ORDER BY hc.cnt DESC`, source, keyExpr, source, keyExpr)
}

// GetTopErrorHosts returns hosts with the most errors (severity <= 3).
// The scope parameter selects which feed and (optionally) which hosts to
// query. Note: the analyzer skips this query when the scope already
// restricts to specific hosts (the aggregation is degenerate), but the
// method honors the filter anyway for callers that invoke it directly.
// The "top msgid" per host is the most common event signature (msgid when
// present, otherwise msg_pattern) — see eventKeyExpr.
func (s *Store) GetTopErrorHosts(ctx context.Context, scope model.AnalysisScope, since time.Time, limit int) ([]model.HostErrorCount, error) {
	rows, err := s.pool.Query(ctx, topErrorHostsQuery(scope), appendHostsArg([]any{since, limit}, scope)...)
	if err != nil {
		return nil, fmt.Errorf("top error hosts query: %w", err)
	}

	results, err := pgx.CollectRows(rows, func(row pgx.CollectableRow) (model.HostErrorCount, error) {
		var h model.HostErrorCount
		var topMsgID *string
		if err := row.Scan(&h.Hostname, &h.Count, &topMsgID); err != nil {
			return h, err
		}
		if topMsgID != nil {
			h.TopMsgID = *topMsgID
		}
		return h, nil
	})
	if err != nil {
		return nil, fmt.Errorf("scan error host: %w", err)
	}
	return results, nil
}

// newMsgIDsQuery builds the GetNewMsgIDs SQL as a set difference between the
// current window and the baseline window.
//
// The obvious shape — one CTE of (event_key, received_at) read by both a
// current-window SELECT and a correlated NOT EXISTS — carries the fence
// described above, and un-fencing it alone would not be enough: NOT EXISTS
// correlates on a computed event_key that no index can serve, so it degrades
// into a self-join over the whole window. EXCEPT lets each side aggregate its
// own window independently, with chunk exclusion intact on both.
//
// EXCEPT is EXCEPT DISTINCT, which preserves the original SELECT DISTINCT.
// It also treats NULL as equal to NULL where the old `base.event_key =
// curr.event_key` did not, but eventKeyExpr can never yield NULL: msg_pattern
// is NOT NULL DEFAULT ” on both event tables, so the COALESCE always lands on
// a non-null column. The baseline side needs no `<> ”` filter — it can only
// match a current-side key, which is already non-empty.
func newMsgIDsQuery(scope model.AnalysisScope) string {
	// Hosts param is $3 (existing args: since, baselineSince).
	source := scopedSource(scope, "received_at, hostname, msgid, msg_pattern", 3)
	keyExpr := eventKeyExpr(scope.Feed)

	return fmt.Sprintf(`
		SELECT %s AS event_key
		FROM %s
		WHERE received_at >= $1 AND %s <> ''
		EXCEPT
		SELECT %s
		FROM %s
		WHERE received_at >= $2 AND received_at < $1
		ORDER BY event_key`, keyExpr, source, keyExpr, keyExpr, source)
}

// GetNewMsgIDs returns event signatures seen in the current period but not in
// the baseline period. The scope parameter selects which feed and (optionally)
// which hosts to query; both the current and baseline windows are filtered by
// the same host scope. The signature is msgid when present and msg_pattern
// otherwise — see eventKeyExpr.
func (s *Store) GetNewMsgIDs(ctx context.Context, scope model.AnalysisScope, since, baselineSince time.Time) ([]string, error) {
	rows, err := s.pool.Query(ctx, newMsgIDsQuery(scope), appendHostsArg([]any{since, baselineSince}, scope)...)
	if err != nil {
		return nil, fmt.Errorf("new msgids query: %w", err)
	}

	msgids, err := pgx.CollectRows(rows, pgx.RowTo[string])
	if err != nil {
		return nil, fmt.Errorf("scan new msgid: %w", err)
	}
	return msgids, nil
}

// GetEventClusters returns time windows where events from multiple hosts overlap.
// The scope parameter selects which feed and (optionally) which hosts to
// query. Note: the analyzer skips this query when the scope already restricts
// to specific hosts ("clusters across hosts" is degenerate under a narrow
// scope), but the method honors the filter for direct callers.
// Cluster signatures use the same msgid-or-msg_pattern fallback as the rest
// of the analyzer (see eventKeyExpr), so srvlog clusters surface even when
// the underlying rows have no RFC 5424 MSGID.
func (s *Store) GetEventClusters(ctx context.Context, scope model.AnalysisScope, since time.Time, windowMinutes int) ([]model.EventCluster, error) {
	// Hosts param is $3 (existing args: since, interval).
	source := scopedSource(scope, "received_at, hostname, msgid, msg_pattern", 3)
	keyExpr := eventKeyExpr(scope.Feed)

	query := fmt.Sprintf(`
		SELECT time_bucket($2::interval, received_at) AS bucket,
		       array_agg(DISTINCT hostname) AS hosts,
		       array_agg(DISTINCT %s) FILTER (WHERE %s <> '') AS msgids,
		       count(*) AS total
		FROM %s
		WHERE received_at >= $1
		GROUP BY bucket
		HAVING count(DISTINCT hostname) > 1
		ORDER BY total DESC
		LIMIT %d`, keyExpr, keyExpr, source, eventClusterLimit)

	interval := fmt.Sprintf("%d minutes", windowMinutes)
	rows, err := s.pool.Query(ctx, query, appendHostsArg([]any{since, interval}, scope)...)
	if err != nil {
		return nil, fmt.Errorf("event clusters query: %w", err)
	}

	clusters, err := pgx.CollectRows(rows, func(row pgx.CollectableRow) (model.EventCluster, error) {
		var c model.EventCluster
		err := row.Scan(&c.Bucket, &c.Hosts, &c.MsgIDs, &c.Total)
		return c, err
	})
	if err != nil {
		return nil, fmt.Errorf("scan event cluster: %w", err)
	}
	return clusters, nil
}

// sampleMessageMaxLen caps the message text stored per sample. Picked to
// give the model enough context to reason about a single line without
// blowing the prompt budget when 25 top signatures each carry samples.
const sampleMessageMaxLen = 300

// GetMsgIDSamples returns up to perKeyLimit recent representative messages
// per event signature in keys. Empty keys input returns an empty map.
//
// "Recent" means ORDER BY received_at DESC — the model gets the freshest
// message for each signature, which is usually the most diagnostically
// useful one. Message text is left-truncated to sampleMessageMaxLen.
//
// The returned map is keyed by event signature (same string the caller
// passed in keys). Signatures with no rows in the window are simply absent.
func (s *Store) GetMsgIDSamples(ctx context.Context, scope model.AnalysisScope, since time.Time, keys []string, perKeyLimit int) (map[string][]model.SampleMessage, error) {
	out := make(map[string][]model.SampleMessage)
	if len(keys) == 0 || perKeyLimit <= 0 {
		return out, nil
	}

	// Hosts param is $4 (existing args: since, keys, perKeyLimit).
	source := scopedSource(scope, "received_at, hostname, severity, message, msgid, msg_pattern", 4)
	keyExpr := eventKeyExpr(scope.Feed)

	query := fmt.Sprintf(`
		WITH tagged AS (
			SELECT %s AS event_key,
			       hostname, received_at, severity,
			       LEFT(message, %d) AS message
			FROM %s
			WHERE received_at >= $1 AND %s = ANY($2)
		), ranked AS (
			SELECT event_key, hostname, received_at, severity, message,
			       ROW_NUMBER() OVER (PARTITION BY event_key ORDER BY received_at DESC) AS rn
			FROM tagged
		)
		SELECT event_key, hostname, received_at, severity, message
		FROM ranked
		WHERE rn <= $3
		ORDER BY event_key, rn`, keyExpr, sampleMessageMaxLen, source, keyExpr)

	rows, err := s.pool.Query(ctx, query, appendHostsArg([]any{since, keys, perKeyLimit}, scope)...)
	if err != nil {
		return nil, fmt.Errorf("msgid samples query: %w", err)
	}
	defer rows.Close()

	for rows.Next() {
		var key string
		var sm model.SampleMessage
		if err := rows.Scan(&key, &sm.Hostname, &sm.ReceivedAt, &sm.Severity, &sm.Message); err != nil {
			return nil, fmt.Errorf("scan msgid sample: %w", err)
		}
		out[key] = append(out[key], sm)
	}
	return out, rows.Err()
}

// GetTopPrograms returns the top srvlog programnames by total count with an
// errors (severity ≤ 3) breakdown alongside. Only meaningful for srvlog;
// netlog rows have no programname so this returns empty. The caller is
// expected to skip rendering when feed is netlog.
func (s *Store) GetTopPrograms(ctx context.Context, scope model.AnalysisScope, since time.Time, limit int) ([]model.ProgramCount, error) {
	// netlog_events doesn't carry programname — skip the union and just
	// return empty rather than emitting a no-op query.
	if scope.Feed == feedNetlog {
		return nil, nil
	}
	// Hosts param is $3 (existing args: since, limit). hostname is projected
	// so the optional host filter has a column to reference.
	source := scopedSource(scope, "received_at, hostname, programname, severity", 3)

	topQuery := fmt.Sprintf(`
		SELECT programname,
		       count(*) AS cnt,
		       count(*) FILTER (WHERE severity <= 3) AS err_cnt
		FROM %s
		WHERE received_at >= $1 AND programname <> ''
		GROUP BY programname
		ORDER BY cnt DESC
		LIMIT $2`, source)

	rows, err := s.pool.Query(ctx, topQuery, appendHostsArg([]any{since, limit}, scope)...)
	if err != nil {
		return nil, fmt.Errorf("top programs query: %w", err)
	}
	defer rows.Close()

	var results []model.ProgramCount
	progIndex := make(map[string]int)
	for rows.Next() {
		var pc model.ProgramCount
		if err := rows.Scan(&pc.Programname, &pc.Count, &pc.ErrorCount); err != nil {
			return nil, fmt.Errorf("scan top program: %w", err)
		}
		pc.SeverityCounts = make(map[int]int64)
		progIndex[pc.Programname] = len(results)
		results = append(results, pc)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("top programs rows: %w", err)
	}

	if len(results) == 0 {
		return results, nil
	}

	names := make([]string, len(results))
	for i, pc := range results {
		names[i] = pc.Programname
	}

	sevQuery := fmt.Sprintf(`
		SELECT programname, severity, count(*) AS cnt
		FROM %s
		WHERE received_at >= $1 AND programname = ANY($2)
		GROUP BY programname, severity`, source)

	sevRows, err := s.pool.Query(ctx, sevQuery, appendHostsArg([]any{since, names}, scope)...)
	if err != nil {
		return nil, fmt.Errorf("program severity breakdown query: %w", err)
	}
	defer sevRows.Close()

	for sevRows.Next() {
		var name string
		var sev int
		var cnt int64
		if err := sevRows.Scan(&name, &sev, &cnt); err != nil {
			return nil, fmt.Errorf("scan program severity breakdown: %w", err)
		}
		if idx, ok := progIndex[name]; ok {
			results[idx].SeverityCounts[sev] = cnt
		}
	}
	return results, sevRows.Err()
}

// GetTopFacilities returns the top syslog facilities by total count with an
// errors (severity ≤ 3) breakdown. Cheap (facility is indexed) and useful
// for surfacing auth/authpriv activity as a first-class signal.
func (s *Store) GetTopFacilities(ctx context.Context, scope model.AnalysisScope, since time.Time, limit int) ([]model.FacilityCount, error) {
	if scope.Feed == feedNetlog {
		return nil, nil
	}
	// Hosts param is $3 (existing args: since, limit). hostname is projected
	// so the optional host filter has a column to reference.
	source := scopedSource(scope, "received_at, hostname, facility, severity", 3)

	query := fmt.Sprintf(`
		SELECT facility,
		       count(*) AS cnt,
		       count(*) FILTER (WHERE severity <= 3) AS err_cnt
		FROM %s
		WHERE received_at >= $1
		GROUP BY facility
		ORDER BY cnt DESC
		LIMIT $2`, source)

	rows, err := s.pool.Query(ctx, query, appendHostsArg([]any{since, limit}, scope)...)
	if err != nil {
		return nil, fmt.Errorf("top facilities query: %w", err)
	}

	results, err := pgx.CollectRows(rows, func(row pgx.CollectableRow) (model.FacilityCount, error) {
		var fc model.FacilityCount
		if err := row.Scan(&fc.Facility, &fc.Count, &fc.ErrorCount); err != nil {
			return fc, err
		}
		fc.Label = model.FacilityLabel(fc.Facility)
		return fc, nil
	})
	if err != nil {
		return nil, fmt.Errorf("scan top facility: %w", err)
	}
	return results, nil
}

// GetVolumeTimeline returns event counts bucketed across the analysis
// period, with errors (severity ≤ 3) called out separately. bucketMinutes
// controls bucket granularity.
//
// For buckets ≥ 60 minutes the query reads from the continuous aggregate
// (srvlog_summary_hourly / netlog_summary_hourly) since those views are
// already pre-rolled per (bucket, hostname, severity); for finer buckets
// the function falls back to the raw event tables.
func (s *Store) GetVolumeTimeline(ctx context.Context, scope model.AnalysisScope, since, until time.Time, bucketMinutes int) ([]model.AnalysisVolumeBucket, error) {
	if bucketMinutes <= 0 {
		return nil, nil
	}
	interval := fmt.Sprintf("%d minutes", bucketMinutes)

	var query string
	switch {
	case bucketMinutes >= 60 && scope.IsAllHosts():
		// Hourly continuous aggregates are pre-rolled per (bucket, severity)
		// and do NOT carry hostname; only safe to use when the report has
		// no host filter. Scoped reports fall through to the raw path.
		caSource := analysisAggregateSource(scope.Feed)
		query = fmt.Sprintf(`
			SELECT time_bucket($1::interval, bucket) AS b,
			       SUM(cnt) AS total,
			       SUM(cnt) FILTER (WHERE severity <= 3) AS err_cnt
			FROM %s
			WHERE bucket >= $2 AND bucket < $3
			GROUP BY b
			ORDER BY b`, caSource)
	default:
		// Hosts param is $4 (existing args: interval, since, until).
		source := scopedSource(scope, "received_at, hostname, severity", 4)
		query = fmt.Sprintf(`
			SELECT time_bucket($1::interval, received_at) AS b,
			       count(*) AS total,
			       count(*) FILTER (WHERE severity <= 3) AS err_cnt
			FROM %s
			WHERE received_at >= $2 AND received_at < $3
			GROUP BY b
			ORDER BY b`, source)
	}

	rows, err := s.pool.Query(ctx, query, appendHostsArg([]any{interval, since, until}, scope)...)
	if err != nil {
		return nil, fmt.Errorf("volume timeline query: %w", err)
	}

	results, err := pgx.CollectRows(rows, func(row pgx.CollectableRow) (model.AnalysisVolumeBucket, error) {
		var b model.AnalysisVolumeBucket
		err := row.Scan(&b.Bucket, &b.Total, &b.ErrorCount)
		return b, err
	})
	if err != nil {
		return nil, fmt.Errorf("scan volume bucket: %w", err)
	}
	return results, nil
}

// analysisAggregateSource returns the hourly continuous-aggregate source
// for the given syslog feed; "" for any other, like analysisTableName.
func analysisAggregateSource(feed string) string {
	switch feed {
	case feedNetlog:
		return "netlog_summary_hourly"
	case feedSrvlog:
		return "srvlog_summary_hourly"
	default:
		return ""
	}
}

// ListAnalysisHosts returns distinct hostnames from the meta caches for the
// given feed, sorted alphabetically. Used by the analysis handler to validate
// caller-supplied host scopes before enqueueing a report.
//
// "srvlog" and "netlog" return their respective meta caches; anything else
// returns an empty slice without error so callers don't need to special-case
// feed validation that already happened upstream.
//
// Delegates to the existing ListSrvlogHosts / ListNetlogHosts helpers, which
// know the meta-cache schema is (column_name, value) rather than a bare
// hostname column — writing the query inline here was the source of an
// earlier "failed to validate host scope" 500.
func (s *Store) ListAnalysisHosts(ctx context.Context, feed string) ([]string, error) {
	switch feed {
	case feedSrvlog:
		return s.ListSrvlogHosts(ctx)
	case feedNetlog:
		return s.ListNetlogHosts(ctx)
	default:
		return nil, nil
	}
}

// ListAnalysisHostEntries returns hostname + last_seen rows for the analysis
// picker. Hosts are sourced from the meta cache (every hostname that has
// ever logged on the feed); last_seen comes from a LEFT JOIN against the
// hourly continuous aggregate so a host appears even when it has no recent
// activity (LastSeen is nil in that case).
func (s *Store) ListAnalysisHostEntries(ctx context.Context, feed string) ([]model.AnalysisHostEntry, error) {
	var query string
	switch feed {
	case feedSrvlog:
		query = `
			SELECT mc.value AS hostname, ls.last_seen
			FROM srvlog_meta_cache mc
			LEFT JOIN (
				SELECT hostname, MAX(bucket) AS last_seen
				FROM srvlog_summary_hourly
				GROUP BY hostname
			) ls ON ls.hostname = mc.value
			WHERE mc.column_name = 'hostname'
			ORDER BY mc.value`
	case feedNetlog:
		query = `
			SELECT mc.value AS hostname, ls.last_seen
			FROM netlog_meta_cache mc
			LEFT JOIN (
				SELECT hostname, MAX(bucket) AS last_seen
				FROM netlog_summary_hourly
				GROUP BY hostname
			) ls ON ls.hostname = mc.value
			WHERE mc.column_name = 'hostname'
			ORDER BY mc.value`
	default:
		return nil, nil
	}

	rows, err := s.pool.Query(ctx, query)
	if err != nil {
		return nil, fmt.Errorf("list analysis host entries (%s): %w", feed, err)
	}

	entries, err := pgx.CollectRows(rows, func(row pgx.CollectableRow) (model.AnalysisHostEntry, error) {
		var e model.AnalysisHostEntry
		err := row.Scan(&e.Hostname, &e.LastSeen)
		return e, err
	})
	if err != nil {
		return nil, fmt.Errorf("scan analysis host entry: %w", err)
	}
	return entries, nil
}

// LookupJuniperRefs returns Juniper reference data for the given msgid names.
func (s *Store) LookupJuniperRefs(ctx context.Context, names []string) (map[string]model.JuniperNetlogRef, error) {
	if len(names) == 0 {
		return nil, nil
	}

	query, args, err := psq.
		Select("name", "description", "cause", "action").
		From("juniper_netlog_ref").
		Where(sq.Eq{"name": names}).
		ToSql()
	if err != nil {
		return nil, fmt.Errorf("build juniper refs query: %w", err)
	}

	rows, err := s.pool.Query(ctx, query, args...)
	if err != nil {
		return nil, fmt.Errorf("juniper refs query: %w", err)
	}
	defer rows.Close()

	refs := make(map[string]model.JuniperNetlogRef)
	for rows.Next() {
		var r model.JuniperNetlogRef
		if err := rows.Scan(&r.Name, &r.Description, &r.Cause, &r.Action); err != nil {
			return nil, fmt.Errorf("scan juniper ref: %w", err)
		}
		// Keep first match per name (multiple OS variants may exist).
		if _, exists := refs[r.Name]; !exists {
			refs[r.Name] = r
		}
	}
	return refs, rows.Err()
}
