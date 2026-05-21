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
	feedAll = "all"
)

// analysisTableName returns the table name for the given feed.
// Valid feeds: "srvlog", "netlog". For "all", use analysisUnionSource instead.
func analysisTableName(feed string) string {
	switch feed {
	case "netlog":
		return "netlog_events"
	case "srvlog":
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

// GetTopMsgIDs returns the top msgids by count since the given time,
// with per-severity breakdowns. The feed parameter selects which table(s)
// to query: "srvlog", "netlog", or "all".
func (s *Store) GetTopMsgIDs(ctx context.Context, feed string, since time.Time, limit int) ([]model.MsgIDCount, error) {
	var table string
	if feed == feedAll {
		table = analysisUnionSource("received_at, msgid, severity")
	} else {
		table = analysisTableName(feed)
	}

	// First get top msgids by total count.
	query, args, err := psq.
		Select("msgid", "count(*) AS cnt").
		From(table).
		Where(sq.GtOrEq{"received_at": since}).
		Where(sq.NotEq{"msgid": ""}).
		GroupBy("msgid").
		OrderBy("cnt DESC").
		Limit(uint64(limit)).
		ToSql()
	if err != nil {
		return nil, fmt.Errorf("build top msgids query: %w", err)
	}

	rows, err := s.pool.Query(ctx, query, args...)
	if err != nil {
		return nil, fmt.Errorf("top msgids query: %w", err)
	}
	defer rows.Close()

	var results []model.MsgIDCount
	msgidIndex := make(map[string]int)
	for rows.Next() {
		var mc model.MsgIDCount
		if err := rows.Scan(&mc.MsgID, &mc.Count); err != nil {
			return nil, fmt.Errorf("scan top msgid: %w", err)
		}
		mc.SeverityCounts = make(map[int]int64)
		msgidIndex[mc.MsgID] = len(results)
		results = append(results, mc)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("top msgids rows: %w", err)
	}

	if len(results) == 0 {
		return results, nil
	}

	// Get severity breakdown for these msgids.
	msgids := make([]string, len(results))
	for i, mc := range results {
		msgids[i] = mc.MsgID
	}

	sevQuery, sevArgs, err := psq.
		Select("msgid", "severity", "count(*) AS cnt").
		From(table).
		Where(sq.GtOrEq{"received_at": since}).
		Where(sq.Eq{"msgid": msgids}).
		GroupBy("msgid", "severity").
		ToSql()
	if err != nil {
		return nil, fmt.Errorf("build severity breakdown query: %w", err)
	}

	sevRows, err := s.pool.Query(ctx, sevQuery, sevArgs...)
	if err != nil {
		return nil, fmt.Errorf("severity breakdown query: %w", err)
	}
	defer sevRows.Close()

	for sevRows.Next() {
		var msgid string
		var sev int
		var cnt int64
		if err := sevRows.Scan(&msgid, &sev, &cnt); err != nil {
			return nil, fmt.Errorf("scan severity breakdown: %w", err)
		}
		if idx, ok := msgidIndex[msgid]; ok {
			results[idx].SeverityCounts[sev] = cnt
		}
	}
	if err := sevRows.Err(); err != nil {
		return nil, fmt.Errorf("severity breakdown rows: %w", err)
	}

	return results, nil
}

// GetSeverityComparison compares current period severity counts against baseline daily average.
// The feed parameter selects which table(s) to query: "srvlog", "netlog", or "all".
func (s *Store) GetSeverityComparison(ctx context.Context, feed string, currentSince, baselineSince time.Time) (model.SeverityComparison, error) {
	var table string
	if feed == feedAll {
		table = analysisUnionSource("received_at, severity")
	} else {
		table = analysisTableName(feed)
	}

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
func (s *Store) GetTopErrorHosts(ctx context.Context, feed string, since time.Time, limit int) ([]model.HostErrorCount, error) {
	var source string
	if feed == feedAll {
		source = analysisUnionSource("received_at, hostname, severity, msgid")
	} else {
		source = analysisTableName(feed)
	}

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
		SELECT hc.hostname, hc.cnt, tm.msgid AS top_msgid
		FROM host_counts hc
		LEFT JOIN LATERAL (
			SELECT msgid
			FROM events
			WHERE hostname = hc.hostname AND received_at >= $1 AND severity <= 3 AND msgid != ''
			GROUP BY msgid
			ORDER BY count(*) DESC
			LIMIT 1
		) tm ON true
		ORDER BY hc.cnt DESC`, source)

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

// GetNewMsgIDs returns msgids seen in the current period but not in the baseline period.
// The feed parameter selects which table(s) to query: "srvlog", "netlog", or "all".
func (s *Store) GetNewMsgIDs(ctx context.Context, feed string, since, baselineSince time.Time) ([]string, error) {
	var source string
	if feed == feedAll {
		source = analysisUnionSource("received_at, msgid")
	} else {
		source = analysisTableName(feed)
	}

	query := fmt.Sprintf(`
		WITH events AS (
			SELECT * FROM %s
		)
		SELECT DISTINCT msgid FROM events curr
		WHERE curr.received_at >= $1 AND curr.msgid != ''
		  AND NOT EXISTS (
		    SELECT 1 FROM events base
		    WHERE base.msgid = curr.msgid
		      AND base.received_at >= $2 AND base.received_at < $1
		      AND base.msgid != ''
		  )
		ORDER BY msgid`, source)

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
func (s *Store) GetEventClusters(ctx context.Context, feed string, since time.Time, windowMinutes int) ([]model.EventCluster, error) {
	var source string
	if feed == feedAll {
		source = analysisUnionSource("received_at, hostname, msgid")
	} else {
		source = analysisTableName(feed)
	}

	query := fmt.Sprintf(`
		SELECT time_bucket($2::interval, received_at) AS bucket,
		       array_agg(DISTINCT hostname) AS hosts,
		       array_agg(DISTINCT msgid) FILTER (WHERE msgid != '') AS msgids,
		       count(*) AS total
		FROM %s
		WHERE received_at >= $1
		GROUP BY bucket
		HAVING count(DISTINCT hostname) > 1
		ORDER BY total DESC
		LIMIT 20`, source)

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
const analysisReportColumns = "id, slug, feed, model, period_start, period_end, " +
	"report, prompt_tokens, completion_tokens, status, error, " +
	"created_at, started_at, completed_at"

// analysisReportSummaryColumns lists the columns selected for list reads.
const analysisReportSummaryColumns = "id, slug, feed, model, period_start, period_end, " +
	"prompt_tokens, completion_tokens, status, " +
	"created_at, started_at, completed_at"

// BuildAnalysisSlug returns the canonical slug for a (feed, periodEnd) pair.
// periodEnd is truncated to the minute in UTC before formatting.
func BuildAnalysisSlug(feed string, periodEnd time.Time) string {
	t := periodEnd.UTC().Truncate(time.Minute)
	return fmt.Sprintf("%s-%s-%s", feed, t.Format("2006-01-02"), t.Format("1504"))
}

// InsertPendingReport creates a new report row in the pending state. It
// generates a slug from feed+periodEnd and resolves slug collisions with a
// numeric suffix so historical reports for the same minute can coexist.
//
// Returns ErrDuplicateActiveReport when the partial unique index
// analysis_reports_active_uniq is violated, which happens when another pending
// or running report already covers the same (feed, period_end).
func (s *Store) InsertPendingReport(ctx context.Context, r model.AnalysisReport) (model.AnalysisReport, error) {
	if r.Slug == "" {
		r.Slug = BuildAnalysisSlug(r.Feed, r.PeriodEnd)
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
			Columns("slug", "feed", "model", "period_start", "period_end", "status").
			Values(r.Slug, r.Feed, r.Model, r.PeriodStart, r.PeriodEnd, r.Status).
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
			&r.ID, &r.Slug, &r.Feed, &r.Model, &r.PeriodStart, &r.PeriodEnd,
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
		&r.ID, &r.Slug, &r.Feed, &r.Model, &r.PeriodStart, &r.PeriodEnd,
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
