package postgres

import (
	"context"
	"errors"
	"fmt"
	"time"

	sq "github.com/Masterminds/squirrel"
	"github.com/jackc/pgx/v5"

	"github.com/lasseh/taillight/internal/notification"
)

// --- Channels ---

// ListNotificationChannels returns all notification channels.
func (s *Store) ListNotificationChannels(ctx context.Context) ([]notification.Channel, error) {
	query, args, err := psq.
		Select("id", "name", "type", "config", "enabled", "created_at", "updated_at").
		From("notification_channels").
		OrderBy("id ASC").
		ToSql()
	if err != nil {
		return nil, fmt.Errorf("build query: %w", err)
	}

	rows, err := s.pool.Query(ctx, query, args...)
	if err != nil {
		return nil, fmt.Errorf("list notification channels: %w", err)
	}
	defer rows.Close()

	var channels []notification.Channel
	for rows.Next() {
		var ch notification.Channel
		if err := rows.Scan(&ch.ID, &ch.Name, &ch.Type, &ch.Config, &ch.Enabled, &ch.CreatedAt, &ch.UpdatedAt); err != nil {
			return nil, fmt.Errorf("scan notification channel: %w", err)
		}
		channels = append(channels, ch)
	}
	return channels, rows.Err()
}

// GetNotificationChannel returns a single notification channel by ID.
func (s *Store) GetNotificationChannel(ctx context.Context, id int64) (notification.Channel, error) {
	query, args, err := psq.
		Select("id", "name", "type", "config", "enabled", "created_at", "updated_at").
		From("notification_channels").
		Where(sq.Eq{"id": id}).
		ToSql()
	if err != nil {
		return notification.Channel{}, fmt.Errorf("build query: %w", err)
	}

	var ch notification.Channel
	err = s.pool.QueryRow(ctx, query, args...).Scan(&ch.ID, &ch.Name, &ch.Type, &ch.Config, &ch.Enabled, &ch.CreatedAt, &ch.UpdatedAt)
	if errors.Is(err, pgx.ErrNoRows) {
		return notification.Channel{}, err
	}
	if err != nil {
		return notification.Channel{}, fmt.Errorf("get notification channel %d: %w", id, err)
	}
	return ch, nil
}

// CreateNotificationChannel inserts a new notification channel.
func (s *Store) CreateNotificationChannel(ctx context.Context, ch notification.Channel) (notification.Channel, error) {
	query, args, err := psq.
		Insert("notification_channels").
		Columns("name", "type", "config", "enabled").
		Values(ch.Name, ch.Type, ch.Config, ch.Enabled).
		Suffix("RETURNING id, created_at, updated_at").
		ToSql()
	if err != nil {
		return notification.Channel{}, fmt.Errorf("build query: %w", err)
	}

	err = s.pool.QueryRow(ctx, query, args...).Scan(&ch.ID, &ch.CreatedAt, &ch.UpdatedAt)
	if err != nil {
		return notification.Channel{}, fmt.Errorf("create notification channel: %w", err)
	}
	return ch, nil
}

// UpdateNotificationChannel updates an existing notification channel.
func (s *Store) UpdateNotificationChannel(ctx context.Context, id int64, ch notification.Channel) (notification.Channel, error) {
	query, args, err := psq.
		Update("notification_channels").
		Set("name", ch.Name).
		Set("type", ch.Type).
		Set("config", ch.Config).
		Set("enabled", ch.Enabled).
		Set("updated_at", time.Now()).
		Where(sq.Eq{"id": id}).
		Suffix("RETURNING id, name, type, config, enabled, created_at, updated_at").
		ToSql()
	if err != nil {
		return notification.Channel{}, fmt.Errorf("build query: %w", err)
	}

	var updated notification.Channel
	err = s.pool.QueryRow(ctx, query, args...).Scan(
		&updated.ID, &updated.Name, &updated.Type, &updated.Config,
		&updated.Enabled, &updated.CreatedAt, &updated.UpdatedAt,
	)
	if errors.Is(err, pgx.ErrNoRows) {
		return notification.Channel{}, err
	}
	if err != nil {
		return notification.Channel{}, fmt.Errorf("update notification channel %d: %w", id, err)
	}
	return updated, nil
}

