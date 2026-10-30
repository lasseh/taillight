package postgres

import (
	"context"
	"fmt"
	"time"

	"github.com/jackc/pgx/v5"

	"github.com/lasseh/taillight/internal/mute"
)

const muteRuleColumns = "id, feed, hostname, programname, search, reason, created_by, created_at, expires_at"

// scanMuteRule scans one muteRuleColumns row. NULL match fields become "".
func scanMuteRule(row pgx.Row) (mute.Rule, error) {
	var r mute.Rule
	var hostname, programname, search *string
	if err := row.Scan(&r.ID, &r.Feed, &hostname, &programname, &search,
		&r.Reason, &r.CreatedBy, &r.CreatedAt, &r.ExpiresAt); err != nil {
		return mute.Rule{}, err
	}
	r.Hostname = deref(hostname)
	r.Programname = deref(programname)
	r.Search = deref(search)
	return r, nil
}

// ListMuteRules returns the rules that expire after expiresAfter, newest
// expiry first. The rule set passes now (active rules); the list endpoint
// passes now minus the ended-rule window.
func (s *Store) ListMuteRules(ctx context.Context, expiresAfter time.Time) ([]mute.Rule, error) {
	const query = `SELECT ` + muteRuleColumns + ` FROM mute_rules
		WHERE expires_at > $1
		ORDER BY expires_at DESC, id DESC`

	rows, err := s.pool.Query(ctx, query, expiresAfter)
	if err != nil {
		return nil, fmt.Errorf("list mute rules: %w", err)
	}
	rules, err := pgx.CollectRows(rows, func(row pgx.CollectableRow) (mute.Rule, error) {
		return scanMuteRule(row)
	})
	if err != nil {
		return nil, fmt.Errorf("scan mute rule: %w", err)
	}
	return rules, nil
}

// CreateMuteRule inserts r and returns it with its id and created_at. Empty
// match fields are stored as NULL.
func (s *Store) CreateMuteRule(ctx context.Context, r mute.Rule) (mute.Rule, error) {
	const query = `INSERT INTO mute_rules (feed, hostname, programname, search, reason, created_by, expires_at)
		VALUES ($1, $2, $3, $4, $5, $6, $7)
		RETURNING id, created_at`

	err := s.pool.QueryRow(ctx, query,
		string(r.Feed), nullIfEmpty(r.Hostname), nullIfEmpty(r.Programname), nullIfEmpty(r.Search),
		r.Reason, r.CreatedBy, r.ExpiresAt,
	).Scan(&r.ID, &r.CreatedAt)
	if err != nil {
		return mute.Rule{}, fmt.Errorf("create mute rule: %w", err)
	}
	return r, nil
}

// SetMuteRuleExpiry sets a rule's expires_at, which also revives an ended
// rule, and returns the updated row. An unknown id gives pgx.ErrNoRows.
func (s *Store) SetMuteRuleExpiry(ctx context.Context, id int64, at time.Time) (mute.Rule, error) {
	const query = `UPDATE mute_rules SET expires_at = $2 WHERE id = $1 RETURNING ` + muteRuleColumns

	r, err := scanMuteRule(s.pool.QueryRow(ctx, query, id, at))
	if err != nil {
		return mute.Rule{}, fmt.Errorf("set mute rule %d expiry: %w", id, err)
	}
	return r, nil
}

// EndMuteRule ends a rule at the given time, or keeps its expiry when that is
// earlier, so a repeat end is a no-op. The row is kept. An unknown id gives
// pgx.ErrNoRows.
func (s *Store) EndMuteRule(ctx context.Context, id int64, at time.Time) error {
	const query = `UPDATE mute_rules SET expires_at = LEAST(expires_at, $2) WHERE id = $1`

	tag, err := s.pool.Exec(ctx, query, id, at)
	if err != nil {
		return fmt.Errorf("end mute rule %d: %w", id, err)
	}
	if tag.RowsAffected() == 0 {
		return pgx.ErrNoRows
	}
	return nil
}
