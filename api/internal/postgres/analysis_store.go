package postgres

import (
	"context"
	"errors"
	"fmt"
	"time"

	sq "github.com/Masterminds/squirrel"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"

	"github.com/lasseh/taillight/internal/model"
)

// ErrDuplicateActiveReport is returned by InsertPendingReport when a pending
// or running report already exists for the same (feed, period_end). The
// underlying database guard is a partial unique index.
var ErrDuplicateActiveReport = errors.New("analysis report already active for feed/period")

// pgUniqueViolation is the SQLSTATE code returned by Postgres on a unique
// constraint or unique-index violation.
const pgUniqueViolation = "23505"

const (
	// feedAll indicates analysis should query both srvlog and netlog tables.
	feedAll    = "all"
	feedNetlog = "netlog"
	feedSrvlog = "srvlog"
)

// analysisTableName returns the table name for the given feed.
// Valid feeds: "srvlog", "netlog". For "all", use analysisUnionSource instead.
func analysisTableName(feed string) string {
	switch feed {
	case feedNetlog:
		return "netlog_events"
	case feedSrvlog:
		return "srvlog_events"
	default:
		return "srvlog_events"
	}
}

// analysisUnionSource returns a SQL subquery expression that unions
// the given columns from both srvlog_events and netlog_events.
// The result can be used as a FROM source: `FROM (... ) AS combined`.
func analysisUnionSource(columns string) string {
	return fmt.Sprintf(
		"(SELECT %s FROM srvlog_events UNION ALL SELECT %s FROM netlog_events) AS combined",
		columns, columns,
	)
}