// DeleteNotificationChannel deletes a notification channel by ID.
func (s *Store) DeleteNotificationChannel(ctx context.Context, id int64) error {
	query, args, err := psq.
		Delete("notification_channels").
		Where(sq.Eq{"id": id}).
		ToSql()
	if err != nil {
		return fmt.Errorf("build query: %w", err)
	}

	tag, err := s.pool.Exec(ctx, query, args...)
	if err != nil {
		return fmt.Errorf("delete notification channel %d: %w", id, err)
	}
	if tag.RowsAffected() == 0 {
		return pgx.ErrNoRows
	}
	return nil
}

// --- Rules ---

// ListNotificationRules returns all notification rules with their channel IDs.
func (s *Store) ListNotificationRules(ctx context.Context) ([]notification.Rule, error) {
	query, args, err := psq.
		Select(
			"id", "name", "enabled", "event_kind",
			"hostname", "programname", "severity", "severity_max",
			"facility", "syslogtag", "msgid",
			"service", "component", "host", "level", "search",
			"burst_window", "cooldown_seconds",
			"group_by", "max_cooldown_seconds",
			"created_at", "updated_at",
		).
		From("notification_rules").
		OrderBy("id ASC").
		ToSql()
	if err != nil {
		return nil, fmt.Errorf("build query: %w", err)
	}

	rows, err := s.pool.Query(ctx, query, args...)
	if err != nil {
		return nil, fmt.Errorf("list notification rules: %w", err)
	}
	defer rows.Close()

	var rules []notification.Rule
	for rows.Next() {
		var r notification.Rule
		var hostname, programname, syslogtag, msgid *string
		var service, component, host, level, search *string
		if err := rows.Scan(
			&r.ID, &r.Name, &r.Enabled, &r.EventKind,
			&hostname, &programname, &r.Severity, &r.SeverityMax,
			&r.Facility, &syslogtag, &msgid,
			&service, &component, &host, &level, &search,
			&r.BurstWindow, &r.CooldownSeconds,
			&r.GroupBy, &r.MaxCooldownSeconds,
			&r.CreatedAt, &r.UpdatedAt,
		); err != nil {
			return nil, fmt.Errorf("scan notification rule: %w", err)
		}
		r.Hostname = deref(hostname)
		r.Programname = deref(programname)
		r.SyslogTag = deref(syslogtag)
		r.MsgID = deref(msgid)
		r.Service = deref(service)
		r.Component = deref(component)
		r.Host = deref(host)
		r.Level = deref(level)
		r.Search = deref(search)
		rules = append(rules, r)
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}

	// Load channel IDs for each rule.
	if len(rules) > 0 {
		ruleIDs := make([]int64, len(rules))
		for i, r := range rules {
			ruleIDs[i] = r.ID
		}
		channelMap, err := s.loadRuleChannelIDs(ctx, ruleIDs)
		if err != nil {
			return nil, err
		}
		for i := range rules {
			rules[i].ChannelIDs = channelMap[rules[i].ID]
		}
	}

	return rules, nil
}

