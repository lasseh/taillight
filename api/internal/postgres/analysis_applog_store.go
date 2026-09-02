package postgres

import (
	"context"
	"fmt"
	"time"

	"github.com/jackc/pgx/v5"

	"github.com/lasseh/taillight/internal/model"
)

// applogAnalysisLevels is the level set every raw-row applog analysis query
// reads: warn and above. INFO and DEBUG rows are never scanned; where totals
// are needed they come from the hourly aggregate (ADR 0006).
var applogAnalysisLevels = model.AppLogLevelsAtLeast(appLevelWarn)

// applogErrorLevels is the error-and-above subset, used to split the volume
// timeline the way the syslog path splits on severity <= 3.
var applogErrorLevels = model.AppLogLevelsAtLeast(appLevelError)

// applogServiceFilter returns the predicate that narrows a query to the
// scope's services, or "" when the scope covers every service. The caller
// appends scope.Services to its args at position param (appendServicesArg).
func applogServiceFilter(scope model.AnalysisScope, param int) string {
	if scope.IsAllServices() {
		return ""
	}
	return fmt.Sprintf(" AND service = ANY($%d)", param)
}

// appendServicesArg returns args extended with the scope's service list
// when the scope is non-empty. Pairs with applogServiceFilter.
func appendServicesArg(args []any, scope model.AnalysisScope) []any {
	if scope.IsAllServices() {
		return args
	}
	return append(args, scope.Services)
}

// addAppLogLevelCount folds one (level, count) row into c.
func addAppLogLevelCount(c *model.AppLogLevelCounts, level string, n int64) {
	c.Total += n
	switch canon, _ := model.NormalizeLevel(level); canon {
	case appLevelWarn:
		c.Warn += n
	case appLevelError:
		c.Error += n
	case appLevelFatal:
		c.Fatal += n
	}
}

// GetAppLogServiceStats returns per-service counts for the window and for
// the baseline before it, read from applog_summary_hourly at every level.
// The aggregate is hour-bucketed, so a window edge inside an hour attributes
// that whole hour to whichever side its bucket start falls on.
func (s *Store) GetAppLogServiceStats(ctx context.Context, scope model.AnalysisScope, since, baselineSince time.Time) ([]model.AppLogServiceStats, error) {
	query := `
		SELECT service, level,
		       COALESCE(SUM(cnt) FILTER (WHERE bucket >= $1), 0) AS cur,
		       COALESCE(SUM(cnt) FILTER (WHERE bucket < $1), 0) AS base
		FROM applog_summary_hourly
		WHERE bucket >= $2` + applogServiceFilter(scope, 3) + `
		GROUP BY service, level
		ORDER BY service, level`

	rows, err := s.pool.Query(ctx, query, appendServicesArg([]any{since, baselineSince}, scope)...)
	if err != nil {
		return nil, fmt.Errorf("applog service stats query: %w", err)
	}
	defer rows.Close()

	var out []model.AppLogServiceStats
	index := make(map[string]int)
	for rows.Next() {
		var service, level string
		var cur, base int64
		if err := rows.Scan(&service, &level, &cur, &base); err != nil {
			return nil, fmt.Errorf("scan applog service stats: %w", err)
		}
		i, ok := index[service]
		if !ok {
			i = len(out)
			index[service] = i
			out = append(out, model.AppLogServiceStats{Service: service})
		}
		addAppLogLevelCount(&out[i].Current, level, cur)
		addAppLogLevelCount(&out[i].Baseline, level, base)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("applog service stats rows: %w", err)
	}
	return out, nil
}

