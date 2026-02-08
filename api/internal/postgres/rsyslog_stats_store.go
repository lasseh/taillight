package postgres

import (
	"context"
	"encoding/json"
	"fmt"
	"time"

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

// innerStatsExpr is the SQL expression that extracts the inner JSON object.
// ompgsql stores impstats as {"msg": "{ ... }"} — the actual stats are a
// JSON string inside the "msg" key. This expression parses it back to JSONB.
const innerStatsExpr = `(stats ->> 'msg')::jsonb`

// GetRsyslogStatsSummary returns aggregated KPIs from the latest snapshot per component.
func (s *Store) GetRsyslogStatsSummary(ctx context.Context, rangeDur time.Duration) (model.RsyslogStatsSummary, error) {
	since := time.Now().UTC().Add(-rangeDur)

	// Extract origin/name from the inner JSON since ompgsql doesn't populate
	// the origin/name columns. Use DISTINCT ON to get the latest per component.
	query := fmt.Sprintf(
		`SELECT DISTINCT ON (inner_origin, inner_name)
		        collected_at,
		        COALESCE((%s ->> 'origin'), origin) AS inner_origin,
		        COALESCE((%s ->> 'name'), name) AS inner_name,
		        %s AS inner_stats
		 FROM rsyslog_stats
		 WHERE collected_at >= $1
		 ORDER BY inner_origin, inner_name, collected_at DESC`,
		innerStatsExpr, innerStatsExpr, innerStatsExpr)

	rows, err := s.pool.Query(ctx, query, since)
	if err != nil {
		return model.RsyslogStatsSummary{}, fmt.Errorf("rsyslog stats summary query: %w", err)
	}
	defer rows.Close()

	var summary model.RsyslogStatsSummary
	summary.Components = make([]model.RsyslogStatsComponent, 0)

	for rows.Next() {
		var comp model.RsyslogStatsComponent
		if err := rows.Scan(&comp.CollectedAt, &comp.Origin, &comp.Name, &comp.Stats); err != nil {
			return model.RsyslogStatsSummary{}, fmt.Errorf("scan rsyslog stats component: %w", err)
		}

		// Parse the JSONB stats to extract numeric fields.
		var fields map[string]json.Number
		if err := json.Unmarshal(comp.Stats, &fields); err != nil {
			summary.Components = append(summary.Components, comp)
			continue
		}

		submitted := jsonNumberToInt64(fields["submitted"])
		processed := jsonNumberToInt64(fields["processed"])
		failed := jsonNumberToInt64(fields["failed"])
		suspended := jsonNumberToInt64(fields["suspended"])
		discardedFull := jsonNumberToInt64(fields["discarded.full"])
		discardedNF := jsonNumberToInt64(fields["discarded.nf"])
		size := jsonNumberToInt64(fields["size"])
		maxqsize := jsonNumberToInt64(fields["maxqsize"])

		// Input modules report "msgs.received" instead of "submitted".
		if submitted == 0 {
			submitted = jsonNumberToInt64(fields["msgs.received"])
		}

		// Aggregate by origin type.
		switch comp.Origin {
		case "imudp", "imtcp", "imptcp":
			summary.TotalSubmitted += submitted
		}

		// Actions have "processed" and "failed" fields.
		if processed > 0 || failed > 0 {
			summary.TotalProcessed += processed
			summary.TotalFailed += failed
			summary.TotalSuspended += suspended
		}

		// Queue metrics from "main Q".
		if comp.Name == "main Q" {
			summary.MainQueueSize = size
			summary.MainQueueMaxSize = maxqsize
			summary.TotalDiscarded += discardedFull + discardedNF
		}

		summary.Components = append(summary.Components, comp)
	}
	if err := rows.Err(); err != nil {
		return model.RsyslogStatsSummary{}, fmt.Errorf("rsyslog stats rows: %w", err)
	}

	// Compute rates.
	if summary.TotalSubmitted > 0 {
		summary.FilterRate = float64(summary.TotalSubmitted-summary.TotalProcessed) / float64(summary.TotalSubmitted) * 100
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
	case "discarded.full":
		fieldExpr = fmt.Sprintf(`(%s #>> '{discarded.full}')::numeric`, innerStatsExpr)
	case "discarded.nf":
		fieldExpr = fmt.Sprintf(`(%s #>> '{discarded.nf}')::numeric`, innerStatsExpr)
	default:
		fieldExpr = fmt.Sprintf(`(%s ->> '%s')::numeric`, innerStatsExpr, field)
	}

	// Also extract the name from the inner JSON for grouping.
	nameExpr := fmt.Sprintf(`COALESCE((%s ->> 'name'), name)`, innerStatsExpr)

	query := fmt.Sprintf(
		`SELECT time_bucket($1::interval, collected_at) AS bucket,
		        %s AS comp_name,
		        SUM(COALESCE(%s, 0)) AS val
		 FROM rsyslog_stats
		 WHERE collected_at >= $2
		 GROUP BY bucket, comp_name
		 ORDER BY bucket ASC, comp_name`, nameExpr, fieldExpr)

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

func jsonNumberToInt64(n json.Number) int64 {
	if n == "" {
		return 0
	}
	v, _ := n.Int64()
	return v
}
