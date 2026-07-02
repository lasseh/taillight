package postgres

import (
	"context"
	"errors"
	"fmt"
	"time"

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

// analysisReportColumns lists the columns selected for full report reads.
const analysisReportColumns = "id, slug, feed, prompt_mode, hosts, model, period_start, period_end, " +
	"report, prompt_tokens, completion_tokens, status, error, " +
	"created_at, started_at, completed_at, notify_channel_ids"

// analysisReportSummaryColumns lists the columns selected for list reads.
const analysisReportSummaryColumns = "id, slug, feed, prompt_mode, hosts, model, period_start, period_end, " +
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
	// Normalize hosts at the persistence boundary so the active-report unique
	// index treats ["a","b"] and ["b","a","a"] as the same key. Empty/nil
	// hosts are written to Postgres as '{}' — the canonical "all hosts" value
	// — by the explicit []string{} fallback below.
	r.Hosts = model.NormalizeHosts(r.Hosts)
	hostsArg := r.Hosts
	if hostsArg == nil {
		hostsArg = []string{}
	}

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
			Columns("slug", "feed", "prompt_mode", "hosts", "model", "period_start", "period_end", "status", "notify_channel_ids").
			Values(r.Slug, r.Feed, r.PromptMode, hostsArg, r.Model, r.PeriodStart, r.PeriodEnd, r.Status, channelIDsOrEmpty(r.NotifyChannelIDs)).
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

// MarkReportNotified is the idempotency seam for completion emails. It runs
// an atomic CAS that flips notified_at from NULL to now() exactly once per
// row; subsequent calls find notified_at non-NULL and return won=false. The
// worker uses this to gate engine.SendAnalysisReport so a worker retry on
// MarkReportCompleted can't deliver duplicate emails. Returns won=false (no
// error) when the row already had notified_at set or when no row matched
// (deleted mid-flight).
func (s *Store) MarkReportNotified(ctx context.Context, id int64) (bool, error) {
	var got int64
	err := s.pool.QueryRow(ctx,
		`UPDATE analysis_reports
		   SET notified_at = now()
		 WHERE id = $1 AND notified_at IS NULL
		 RETURNING id`, id).Scan(&got)
	if errors.Is(err, pgx.ErrNoRows) {
		return false, nil
	}
	if err != nil {
		return false, fmt.Errorf("mark report %d notified: %w", id, err)
	}
	return true, nil
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

	reports, err := pgx.CollectRows(rows, func(row pgx.CollectableRow) (model.AnalysisReportSummary, error) {
		var r model.AnalysisReportSummary
		err := row.Scan(
			&r.ID, &r.Slug, &r.Feed, &r.PromptMode, &r.Hosts, &r.Model, &r.PeriodStart, &r.PeriodEnd,
			&r.PromptTokens, &r.CompletionTokens, &r.Status,
			&r.CreatedAt, &r.StartedAt, &r.CompletedAt,
		)
		return r, err
	})
	if err != nil {
		return nil, fmt.Errorf("scan report summary: %w", err)
	}
	return reports, nil
}

// scanAnalysisReport scans a full analysis report row, handling nullable fields.
func scanAnalysisReport(row pgx.Row) (model.AnalysisReport, error) {
	var r model.AnalysisReport
	var body, errMsg *string
	if err := row.Scan(
		&r.ID, &r.Slug, &r.Feed, &r.PromptMode, &r.Hosts, &r.Model, &r.PeriodStart, &r.PeriodEnd,
		&body, &r.PromptTokens, &r.CompletionTokens, &r.Status, &errMsg,
		&r.CreatedAt, &r.StartedAt, &r.CompletedAt, &r.NotifyChannelIDs,
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
