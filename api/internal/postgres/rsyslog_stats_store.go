package postgres

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"regexp"
	"strings"
	"time"

	"github.com/jackc/pgx/v5"

	"github.com/lasseh/taillight/internal/model"
)

// allowedStatsFields is a whitelist of JSONB fields that can be queried.
var allowedStatsFields = map[string]struct{}{
	"submitted":      {},
	"enqueued":       {},
	"size":           {},
	"processed":      {},
	"failed":         {},
	"suspended":      {},
	"discarded.full": {},
	"discarded.nf":   {},
	"maxqsize":       {},
}

// workerRe matches rsyslog worker thread names like "imudp(w0)" or "w0/imtcp".
// These duplicate the listener-level stats and must be excluded from totals.
var workerRe = regexp.MustCompile(`\(w\d+\)|^w\d+/`)

// innerStatsExpr is the SQL expression that extracts the inner JSON object.
// ompgsql stores impstats as {"msg": "{ ... }"} — the actual stats are a
// JSON string inside the "msg" key. This expression parses it back to JSONB.
const innerStatsExpr = `(stats ->> 'msg')::jsonb`

// GetRsyslogStatsSummary returns aggregated KPIs from all snapshots in the range.
// Because impstats uses resetCounters=on, each snapshot contains deltas for
// that interval — we SUM across all snapshots to get cumulative totals.
// Queue size/maxqsize are gauges, so we take the latest snapshot for those.
func (s *Store) GetRsyslogStatsSummary(ctx context.Context, rangeDur time.Duration) (model.RsyslogStatsSummary, error) {
	since := time.Now().UTC().Add(-rangeDur)

	// SUM all counter fields per component across the range.
	// For "msgs.received" (inputs) we coalesce it into the submitted slot.
	query := fmt.Sprintf(
		`SELECT COALESCE((%[1]s ->> 'origin'), origin) AS comp_origin,
		        COALESCE((%[1]s ->> 'name'), name)     AS comp_name,
		        COALESCE(SUM((%[1]s ->> 'submitted')::numeric), 0)
		          + COALESCE(SUM((%[1]s #>> '{msgs.received}')::numeric), 0) AS sum_submitted,
		        COALESCE(SUM((%[1]s ->> 'processed')::numeric), 0)      AS sum_processed,
		        COALESCE(SUM((%[1]s ->> 'failed')::numeric), 0)         AS sum_failed,
		        COALESCE(SUM((%[1]s ->> 'suspended')::numeric), 0)      AS sum_suspended,
		        COALESCE(SUM((%[1]s #>> '{discarded.full}')::numeric), 0)
		          + COALESCE(SUM((%[1]s #>> '{discarded.nf}')::numeric), 0) AS sum_discarded,
		        COALESCE(MAX((%[1]s ->> 'maxqsize')::numeric), 0)        AS max_qsize
		 FROM rsyslog_stats
		 WHERE collected_at >= $1
		 GROUP BY comp_origin, comp_name
		 ORDER BY comp_origin, comp_name`, innerStatsExpr)

	rows, err := s.pool.Query(ctx, query, since)
	if err != nil {
		return model.RsyslogStatsSummary{}, fmt.Errorf("rsyslog stats summary query: %w", err)
	}
	defer rows.Close()

	var summary model.RsyslogStatsSummary
	summary.Components = make([]model.RsyslogStatsComponent, 0)

	for rows.Next() {
		var (
			origin    string
			name      string
			submitted int64
			processed int64
			failed    int64
			suspended int64
			discarded int64
			maxqsize  int64
		)
		if err := rows.Scan(&origin, &name, &submitted, &processed, &failed, &suspended, &discarded, &maxqsize); err != nil {
			return model.RsyslogStatsSummary{}, fmt.Errorf("scan rsyslog stats summary: %w", err)
		}

		// Build per-component stats JSON for the frontend.
		statsMap := map[string]int64{
			"submitted": submitted,
			"processed": processed,
			"failed":    failed,
			"suspended": suspended,
			"maxqsize":  maxqsize,
		}
		statsJSON, err := json.Marshal(statsMap)
		if err != nil {
			return model.RsyslogStatsSummary{}, fmt.Errorf("marshal component stats: %w", err)
		}

		comp := model.RsyslogStatsComponent{
			Origin: origin,
			Name:   name,
			Stats:  statsJSON,
		}

		// Aggregate KPI totals. Skip worker threads to avoid double-counting
		// (listener-level and worker-level stats report the same messages).
		switch origin {
		case "imudp", "imtcp", "imptcp":
			if !workerRe.MatchString(name) {
				summary.TotalSubmitted += submitted
			}
		}

		// Use the ompgsql syslog action as the canonical "processed" count.
		// Match by explicit name or auto-generated pattern (action-N-builtin:ompgsql),
		// but exclude the stats writer action.
		isSyslogPgsql := name == "syslog_to_pgsql" ||
			(strings.Contains(name, "ompgsql") && name != "stats_to_pgsql")
		if isSyslogPgsql {
			summary.TotalProcessed += processed
			summary.TotalFailed += failed
			summary.TotalSuspended += suspended
		}

		if name == "main Q" {
			summary.MainQueueMaxSize = maxqsize
			summary.TotalDiscarded += discarded
		}

		summary.Components = append(summary.Components, comp)
	}
	if err := rows.Err(); err != nil {
		return model.RsyslogStatsSummary{}, fmt.Errorf("rsyslog stats rows: %w", err)
	}

	// Queue size is a gauge — get the latest value.
	sizeQuery := fmt.Sprintf(
		`SELECT COALESCE((%[1]s ->> 'size')::bigint, 0)
		 FROM rsyslog_stats
		 WHERE collected_at >= $1
		   AND COALESCE((%[1]s ->> 'name'), name) = 'main Q'
		 ORDER BY collected_at DESC
		 LIMIT 1`, innerStatsExpr)

	if err := s.pool.QueryRow(ctx, sizeQuery, since).Scan(&summary.MainQueueSize); err != nil && !errors.Is(err, pgx.ErrNoRows) {
		return model.RsyslogStatsSummary{}, fmt.Errorf("rsyslog main queue size: %w", err)
	}

	// Compute rates. Clamp filter rate to [0,100] — processed can exceed
	// submitted when internal actions (impstats pipeline) are counted.
	if summary.TotalSubmitted > 0 {
		summary.FilterRate = float64(summary.TotalSubmitted-summary.TotalProcessed) / float64(summary.TotalSubmitted) * 100
		if summary.FilterRate < 0 {
			summary.FilterRate = 0
		}
	}
	if summary.TotalProcessed > 0 {
		summary.FailureRate = float64(summary.TotalFailed) / float64(summary.TotalProcessed) * 100
	}

	// Ingest rate: msgs/min over the range.
	rangeMinutes := rangeDur.Minutes()
	if rangeMinutes > 0 && summary.TotalSubmitted > 0 {
		summary.IngestRate = float64(summary.TotalSubmitted) / rangeMinutes
	}

	return summary, nil
}