// GetNotificationRule returns a single notification rule by ID.
func (s *Store) GetNotificationRule(ctx context.Context, id int64) (notification.Rule, error) {
	query, args, err := psq.
		Select(
			"id", "name", "enabled", "event_kind",
			"hostname", "programname", "severity", "severity_max",
			"facility", "syslogtag", "msgid",
			"service", "component", "host", "level", "search",
			"burst_window", "cooldown_seconds",
			"group_by", "max_cooldown_seconds",
			"created_at", "updated_at",
		).
		From("notification_rules").
		Where(sq.Eq{"id": id}).
		ToSql()
	if err != nil {
		return notification.Rule{}, fmt.Errorf("build query: %w", err)
	}

	var r notification.Rule
	var hostname, programname, syslogtag, msgid *string
	var service, component, host, level, search *string
	err = s.pool.QueryRow(ctx, query, args...).Scan(
		&r.ID, &r.Name, &r.Enabled, &r.EventKind,
		&hostname, &programname, &r.Severity, &r.SeverityMax,
		&r.Facility, &syslogtag, &msgid,
		&service, &component, &host, &level, &search,
		&r.BurstWindow, &r.CooldownSeconds,
		&r.GroupBy, &r.MaxCooldownSeconds,
		&r.CreatedAt, &r.UpdatedAt,
	)
	if errors.Is(err, pgx.ErrNoRows) {
		return notification.Rule{}, err
	}
	if err != nil {
		return notification.Rule{}, fmt.Errorf("get notification rule %d: %w", id, err)
	}

	r.Hostname = deref(hostname)
	r.Programname = deref(programname)
	r.SyslogTag = deref(syslogtag)
	r.MsgID = deref(msgid)
	r.Service = deref(service)
	r.Component = deref(component)
	r.Host = deref(host)
	r.Level = deref(level)
	r.Search = deref(search)

	channelMap, err := s.loadRuleChannelIDs(ctx, []int64{id})
	if err != nil {
		return notification.Rule{}, err
	}
	r.ChannelIDs = channelMap[id]

	return r, nil
}

// CreateNotificationRule inserts a new rule and its channel associations in a transaction.
func (s *Store) CreateNotificationRule(ctx context.Context, r notification.Rule) (notification.Rule, error) {
	tx, err := s.pool.Begin(ctx)
	if err != nil {
		return notification.Rule{}, fmt.Errorf("begin tx: %w", err)
	}
	defer tx.Rollback(ctx) //nolint:errcheck // Rollback after commit is a no-op.

	query, args, err := psq.
		Insert("notification_rules").
		Columns(
			"name", "enabled", "event_kind",
			"hostname", "programname", "severity", "severity_max",
			"facility", "syslogtag", "msgid",
			"service", "component", "host", "level", "search",
			"burst_window", "cooldown_seconds",
			"group_by", "max_cooldown_seconds",
		).
		Values(
			r.Name, r.Enabled, r.EventKind,
			nullIfEmpty(r.Hostname), nullIfEmpty(r.Programname), r.Severity, r.SeverityMax,
			r.Facility, nullIfEmpty(r.SyslogTag), nullIfEmpty(r.MsgID),
			nullIfEmpty(r.Service), nullIfEmpty(r.Component), nullIfEmpty(r.Host),
			nullIfEmpty(r.Level), nullIfEmpty(r.Search),
			r.BurstWindow, r.CooldownSeconds,
			r.GroupBy, r.MaxCooldownSeconds,
		).
		Suffix("RETURNING id, created_at, updated_at").
		ToSql()
	if err != nil {
		return notification.Rule{}, fmt.Errorf("build query: %w", err)
	}

	err = tx.QueryRow(ctx, query, args...).Scan(&r.ID, &r.CreatedAt, &r.UpdatedAt)
	if err != nil {
		return notification.Rule{}, fmt.Errorf("insert notification rule: %w", err)
	}

	if err := insertRuleChannels(ctx, tx, r.ID, r.ChannelIDs); err != nil {
		return notification.Rule{}, err
	}

	if err := tx.Commit(ctx); err != nil {
		return notification.Rule{}, fmt.Errorf("commit tx: %w", err)
	}
	return r, nil
}