// GetAppLogVolumeTimeline returns event counts per bucket across the window
// from the hourly aggregate, with error-and-above called out separately.
// bucketMinutes must be a multiple of 60: the aggregate has no finer grain.
func (s *Store) GetAppLogVolumeTimeline(ctx context.Context, scope model.AnalysisScope, since, until time.Time, bucketMinutes int) ([]model.AnalysisVolumeBucket, error) {
	if bucketMinutes <= 0 {
		return nil, nil
	}
	interval := fmt.Sprintf("%d minutes", bucketMinutes)
	query := `
		SELECT time_bucket($1::interval, bucket) AS b,
		       SUM(cnt) AS total,
		       COALESCE(SUM(cnt) FILTER (WHERE level = ANY($4)), 0) AS err_cnt
		FROM applog_summary_hourly
		WHERE bucket >= $2 AND bucket < $3` + applogServiceFilter(scope, 5) + `
		GROUP BY b
		ORDER BY b`

	rows, err := s.pool.Query(ctx, query, appendServicesArg([]any{interval, since, until, applogErrorLevels}, scope)...)
	if err != nil {
		return nil, fmt.Errorf("applog volume timeline query: %w", err)
	}
	results, err := pgx.CollectRows(rows, func(row pgx.CollectableRow) (model.AnalysisVolumeBucket, error) {
		var b model.AnalysisVolumeBucket
		err := row.Scan(&b.Bucket, &b.Total, &b.ErrorCount)
		return b, err
	})
	if err != nil {
		return nil, fmt.Errorf("scan applog volume bucket: %w", err)
	}
	return results, nil
}

// applogTopTemplatesQuery ranks warn-and-above templates per service in two
// lists, ERROR/FATAL and WARN, each by count. Params: $1 since, $2 levels,
// $3 services, $4 error-list limit, $5 warn-list limit, $6 the WARN level
// name. The services list is the scope here, so no separate filter applies.
//
// The window function runs over the grouped rows, so one scan produces the
// counts and the per-service rank together. No CTE: a derived table keeps
// the time predicate on the scan (see TestIntegration_AnalysisQueriesHaveNoCTEScan).
func applogTopTemplatesQuery() string {
	return `
		SELECT service, component, msg_pattern, level, cnt, hosts, first_seen, last_seen
		FROM (
			SELECT service, component, msg_pattern, level,
			       count(*) AS cnt,
			       count(DISTINCT host) AS hosts,
			       min(received_at) AS first_seen,
			       max(received_at) AS last_seen,
			       ROW_NUMBER() OVER (PARTITION BY service, level = $6 ORDER BY count(*) DESC, msg_pattern) AS rn
			FROM applog_events
			WHERE received_at >= $1 AND level = ANY($2) AND service = ANY($3) AND msg_pattern <> ''
			GROUP BY service, component, msg_pattern, level
		) t
		WHERE (level = $6 AND rn <= $5) OR (level <> $6 AND rn <= $4)
		ORDER BY service, level = $6, cnt DESC, msg_pattern`
}

// GetAppLogTopTemplates returns the most frequent warn-and-above templates
// for the given services: up to errorLimit ERROR/FATAL templates and up to
// warnLimit WARN templates per service, ordered by service, then errors
// before warnings, then count. Rows carry no samples; see
// GetAppLogTemplateSamples.
func (s *Store) GetAppLogTopTemplates(ctx context.Context, since time.Time, services []string, errorLimit, warnLimit int) ([]model.AppLogTemplate, error) {
	if len(services) == 0 || (errorLimit <= 0 && warnLimit <= 0) {
		return nil, nil
	}
	rows, err := s.pool.Query(ctx, applogTopTemplatesQuery(),
		since, applogAnalysisLevels, services, errorLimit, warnLimit, appLevelWarn)
	if err != nil {
		return nil, fmt.Errorf("applog top templates query: %w", err)
	}
	out, err := pgx.CollectRows(rows, scanAppLogTemplate)
	if err != nil {
		return nil, fmt.Errorf("scan applog top template: %w", err)
	}
	return out, nil
}

// scanAppLogTemplate reads the column set shared by the top and new
// template queries.
func scanAppLogTemplate(row pgx.CollectableRow) (model.AppLogTemplate, error) {
	var t model.AppLogTemplate
	err := row.Scan(&t.Service, &t.Component, &t.Pattern, &t.Level, &t.Count, &t.HostCount, &t.FirstSeen, &t.LastSeen)
	return t, err
}