// GetRsyslogStatsTimeSeries returns time-bucketed values for a JSONB field.
func (s *Store) GetRsyslogStatsTimeSeries(ctx context.Context, field string, interval model.VolumeInterval, rangeDur time.Duration) ([]model.RsyslogStatsTimeSeries, error) {
	if _, ok := allowedStatsFields[field]; !ok {
		return nil, fmt.Errorf("disallowed stats field: %s", field)
	}
	if !interval.IsValid() {
		return nil, fmt.Errorf("invalid volume interval: %s", interval)
	}

	since := time.Now().UTC().Add(-rangeDur)

	// Extract the field from the inner JSON (stats->'msg' parsed as JSONB).
	var fieldExpr string
	switch field {
	case "submitted":
		// Input modules report msgs.received, not submitted — combine both
		// to match the summary logic. Exclude worker threads to avoid
		// double-counting (e.g. imudp(w0), w0/imtcp).
		fieldExpr = fmt.Sprintf(
			`COALESCE((%[1]s ->> 'submitted')::numeric, 0) + COALESCE((%[1]s #>> '{msgs.received}')::numeric, 0)`,
			innerStatsExpr)
	case "discarded.full":
		fieldExpr = fmt.Sprintf(`(%s #>> '{discarded.full}')::numeric`, innerStatsExpr)
	case "discarded.nf":
		fieldExpr = fmt.Sprintf(`(%s #>> '{discarded.nf}')::numeric`, innerStatsExpr)
	default:
		fieldExpr = fmt.Sprintf(`(%s ->> '%s')::numeric`, innerStatsExpr, field)
	}

	// Also extract the name from the inner JSON for grouping.
	nameExpr := fmt.Sprintf(`COALESCE((%s ->> 'name'), name)`, innerStatsExpr)

	// Filter to match the summary logic and avoid inflated totals.
	var extraWhere string
	switch field {
	case "submitted":
		// Only input origins, exclude worker threads to avoid double-counting.
		originExpr := fmt.Sprintf(`COALESCE((%s ->> 'origin'), origin)`, innerStatsExpr)
		extraWhere = fmt.Sprintf(
			` AND %s IN ('imudp', 'imtcp', 'imptcp') AND NOT (%[2]s ~ '\(w\d+\)' OR %[2]s ~ '^w\d+/')`,
			originExpr, nameExpr)
	case "processed", "failed", "suspended":
		// Only the syslog-to-DB action, matching the summary KPI filter.
		extraWhere = fmt.Sprintf(
			` AND (%[1]s = 'syslog_to_pgsql' OR (%[1]s LIKE '%%ompgsql%%' AND %[1]s != 'stats_to_pgsql'))`,
			nameExpr)
	case "enqueued", "size", "maxqsize":
		// Only the main queue — other queues (action queues, disk-assisted)
		// would inflate the totals.
		extraWhere = fmt.Sprintf(` AND %s = 'main Q'`, nameExpr)
	}

	query := fmt.Sprintf(
		`SELECT time_bucket($1::interval, collected_at) AS bucket,
		        %s AS comp_name,
		        SUM(COALESCE(%s, 0)) AS val
		 FROM rsyslog_stats
		 WHERE collected_at >= $2%s
		 GROUP BY bucket, comp_name
		 ORDER BY bucket ASC, comp_name`, nameExpr, fieldExpr, extraWhere)

	rows, err := s.pool.Query(ctx, query, interval.String(), since)
	if err != nil {
		return nil, fmt.Errorf("rsyslog stats time series query: %w", err)
	}
	defer rows.Close()

	var series []model.RsyslogStatsTimeSeries
	for rows.Next() {
		var ts model.RsyslogStatsTimeSeries
		if err := rows.Scan(&ts.Time, &ts.Name, &ts.Value); err != nil {
			return nil, fmt.Errorf("scan rsyslog stats time series: %w", err)
		}
		series = append(series, ts)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("rsyslog stats time series rows: %w", err)
	}

	return series, nil
}