// UpdateNotificationRule updates a rule and replaces its channel associations.
func (s *Store) UpdateNotificationRule(ctx context.Context, id int64, r notification.Rule) (notification.Rule, error) {
	tx, err := s.pool.Begin(ctx)
	if err != nil {
		return notification.Rule{}, fmt.Errorf("begin tx: %w", err)
	}
	defer tx.Rollback(ctx) //nolint:errcheck // Rollback after commit is a no-op.

	query, args, err := psq.
		Update("notification_rules").
		Set("name", r.Name).
		Set("enabled", r.Enabled).
		Set("event_kind", r.EventKind).
		Set("hostname", nullIfEmpty(r.Hostname)).
		Set("programname", nullIfEmpty(r.Programname)).
		Set("severity", r.Severity).
		Set("severity_max", r.SeverityMax).
		Set("facility", r.Facility).
		Set("syslogtag", nullIfEmpty(r.SyslogTag)).
		Set("msgid", nullIfEmpty(r.MsgID)).
		Set("service", nullIfEmpty(r.Service)).
		Set("component", nullIfEmpty(r.Component)).
		Set("host", nullIfEmpty(r.Host)).
		Set("level", nullIfEmpty(r.Level)).
		Set("search", nullIfEmpty(r.Search)).
		Set("burst_window", r.BurstWindow).
		Set("cooldown_seconds", r.CooldownSeconds).
		Set("group_by", r.GroupBy).
		Set("max_cooldown_seconds", r.MaxCooldownSeconds).
		Set("updated_at", time.Now()).
		Where(sq.Eq{"id": id}).
		Suffix("RETURNING id, name, enabled, event_kind, created_at, updated_at").
		ToSql()
	if err != nil {
		return notification.Rule{}, fmt.Errorf("build query: %w", err)
	}

	err = tx.QueryRow(ctx, query, args...).Scan(&r.ID, &r.Name, &r.Enabled, &r.EventKind, &r.CreatedAt, &r.UpdatedAt)
	if errors.Is(err, pgx.ErrNoRows) {
		return notification.Rule{}, err
	}
	if err != nil {
		return notification.Rule{}, fmt.Errorf("update notification rule %d: %w", id, err)
	}

	// Replace channel associations.
	delQuery, delArgs, err := psq.Delete("notification_rule_channels").Where(sq.Eq{"rule_id": id}).ToSql()
	if err != nil {
		return notification.Rule{}, fmt.Errorf("build delete query: %w", err)
	}
	if _, err := tx.Exec(ctx, delQuery, delArgs...); err != nil {
		return notification.Rule{}, fmt.Errorf("delete rule channels: %w", err)
	}

	if err := insertRuleChannels(ctx, tx, id, r.ChannelIDs); err != nil {
		return notification.Rule{}, err
	}

	if err := tx.Commit(ctx); err != nil {
		return notification.Rule{}, fmt.Errorf("commit tx: %w", err)
	}
	return r, nil
}

// DeleteNotificationRule deletes a notification rule by ID.
func (s *Store) DeleteNotificationRule(ctx context.Context, id int64) error {
	query, args, err := psq.
		Delete("notification_rules").
		Where(sq.Eq{"id": id}).
		ToSql()
	if err != nil {
		return fmt.Errorf("build query: %w", err)
	}

	tag, err := s.pool.Exec(ctx, query, args...)
	if err != nil {
		return fmt.Errorf("delete notification rule %d: %w", id, err)
	}
	if tag.RowsAffected() == 0 {
		return pgx.ErrNoRows
	}
	return nil
}

// --- Notification Log ---

// InsertNotificationLog inserts a notification log entry.
func (s *Store) InsertNotificationLog(ctx context.Context, entry notification.LogEntry) error {
	query, args, err := psq.
		Insert("notification_log").
		Columns("rule_id", "channel_id", "event_kind", "event_id", "status", "reason", "event_count", "status_code", "duration_ms", "payload").
		Values(entry.RuleID, entry.ChannelID, entry.EventKind, entry.EventID, entry.Status, entry.Reason, entry.EventCount, entry.StatusCode, entry.DurationMS, entry.Payload).
		ToSql()
	if err != nil {
		return fmt.Errorf("build query: %w", err)
	}

	_, err = s.pool.Exec(ctx, query, args...)
	if err != nil {
		return fmt.Errorf("insert notification log: %w", err)
	}
	return nil
}