// applogNewTemplatesQuery finds warn-and-above templates present in the
// window and absent from the baseline. Params: $1 since, $2 baselineSince,
// $3 levels, $4 limit, $5 the WARN level name, $6 services when scoped.
//
// An anti-join between two derived tables, each bounded on its own side:
// the window side aggregates counts, the baseline side is a DISTINCT key
// list. Plain column keys let the planner hash the join; the syslog path
// needed EXCEPT because its key is a computed expression.
func applogNewTemplatesQuery(scope model.AnalysisScope) string {
	filter := applogServiceFilter(scope, 6)
	return fmt.Sprintf(`
		SELECT c.service, c.component, c.msg_pattern, c.level, c.cnt, c.hosts, c.first_seen, c.last_seen
		FROM (
			SELECT service, component, msg_pattern, level,
			       count(*) AS cnt,
			       count(DISTINCT host) AS hosts,
			       min(received_at) AS first_seen,
			       max(received_at) AS last_seen
			FROM applog_events
			WHERE received_at >= $1 AND level = ANY($3) AND msg_pattern <> ''%s
			GROUP BY service, component, msg_pattern, level
		) c
		LEFT JOIN (
			SELECT DISTINCT service, component, msg_pattern
			FROM applog_events
			WHERE received_at >= $2 AND received_at < $1 AND level = ANY($3) AND msg_pattern <> ''%s
		) b ON b.service = c.service AND b.component = c.component AND b.msg_pattern = c.msg_pattern
		WHERE b.service IS NULL
		ORDER BY c.level = $5, c.cnt DESC, c.service, c.msg_pattern
		LIMIT $4`, filter, filter)
}

// GetAppLogNewTemplates returns warn-and-above templates seen in the window
// but not in the baseline, ERROR/FATAL first, then by count, capped at
// limit. The baseline is read at the same level floor, so a template that
// only logged at INFO before counts as new when it first appears at WARN.
func (s *Store) GetAppLogNewTemplates(ctx context.Context, scope model.AnalysisScope, since, baselineSince time.Time, limit int) ([]model.AppLogTemplate, error) {
	if limit <= 0 {
		return nil, nil
	}
	args := appendServicesArg([]any{since, baselineSince, applogAnalysisLevels, limit, appLevelWarn}, scope)
	rows, err := s.pool.Query(ctx, applogNewTemplatesQuery(scope), args...)
	if err != nil {
		return nil, fmt.Errorf("applog new templates query: %w", err)
	}
	out, err := pgx.CollectRows(rows, scanAppLogTemplate)
	if err != nil {
		return nil, fmt.Errorf("scan applog new template: %w", err)
	}
	return out, nil
}

// GetAppLogTemplateSamples returns the most recent warn-and-above row for
// each template key, with msg cut to msgChars and attrs as raw JSON text.
// Keys with no rows in the window are absent from the map.
func (s *Store) GetAppLogTemplateSamples(ctx context.Context, since time.Time, keys []model.AppLogTemplateKey, msgChars int) (map[model.AppLogTemplateKey]model.AppLogSample, error) {
	out := make(map[model.AppLogTemplateKey]model.AppLogSample, len(keys))
	if len(keys) == 0 {
		return out, nil
	}
	services := make([]string, len(keys))
	components := make([]string, len(keys))
	patterns := make([]string, len(keys))
	for i, k := range keys {
		services[i], components[i], patterns[i] = k.Service, k.Component, k.Pattern
	}

	// unnest turns the parallel key arrays into a join table so one query
	// serves every key; DISTINCT ON keeps the newest row per key.
	query := `
		SELECT DISTINCT ON (e.service, e.component, e.msg_pattern)
		       e.service, e.component, e.msg_pattern,
		       e.host, e.level, e.received_at, LEFT(e.msg, $5), COALESCE(e.attrs::text, '')
		FROM applog_events e
		JOIN unnest($2::text[], $3::text[], $4::text[]) AS k(service, component, pattern)
		  ON k.service = e.service AND k.component = e.component AND k.pattern = e.msg_pattern
		WHERE e.received_at >= $1 AND e.level = ANY($6)
		ORDER BY e.service, e.component, e.msg_pattern, e.received_at DESC`

	rows, err := s.pool.Query(ctx, query, since, services, components, patterns, msgChars, applogAnalysisLevels)
	if err != nil {
		return nil, fmt.Errorf("applog template samples query: %w", err)
	}
	defer rows.Close()

	for rows.Next() {
		var key model.AppLogTemplateKey
		var sm model.AppLogSample
		if err := rows.Scan(&key.Service, &key.Component, &key.Pattern,
			&sm.Host, &sm.Level, &sm.ReceivedAt, &sm.Msg, &sm.Attrs); err != nil {
			return nil, fmt.Errorf("scan applog template sample: %w", err)
		}
		out[key] = sm
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("applog template samples rows: %w", err)
	}
	return out, nil
}