// analysisSource picks between a single table and the unioned subquery
// depending on feed. cols is the projected column list — only used for the
// union case but accepted uniformly so call sites stay symmetric.
func analysisSource(feed, cols string) string {
	if feed == feedAll {
		return analysisUnionSource(cols)
	}
	return analysisTableName(feed)
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
// eventKeyExpr for the rationale. The feed parameter selects which table(s)
// to query: "srvlog", "netlog", or "all".
func (s *Store) GetTopMsgIDs(ctx context.Context, feed string, since time.Time, limit int) ([]model.MsgIDCount, error) {
	source := analysisSource(feed, "received_at, msgid, msg_pattern, severity")
	keyExpr := eventKeyExpr(feed)

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

	rows, err := s.pool.Query(ctx, topQuery, since, limit)
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

	sevRows, err := s.pool.Query(ctx, sevQuery, since, keys)
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
	hostSource := analysisSource(feed, "received_at, hostname, msgid, msg_pattern")
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

	hostRows, err := s.pool.Query(ctx, hostQuery, since, keys, topHostsPerMsgID)
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

// GetSeverityComparison compares current period severity counts against baseline daily average.
// The feed parameter selects which table(s) to query: "srvlog", "netlog", or "all".
func (s *Store) GetSeverityComparison(ctx context.Context, feed string, currentSince, baselineSince time.Time) (model.SeverityComparison, error) {
	table := analysisSource(feed, "received_at, severity")

	// Current period counts.
	curQuery, curArgs, err := psq.
		Select("severity", "count(*) AS cnt").
		From(table).
		Where(sq.GtOrEq{"received_at": currentSince}).
		GroupBy("severity").
		OrderBy("severity").
		ToSql()
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
	baseQuery, baseArgs, err := psq.
		Select("severity", "count(*) AS cnt").
		From(table).
		Where(sq.GtOrEq{"received_at": baselineSince}).
		Where(sq.Lt{"received_at": currentSince}).
		GroupBy("severity").
		OrderBy("severity").
		ToSql()
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

// GetTopErrorHosts returns hosts with the most errors (severity <= 3).
// The feed parameter selects which table(s) to query: "srvlog", "netlog", or "all".
// The "top msgid" per host is the most common event signature (msgid when
// present, otherwise msg_pattern) — see eventKeyExpr.
func (s *Store) GetTopErrorHosts(ctx context.Context, feed string, since time.Time, limit int) ([]model.HostErrorCount, error) {
	source := analysisSource(feed, "received_at, hostname, severity, msgid, msg_pattern")
	keyExpr := eventKeyExpr(feed)

	query := fmt.Sprintf(`
		WITH events AS (
			SELECT * FROM %s
		), host_counts AS (
			SELECT hostname, count(*) AS cnt
			FROM events
			WHERE received_at >= $1 AND severity <= 3
			GROUP BY hostname
			ORDER BY cnt DESC
			LIMIT $2
		)
		SELECT hc.hostname, hc.cnt, tm.event_key AS top_msgid
		FROM host_counts hc
		LEFT JOIN LATERAL (
			SELECT %s AS event_key
			FROM events
			WHERE hostname = hc.hostname AND received_at >= $1 AND severity <= 3 AND %s <> ''
			GROUP BY event_key
			ORDER BY count(*) DESC
			LIMIT 1
		) tm ON true
		ORDER BY hc.cnt DESC`, source, keyExpr, keyExpr)

	rows, err := s.pool.Query(ctx, query, since, limit)
	if err != nil {
		return nil, fmt.Errorf("top error hosts query: %w", err)
	}
	defer rows.Close()

	var results []model.HostErrorCount
	for rows.Next() {
		var h model.HostErrorCount
		var topMsgID *string
		if err := rows.Scan(&h.Hostname, &h.Count, &topMsgID); err != nil {
			return nil, fmt.Errorf("scan error host: %w", err)
		}
		if topMsgID != nil {
			h.TopMsgID = *topMsgID
		}
		results = append(results, h)
	}
	return results, rows.Err()
}

// GetNewMsgIDs returns event signatures seen in the current period but not in
// the baseline period. The feed parameter selects which table(s) to query:
// "srvlog", "netlog", or "all". The signature is msgid when present and
// msg_pattern otherwise — see eventKeyExpr.
func (s *Store) GetNewMsgIDs(ctx context.Context, feed string, since, baselineSince time.Time) ([]string, error) {
	source := analysisSource(feed, "received_at, msgid, msg_pattern")
	keyExpr := eventKeyExpr(feed)

	query := fmt.Sprintf(`
		WITH events AS (
			SELECT %s AS event_key, received_at FROM %s
		)
		SELECT DISTINCT event_key
		FROM events curr
		WHERE curr.received_at >= $1 AND curr.event_key <> ''
		  AND NOT EXISTS (
		    SELECT 1 FROM events base
		    WHERE base.event_key = curr.event_key
		      AND base.received_at >= $2 AND base.received_at < $1
		      AND base.event_key <> ''
		  )
		ORDER BY event_key`, keyExpr, source)

	rows, err := s.pool.Query(ctx, query, since, baselineSince)
	if err != nil {
		return nil, fmt.Errorf("new msgids query: %w", err)
	}
	defer rows.Close()

	var msgids []string
	for rows.Next() {
		var m string
		if err := rows.Scan(&m); err != nil {
			return nil, fmt.Errorf("scan new msgid: %w", err)
		}
		msgids = append(msgids, m)
	}
	return msgids, rows.Err()
}

// GetEventClusters returns time windows where events from multiple hosts overlap.
// The feed parameter selects which table(s) to query: "srvlog", "netlog", or "all".
// Cluster signatures use the same msgid-or-msg_pattern fallback as the rest of
// the analyzer (see eventKeyExpr), so srvlog clusters surface even when the
// underlying rows have no RFC 5424 MSGID.
func (s *Store) GetEventClusters(ctx context.Context, feed string, since time.Time, windowMinutes int) ([]model.EventCluster, error) {
	source := analysisSource(feed, "received_at, hostname, msgid, msg_pattern")
	keyExpr := eventKeyExpr(feed)

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
		LIMIT 20`, keyExpr, keyExpr, source)

	interval := fmt.Sprintf("%d minutes", windowMinutes)
	rows, err := s.pool.Query(ctx, query, since, interval)
	if err != nil {
		return nil, fmt.Errorf("event clusters query: %w", err)
	}
	defer rows.Close()

	var clusters []model.EventCluster
	for rows.Next() {
		var c model.EventCluster
		if err := rows.Scan(&c.Bucket, &c.Hosts, &c.MsgIDs, &c.Total); err != nil {
			return nil, fmt.Errorf("scan event cluster: %w", err)
		}
		clusters = append(clusters, c)
	}
	return clusters, rows.Err()
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
func (s *Store) GetMsgIDSamples(ctx context.Context, feed string, since time.Time, keys []string, perKeyLimit int) (map[string][]model.SampleMessage, error) {
	out := make(map[string][]model.SampleMessage)
	if len(keys) == 0 || perKeyLimit <= 0 {
		return out, nil
	}

	source := analysisSource(feed, "received_at, hostname, severity, message, msgid, msg_pattern")
	keyExpr := eventKeyExpr(feed)

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

	rows, err := s.pool.Query(ctx, query, since, keys, perKeyLimit)
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
// errors (severity ≤ 3) breakdown alongside. Only meaningful for srvlog (and
// "all" if it contains srvlog rows); netlog rows have no programname so this
// will return empty. The caller is expected to skip rendering when feed is
// pure netlog.
func (s *Store) GetTopPrograms(ctx context.Context, feed string, since time.Time, limit int) ([]model.ProgramCount, error) {
	// netlog_events doesn't carry programname — skip the union and just
	// return empty rather than emitting a no-op query.
	if feed == feedNetlog {
		return nil, nil
	}
	source := analysisSource(feed, "received_at, programname, severity")

	topQuery := fmt.Sprintf(`
		SELECT programname,
		       count(*) AS cnt,
		       count(*) FILTER (WHERE severity <= 3) AS err_cnt
		FROM %s
		WHERE received_at >= $1 AND programname <> ''
		GROUP BY programname
		ORDER BY cnt DESC
		LIMIT $2`, source)

	rows, err := s.pool.Query(ctx, topQuery, since, limit)
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

	sevRows, err := s.pool.Query(ctx, sevQuery, since, names)
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
func (s *Store) GetTopFacilities(ctx context.Context, feed string, since time.Time, limit int) ([]model.FacilityCount, error) {
	if feed == feedNetlog {
		return nil, nil
	}
	source := analysisSource(feed, "received_at, facility, severity")

	query := fmt.Sprintf(`
		SELECT facility,
		       count(*) AS cnt,
		       count(*) FILTER (WHERE severity <= 3) AS err_cnt
		FROM %s
		WHERE received_at >= $1
		GROUP BY facility
		ORDER BY cnt DESC
		LIMIT $2`, source)

	rows, err := s.pool.Query(ctx, query, since, limit)
	if err != nil {
		return nil, fmt.Errorf("top facilities query: %w", err)
	}
	defer rows.Close()

	var results []model.FacilityCount
	for rows.Next() {
		var fc model.FacilityCount
		if err := rows.Scan(&fc.Facility, &fc.Count, &fc.ErrorCount); err != nil {
			return nil, fmt.Errorf("scan top facility: %w", err)
		}
		fc.Label = model.FacilityLabel(fc.Facility)
		results = append(results, fc)
	}
	return results, rows.Err()
}

// GetVolumeTimeline returns event counts bucketed across the analysis
// period, with errors (severity ≤ 3) called out separately. bucketMinutes
// controls bucket granularity.
//
// For buckets ≥ 60 minutes the query reads from the continuous aggregate
// (srvlog_summary_hourly / netlog_summary_hourly) since those views are
// already pre-rolled per (bucket, hostname, severity); for finer buckets
// the function falls back to the raw event tables.
func (s *Store) GetVolumeTimeline(ctx context.Context, feed string, since, until time.Time, bucketMinutes int) ([]model.AnalysisVolumeBucket, error) {
	if bucketMinutes <= 0 {
		return nil, nil
	}
	interval := fmt.Sprintf("%d minutes", bucketMinutes)

	var query string
	switch {
	case bucketMinutes >= 60:
		caSource := analysisAggregateSource(feed)
		query = fmt.Sprintf(`
			SELECT time_bucket($1::interval, bucket) AS b,
			       SUM(cnt) AS total,
			       SUM(cnt) FILTER (WHERE severity <= 3) AS err_cnt
			FROM %s
			WHERE bucket >= $2 AND bucket < $3
			GROUP BY b
			ORDER BY b`, caSource)
	default:
		source := analysisSource(feed, "received_at, severity")
		query = fmt.Sprintf(`
			SELECT time_bucket($1::interval, received_at) AS b,
			       count(*) AS total,
			       count(*) FILTER (WHERE severity <= 3) AS err_cnt
			FROM %s
			WHERE received_at >= $2 AND received_at < $3
			GROUP BY b
			ORDER BY b`, source)
	}

	rows, err := s.pool.Query(ctx, query, interval, since, until)
	if err != nil {
		return nil, fmt.Errorf("volume timeline query: %w", err)
	}
	defer rows.Close()

	var results []model.AnalysisVolumeBucket
	for rows.Next() {
		var b model.AnalysisVolumeBucket
		if err := rows.Scan(&b.Bucket, &b.Total, &b.ErrorCount); err != nil {
			return nil, fmt.Errorf("scan volume bucket: %w", err)
		}
		results = append(results, b)
	}
	return results, rows.Err()
}

// analysisAggregateSource returns the hourly continuous-aggregate source
// for the given feed. For "all", srvlog and netlog summaries are unioned.
func analysisAggregateSource(feed string) string {
	switch feed {
	case feedNetlog:
		return "netlog_summary_hourly"
	case feedSrvlog:
		return "srvlog_summary_hourly"
	case feedAll:
		return "(SELECT bucket, severity, cnt FROM srvlog_summary_hourly UNION ALL SELECT bucket, severity, cnt FROM netlog_summary_hourly) AS combined_hourly"
	default:
		return "srvlog_summary_hourly"
	}
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

// analysisReportColumns lists the columns selected for full report reads.
const analysisReportColumns = "id, slug, feed, prompt_mode, model, period_start, period_end, " +
	"report, prompt_tokens, completion_tokens, status, error, " +
	"created_at, started_at, completed_at"

// analysisReportSummaryColumns lists the columns selected for list reads.
const analysisReportSummaryColumns = "id, slug, feed, prompt_mode, model, period_start, period_end, " +
	"prompt_tokens, completion_tokens, status, " +
	"created_at, started_at, completed_at"

// BuildAnalysisSlug returns the canonical slug for a (feed, mode, periodEnd)
// triple. periodEnd is truncated to the minute in UTC before formatting. Mode
// is always included as a segment so daily/weekly/incident slugs can coexist
// for the same feed + window without collision. Empty mode defaults to "daily"
// so legacy callers don't need updating.
func BuildAnalysisSlug(feed, mode string, periodEnd time.Time) string {
	if mode == "" {
		mode = model.AnalysisModeDaily
	}
	t := periodEnd.UTC().Truncate(time.Minute)
	return fmt.Sprintf("%s-%s-%s-%s", feed, mode, t.Format("2006-01-02"), t.Format("1504"))
}

// InsertPendingReport creates a new report row in the pending state. It
// generates a slug from feed+periodEnd and resolves slug collisions with a
// numeric suffix so historical reports for the same minute can coexist.
//
// Returns ErrDuplicateActiveReport when the partial unique index
// analysis_reports_active_uniq is violated, which happens when another pending
// or running report already covers the same (feed, period_end).
func (s *Store) InsertPendingReport(ctx context.Context, r model.AnalysisReport) (model.AnalysisReport, error) {
	if r.PromptMode == "" {
		r.PromptMode = model.AnalysisModeDaily
	}
	if r.Slug == "" {
		r.Slug = BuildAnalysisSlug(r.Feed, r.PromptMode, r.PeriodEnd)
	}
	r.Status = model.AnalysisStatusPending

	// Try the natural slug first, then -2, -3, ... if another completed report
	// happens to share the same minute. Capped to avoid runaway loops.
	base := r.Slug
	for attempt := 1; attempt <= 10; attempt++ {
		slug := base
		if attempt > 1 {
			slug = fmt.Sprintf("%s-%d", base, attempt)
		}
		r.Slug = slug

		query, args, err := psq.
			Insert("analysis_reports").
			Columns("slug", "feed", "prompt_mode", "model", "period_start", "period_end", "status").
			Values(r.Slug, r.Feed, r.PromptMode, r.Model, r.PeriodStart, r.PeriodEnd, r.Status).
			Suffix("RETURNING id, created_at").
			ToSql()
		if err != nil {
			return model.AnalysisReport{}, fmt.Errorf("build insert pending report: %w", err)
		}

		err = s.pool.QueryRow(ctx, query, args...).Scan(&r.ID, &r.CreatedAt)
		if err == nil {
			return r, nil
		}

		var pgErr *pgconn.PgError
		if errors.As(err, &pgErr) && pgErr.Code == pgUniqueViolation {
			switch pgErr.ConstraintName {
			case "analysis_reports_active_uniq":
				return model.AnalysisReport{}, ErrDuplicateActiveReport
			case "analysis_reports_slug_uniq":
				continue // try next suffix
			}
		}
		return model.AnalysisReport{}, fmt.Errorf("insert pending report: %w", err)
	}
	return model.AnalysisReport{}, fmt.Errorf("insert pending report: exhausted slug suffix attempts for %q", base)
}

// MarkReportRunning flips a pending row to running and stamps started_at.
// Returns pgx.ErrNoRows if the row was deleted while queued.
func (s *Store) MarkReportRunning(ctx context.Context, id int64) error {
	tag, err := s.pool.Exec(ctx,
		`UPDATE analysis_reports
		   SET status='running', started_at=now()
		 WHERE id=$1 AND status='pending'`, id)
	if err != nil {
		return fmt.Errorf("mark report %d running: %w", id, err)
	}
	if tag.RowsAffected() == 0 {
		return pgx.ErrNoRows
	}
	return nil
}

// MarkReportCompleted writes the report body, token counts, and timestamps.
// Zero rows affected is benign — the report may have been deleted mid-flight.
func (s *Store) MarkReportCompleted(ctx context.Context, id int64, body string, promptTokens, completionTokens int) error {
	_, err := s.pool.Exec(ctx,
		`UPDATE analysis_reports
		   SET status='completed', report=$2, prompt_tokens=$3, completion_tokens=$4,
		       completed_at=now()
		 WHERE id=$1`, id, body, promptTokens, completionTokens)
	if err != nil {
		return fmt.Errorf("mark report %d completed: %w", id, err)
	}
	return nil
}

// MarkReportFailed records a short error message and completion time.
// Zero rows affected is benign for the same reason as MarkReportCompleted.
func (s *Store) MarkReportFailed(ctx context.Context, id int64, msg string) error {
	_, err := s.pool.Exec(ctx,
		`UPDATE analysis_reports
		   SET status='failed', error=$2, completed_at=now()
		 WHERE id=$1`, id, msg)
	if err != nil {
		return fmt.Errorf("mark report %d failed: %w", id, err)
	}
	return nil
}

// ReconcileOrphanedReports marks every pending/running row as failed. Intended
// for boot-time recovery — anything still in flight when the process restarted
// is unrecoverable because the in-memory queue is gone. Returns the number of
// rows touched.
func (s *Store) ReconcileOrphanedReports(ctx context.Context) (int64, error) {
	tag, err := s.pool.Exec(ctx,
		`UPDATE analysis_reports
		   SET status='failed', error='abandoned: server restarted', completed_at=now()
		 WHERE status IN ('pending','running')`)
	if err != nil {
		return 0, fmt.Errorf("reconcile orphaned reports: %w", err)
	}
	return tag.RowsAffected(), nil
}

// GetReport returns a single analysis report by ID.
func (s *Store) GetReport(ctx context.Context, id int64) (model.AnalysisReport, error) {
	row := s.pool.QueryRow(ctx,
		`SELECT `+analysisReportColumns+` FROM analysis_reports WHERE id=$1`, id)
	r, err := scanAnalysisReport(row)
	if errors.Is(err, pgx.ErrNoRows) {
		return model.AnalysisReport{}, err
	}
	if err != nil {
		return model.AnalysisReport{}, fmt.Errorf("get report %d: %w", id, err)
	}
	return r, nil
}

// GetReportBySlug returns a single analysis report by slug.
func (s *Store) GetReportBySlug(ctx context.Context, slug string) (model.AnalysisReport, error) {
	row := s.pool.QueryRow(ctx,
		`SELECT `+analysisReportColumns+` FROM analysis_reports WHERE slug=$1`, slug)
	r, err := scanAnalysisReport(row)
	if errors.Is(err, pgx.ErrNoRows) {
		return model.AnalysisReport{}, err
	}
	if err != nil {
		return model.AnalysisReport{}, fmt.Errorf("get report %q: %w", slug, err)
	}
	return r, nil
}

// DeleteReport removes a report row. Returns pgx.ErrNoRows when nothing matched.
func (s *Store) DeleteReport(ctx context.Context, id int64) error {
	tag, err := s.pool.Exec(ctx, `DELETE FROM analysis_reports WHERE id=$1`, id)
	if err != nil {
		return fmt.Errorf("delete report %d: %w", id, err)
	}
	if tag.RowsAffected() == 0 {
		return pgx.ErrNoRows
	}
	return nil
}

// ListReports returns recent analysis report summaries newest-first.
func (s *Store) ListReports(ctx context.Context, limit int) ([]model.AnalysisReportSummary, error) {
	rows, err := s.pool.Query(ctx,
		`SELECT `+analysisReportSummaryColumns+`
		   FROM analysis_reports
		  ORDER BY created_at DESC
		  LIMIT $1`, limit)
	if err != nil {
		return nil, fmt.Errorf("list reports: %w", err)
	}
	defer rows.Close()

	var reports []model.AnalysisReportSummary
	for rows.Next() {
		var r model.AnalysisReportSummary
		if err := rows.Scan(
			&r.ID, &r.Slug, &r.Feed, &r.PromptMode, &r.Model, &r.PeriodStart, &r.PeriodEnd,
			&r.PromptTokens, &r.CompletionTokens, &r.Status,
			&r.CreatedAt, &r.StartedAt, &r.CompletedAt,
		); err != nil {
			return nil, fmt.Errorf("scan report summary: %w", err)
		}
		reports = append(reports, r)
	}
	return reports, rows.Err()
}

// scanAnalysisReport scans a full analysis report row, handling nullable fields.
func scanAnalysisReport(row pgx.Row) (model.AnalysisReport, error) {
	var r model.AnalysisReport
	var body, errMsg *string
	if err := row.Scan(
		&r.ID, &r.Slug, &r.Feed, &r.PromptMode, &r.Model, &r.PeriodStart, &r.PeriodEnd,
		&body, &r.PromptTokens, &r.CompletionTokens, &r.Status, &errMsg,
		&r.CreatedAt, &r.StartedAt, &r.CompletedAt,
	); err != nil {
		return model.AnalysisReport{}, err
	}
	if body != nil {
		r.Report = *body
	}
	if errMsg != nil {
		r.Error = *errMsg
	}
	return r, nil
}