// ListNotificationLog returns notification log entries matching the filter.
func (s *Store) ListNotificationLog(ctx context.Context, f notification.LogFilter) ([]notification.LogEntry, error) {
	qb := psq.
		Select("id", "created_at", "rule_id", "channel_id", "event_kind", "event_id", "status", "reason", "event_count", "status_code", "duration_ms", "payload").
		From("notification_log").
		OrderBy("created_at DESC").
		Limit(500)

	if f.RuleID != nil {
		qb = qb.Where(sq.Eq{"rule_id": *f.RuleID})
	}
	if f.ChannelID != nil {
		qb = qb.Where(sq.Eq{"channel_id": *f.ChannelID})
	}
	if f.Status != "" {
		qb = qb.Where(sq.Eq{"status": f.Status})
	}
	if f.From != nil {
		qb = qb.Where(sq.GtOrEq{"created_at": *f.From})
	}
	if f.To != nil {
		qb = qb.Where(sq.LtOrEq{"created_at": *f.To})
	}

	query, args, err := qb.ToSql()
	if err != nil {
		return nil, fmt.Errorf("build query: %w", err)
	}

	rows, err := s.pool.Query(ctx, query, args...)
	if err != nil {
		return nil, fmt.Errorf("list notification log: %w", err)
	}
	defer rows.Close()

	var entries []notification.LogEntry
	for rows.Next() {
		var e notification.LogEntry
		if err := rows.Scan(
			&e.ID, &e.CreatedAt, &e.RuleID, &e.ChannelID,
			&e.EventKind, &e.EventID, &e.Status, &e.Reason,
			&e.EventCount, &e.StatusCode, &e.DurationMS, &e.Payload,
		); err != nil {
			return nil, fmt.Errorf("scan notification log: %w", err)
		}
		entries = append(entries, e)
	}
	return entries, rows.Err()
}

// --- Helpers ---

// loadRuleChannelIDs loads channel_ids for the given rule IDs from notification_rule_channels.
func (s *Store) loadRuleChannelIDs(ctx context.Context, ruleIDs []int64) (map[int64][]int64, error) {
	query, args, err := psq.
		Select("rule_id", "channel_id").
		From("notification_rule_channels").
		Where(sq.Eq{"rule_id": ruleIDs}).
		OrderBy("channel_id ASC").
		ToSql()
	if err != nil {
		return nil, fmt.Errorf("build query: %w", err)
	}

	rows, err := s.pool.Query(ctx, query, args...)
	if err != nil {
		return nil, fmt.Errorf("load rule channel ids: %w", err)
	}
	defer rows.Close()

	m := make(map[int64][]int64)
	for rows.Next() {
		var ruleID, channelID int64
		if err := rows.Scan(&ruleID, &channelID); err != nil {
			return nil, fmt.Errorf("scan rule channel: %w", err)
		}
		m[ruleID] = append(m[ruleID], channelID)
	}
	return m, rows.Err()
}

// insertRuleChannels inserts channel associations for a rule within a transaction.
func insertRuleChannels(ctx context.Context, tx pgx.Tx, ruleID int64, channelIDs []int64) error {
	for _, chID := range channelIDs {
		query, args, err := psq.
			Insert("notification_rule_channels").
			Columns("rule_id", "channel_id").
			Values(ruleID, chID).
			ToSql()
		if err != nil {
			return fmt.Errorf("build rule channel query: %w", err)
		}
		if _, err := tx.Exec(ctx, query, args...); err != nil {
			return fmt.Errorf("insert rule channel (%d, %d): %w", ruleID, chID, err)
		}
	}
	return nil
}

// deref returns the value of a string pointer or empty string if nil.
func deref(s *string) string {
	if s == nil {
		return ""
	}
	return *s
}

// nullIfEmpty returns nil if s is empty, otherwise returns a pointer to s.
func nullIfEmpty(s string) *string {
	if s == "" {
		return nil
	}
	return &s
}