// applogDominantTemplatesQuery finds, per service, the single most frequent
// warn-and-above template when it carries at least a given share of the
// service's warn-and-above volume. Params: $1 since, $2 levels, $3 minimum
// service volume, $4 share (0..1), $5 limit, $6 services when scoped.
func applogDominantTemplatesQuery(scope model.AnalysisScope) string {
	return `
		SELECT service, component, msg_pattern, cnt, svc_total
		FROM (
			SELECT service, component, msg_pattern,
			       count(*) AS cnt,
			       SUM(count(*)) OVER (PARTITION BY service) AS svc_total,
			       ROW_NUMBER() OVER (PARTITION BY service ORDER BY count(*) DESC, msg_pattern) AS rn
			FROM applog_events
			WHERE received_at >= $1 AND level = ANY($2) AND msg_pattern <> ''` + applogServiceFilter(scope, 6) + `
			GROUP BY service, component, msg_pattern
		) t
		WHERE rn = 1 AND svc_total >= $3 AND cnt >= $4::float8 * svc_total
		ORDER BY cnt DESC, service
		LIMIT $5`
}

// GetAppLogHygiene computes the log-hygiene facts over warn-and-above rows
// in the window: rows with an empty component, rows whose attrs exceed the
// preview limit, and templates that dominate their service's volume
// (share of at least dominantShare, service volume of at least
// dominantMinEvents, at most dominantLimit services).
func (s *Store) GetAppLogHygiene(ctx context.Context, scope model.AnalysisScope, since time.Time, dominantShare float64, dominantMinEvents int64, dominantLimit int) (model.AppLogHygiene, error) {
	var h model.AppLogHygiene

	countsQuery := `
		SELECT count(*),
		       count(*) FILTER (WHERE component = ''),
		       count(*) FILTER (WHERE attrs IS NOT NULL AND octet_length(attrs::text) > $3)
		FROM applog_events
		WHERE received_at >= $1 AND level = ANY($2)` + applogServiceFilter(scope, 4)
	if err := s.pool.QueryRow(ctx, countsQuery,
		appendServicesArg([]any{since, applogAnalysisLevels, model.AttrsPreviewLimit}, scope)...,
	).Scan(&h.WarnPlusRows, &h.EmptyComponent, &h.OversizeAttrs); err != nil {
		return h, fmt.Errorf("applog hygiene counts query: %w", err)
	}

	if dominantLimit <= 0 {
		return h, nil
	}
	args := appendServicesArg([]any{since, applogAnalysisLevels, dominantMinEvents, dominantShare, dominantLimit}, scope)
	rows, err := s.pool.Query(ctx, applogDominantTemplatesQuery(scope), args...)
	if err != nil {
		return h, fmt.Errorf("applog dominant templates query: %w", err)
	}
	h.Dominant, err = pgx.CollectRows(rows, func(row pgx.CollectableRow) (model.AppLogDominantTemplate, error) {
		var d model.AppLogDominantTemplate
		err := row.Scan(&d.Service, &d.Component, &d.Pattern, &d.Count, &d.ServiceTotal)
		return d, err
	})
	if err != nil {
		return h, fmt.Errorf("scan applog dominant template: %w", err)
	}
	return h, nil
}
