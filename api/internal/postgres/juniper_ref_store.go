package postgres

import (
	"context"
	"fmt"

	sq "github.com/Masterminds/squirrel"
	"github.com/jackc/pgx/v5"

	"github.com/lasseh/taillight/internal/model"
)

// LookupJuniperRef returns all Juniper syslog reference entries matching the given name.
func (s *Store) LookupJuniperRef(ctx context.Context, name string) ([]model.JuniperNetlogRef, error) {
	query, args, err := psq.
		Select("id", "name", "message", "description", "type", "severity", "cause", "action", "os", "created_at").
		From("juniper_netlog_ref").
		Where(sq.Eq{"name": name}).
		OrderBy("os ASC").
		ToSql()
	if err != nil {
		return nil, fmt.Errorf("build juniper ref query: %w", err)
	}

	rows, err := s.pool.Query(ctx, query, args...)
	if err != nil {
		return nil, fmt.Errorf("lookup juniper ref %q: %w", name, err)
	}

	refs, err := pgx.CollectRows(rows, func(row pgx.CollectableRow) (model.JuniperNetlogRef, error) {
		var r model.JuniperNetlogRef
		err := row.Scan(
			&r.ID, &r.Name, &r.Message, &r.Description,
			&r.Type, &r.Severity, &r.Cause, &r.Action,
			&r.OS, &r.CreatedAt,
		)
		return r, err
	})
	if err != nil {
		return nil, fmt.Errorf("scan juniper ref: %w", err)
	}
	return refs, nil
}

// CountJuniperRefsByOS returns the number of juniper_netlog_ref rows for the given OS.
func (s *Store) CountJuniperRefsByOS(ctx context.Context, osName string) (int64, error) {
	query, args, err := psq.
		Select("COUNT(*)").
		From("juniper_netlog_ref").
		Where(sq.Eq{"os": osName}).
		ToSql()
	if err != nil {
		return 0, fmt.Errorf("build juniper ref count query: %w", err)
	}

	var n int64
	if err := s.pool.QueryRow(ctx, query, args...).Scan(&n); err != nil {
		return 0, fmt.Errorf("count juniper refs for os %q: %w", osName, err)
	}
	return n, nil
}

// UpsertJuniperRefs inserts or updates Juniper syslog reference entries.
// Returns the number of rows affected.
func (s *Store) UpsertJuniperRefs(ctx context.Context, refs []model.JuniperNetlogRef) (int64, error) {
	if len(refs) == 0 {
		return 0, nil
	}

	const batchSize = 500
	var total int64

	for i := 0; i < len(refs); i += batchSize {
		end := min(i+batchSize, len(refs))
		batch := refs[i:end]

		qb := psq.Insert("juniper_netlog_ref").
			Columns("name", "message", "description", "type", "severity", "cause", "action", "os")

		for _, r := range batch {
			qb = qb.Values(r.Name, r.Message, r.Description, r.Type, r.Severity, r.Cause, r.Action, r.OS)
		}

		qb = qb.Suffix(`ON CONFLICT (name, os) DO UPDATE SET
			message = EXCLUDED.message,
			description = EXCLUDED.description,
			type = EXCLUDED.type,
			severity = EXCLUDED.severity,
			cause = EXCLUDED.cause,
			action = EXCLUDED.action`)

		query, args, err := qb.ToSql()
		if err != nil {
			return total, fmt.Errorf("build upsert query: %w", err)
		}

		tag, err := s.pool.Exec(ctx, query, args...)
		if err != nil {
			return total, fmt.Errorf("upsert juniper refs: %w", err)
		}
		total += tag.RowsAffected()
	}

	return total, nil
}
