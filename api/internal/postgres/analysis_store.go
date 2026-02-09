package postgres

import (
	"context"
	"errors"
	"fmt"
	"time"

	sq "github.com/Masterminds/squirrel"
	"github.com/jackc/pgx/v5"

	"github.com/lasseh/taillight/internal/model"
)

// GetTopMsgIDs returns the top msgids by count since the given time,
// with per-severity breakdowns.
func (s *Store) GetTopMsgIDs(ctx context.Context, since time.Time, limit int) ([]model.MsgIDCount, error) {
	// First get top msgids by total count.
	query, args, err := psq.
		Select("msgid", "count(*) AS cnt").
		From("syslog_events").
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
		From("syslog_events").
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
func (s *Store) GetSeverityComparison(ctx context.Context, currentSince, baselineSince time.Time) (model.SeverityComparison, error) {
	// Current period counts.
	curQuery, curArgs, err := psq.
		Select("severity", "count(*) AS cnt").
		From("syslog_events").
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
		From("syslog_events").
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
			Current:     cur,
			BaselineAvg: avg,
			ChangePct:   changePct,
		})
	}

	return model.SeverityComparison{Levels: levels}, nil
}

// GetTopErrorHosts returns hosts with the most errors (severity <= 3).
func (s *Store) GetTopErrorHosts(ctx context.Context, since time.Time, limit int) ([]model.HostErrorCount, error) {
	query := `
		SELECT hostname, count(*) AS cnt,
		       (SELECT msgid FROM syslog_events e2
		        WHERE e2.hostname = e1.hostname AND e2.received_at >= $1 AND e2.severity <= 3 AND e2.msgid != ''
		        GROUP BY msgid ORDER BY count(*) DESC LIMIT 1) AS top_msgid
		FROM syslog_events e1
		WHERE received_at >= $1 AND severity <= 3
		GROUP BY hostname
		ORDER BY cnt DESC
		LIMIT $2`

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
func (s *Store) GetNewMsgIDs(ctx context.Context, since, baselineSince time.Time) ([]string, error) {
	query := `
		SELECT DISTINCT msgid FROM syslog_events curr
		WHERE curr.received_at >= $1 AND curr.msgid != ''
		  AND NOT EXISTS (
		    SELECT 1 FROM syslog_events base
		    WHERE base.msgid = curr.msgid
		      AND base.received_at >= $2 AND base.received_at < $1
		      AND base.msgid != ''
		  )
		ORDER BY msgid`

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
func (s *Store) GetEventClusters(ctx context.Context, since time.Time, windowMinutes int) ([]model.EventCluster, error) {
	query := `
		SELECT time_bucket($2::interval, received_at) AS bucket,
		       array_agg(DISTINCT hostname) AS hosts,
		       array_agg(DISTINCT msgid) FILTER (WHERE msgid != '') AS msgids,
		       count(*) AS total
		FROM syslog_events
		WHERE received_at >= $1
		GROUP BY bucket
		HAVING count(DISTINCT hostname) > 1
		ORDER BY total DESC
		LIMIT 20`

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
func (s *Store) LookupJuniperRefs(ctx context.Context, names []string) (map[string]model.JuniperSyslogRef, error) {
	if len(names) == 0 {
		return nil, nil
	}

	query, args, err := psq.
		Select("name", "description", "cause", "action").
		From("juniper_syslog_ref").
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

	refs := make(map[string]model.JuniperSyslogRef)
	for rows.Next() {
		var r model.JuniperSyslogRef
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

// InsertReport stores an analysis report.
func (s *Store) InsertReport(ctx context.Context, r model.AnalysisReport) (int64, error) {
	query, args, err := psq.
		Insert("analysis_reports").
		Columns("model", "period_start", "period_end", "report",
			"prompt_tokens", "completion_tokens", "duration_ms", "status").
		Values(r.Model, r.PeriodStart, r.PeriodEnd, r.Report,
			r.PromptTokens, r.CompletionTokens, r.DurationMS, r.Status).
		Suffix("RETURNING id").
		ToSql()
	if err != nil {
		return 0, fmt.Errorf("build insert report query: %w", err)
	}

	var id int64
	if err := s.pool.QueryRow(ctx, query, args...).Scan(&id); err != nil {
		return 0, fmt.Errorf("insert report: %w", err)
	}
	return id, nil
}

// GetReport returns a single analysis report by ID.
func (s *Store) GetReport(ctx context.Context, id int64) (model.AnalysisReport, error) {
	query, args, err := psq.
		Select("id", "generated_at", "model", "period_start", "period_end",
			"report", "prompt_tokens", "completion_tokens", "duration_ms", "status").
		From("analysis_reports").
		Where(sq.Eq{"id": id}).
		ToSql()
	if err != nil {
		return model.AnalysisReport{}, fmt.Errorf("build get report query: %w", err)
	}

	var r model.AnalysisReport
	err = s.pool.QueryRow(ctx, query, args...).Scan(
		&r.ID, &r.GeneratedAt, &r.Model, &r.PeriodStart, &r.PeriodEnd,
		&r.Report, &r.PromptTokens, &r.CompletionTokens, &r.DurationMS, &r.Status,
	)
	if errors.Is(err, pgx.ErrNoRows) {
		return model.AnalysisReport{}, err
	}
	if err != nil {
		return model.AnalysisReport{}, fmt.Errorf("get report %d: %w", id, err)
	}
	return r, nil
}

// ListReports returns recent analysis report summaries.
func (s *Store) ListReports(ctx context.Context, limit int) ([]model.AnalysisReportSummary, error) {
	query, args, err := psq.
		Select("id", "generated_at", "model", "period_start", "period_end",
			"prompt_tokens", "completion_tokens", "duration_ms", "status").
		From("analysis_reports").
		OrderBy("generated_at DESC").
		Limit(uint64(limit)).
		ToSql()
	if err != nil {
		return nil, fmt.Errorf("build list reports query: %w", err)
	}

	rows, err := s.pool.Query(ctx, query, args...)
	if err != nil {
		return nil, fmt.Errorf("list reports query: %w", err)
	}
	defer rows.Close()

	var reports []model.AnalysisReportSummary
	for rows.Next() {
		var r model.AnalysisReportSummary
		if err := rows.Scan(
			&r.ID, &r.GeneratedAt, &r.Model, &r.PeriodStart, &r.PeriodEnd,
			&r.PromptTokens, &r.CompletionTokens, &r.DurationMS, &r.Status,
		); err != nil {
			return nil, fmt.Errorf("scan report summary: %w", err)
		}
		reports = append(reports, r)
	}
	return reports, rows.Err()
}

// GetLatestReport returns the most recent completed analysis report.
func (s *Store) GetLatestReport(ctx context.Context) (model.AnalysisReport, error) {
	query, args, err := psq.
		Select("id", "generated_at", "model", "period_start", "period_end",
			"report", "prompt_tokens", "completion_tokens", "duration_ms", "status").
		From("analysis_reports").
		Where(sq.Eq{"status": "completed"}).
		OrderBy("generated_at DESC").
		Limit(1).
		ToSql()
	if err != nil {
		return model.AnalysisReport{}, fmt.Errorf("build latest report query: %w", err)
	}

	var r model.AnalysisReport
	err = s.pool.QueryRow(ctx, query, args...).Scan(
		&r.ID, &r.GeneratedAt, &r.Model, &r.PeriodStart, &r.PeriodEnd,
		&r.Report, &r.PromptTokens, &r.CompletionTokens, &r.DurationMS, &r.Status,
	)
	if errors.Is(err, pgx.ErrNoRows) {
		return model.AnalysisReport{}, err
	}
	if err != nil {
		return model.AnalysisReport{}, fmt.Errorf("get latest report: %w", err)
	}
	return r, nil
}
