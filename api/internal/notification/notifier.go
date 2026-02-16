// Package notification provides a pluggable notification engine that alerts
// operators when log events match configurable rules.
package notification

import (
	"context"
	"encoding/json"
	"time"

	"github.com/lasseh/taillight/internal/model"
)

// EventKind identifies the source event type.
type EventKind string

// Event kind constants.
const (
	EventKindSyslog EventKind = "syslog"
	EventKindAppLog EventKind = "applog"
)

// ChannelType identifies a notification backend.
type ChannelType string

// Channel type constants.
const (
	ChannelTypeSlack   ChannelType = "slack"
	ChannelTypeWebhook ChannelType = "webhook"
)

// Channel represents a configured notification destination.
type Channel struct {
	ID        int64           `json:"id"`
	Name      string          `json:"name"`
	Type      ChannelType     `json:"type"`
	Config    json.RawMessage `json:"config"`
	Enabled   bool            `json:"enabled"`
	CreatedAt time.Time       `json:"created_at"`
	UpdatedAt time.Time       `json:"updated_at"`
}

// Rule defines an alert condition with filter criteria and notification behavior.
type Rule struct {
	ID        int64     `json:"id"`
	Name      string    `json:"name"`
	Enabled   bool      `json:"enabled"`
	EventKind EventKind `json:"event_kind"`

	// Syslog filter fields.
	Hostname    string `json:"hostname,omitempty"`
	Programname string `json:"programname,omitempty"`
	Severity    *int   `json:"severity,omitempty"`
	SeverityMax *int   `json:"severity_max,omitempty"`
	Facility    *int   `json:"facility,omitempty"`
	SyslogTag   string `json:"syslogtag,omitempty"`
	MsgID       string `json:"msgid,omitempty"`

	// AppLog filter fields.
	Service   string `json:"service,omitempty"`
	Component string `json:"component,omitempty"`
	Host      string `json:"host,omitempty"`
	Level     string `json:"level,omitempty"`

	// Shared filter field.
	Search string `json:"search,omitempty"`

	// Notification behavior.
	ChannelIDs      []int64 `json:"channel_ids"`
	BurstWindow     int     `json:"burst_window"`
	CooldownSeconds int     `json:"cooldown_seconds"`

	CreatedAt time.Time `json:"created_at"`
	UpdatedAt time.Time `json:"updated_at"`
}

// Payload carries the notification content to backends.
type Payload struct {
	Kind        EventKind          `json:"kind"`
	RuleName    string             `json:"rule_name"`
	Timestamp   time.Time          `json:"timestamp"`
	EventCount  int                `json:"event_count"`
	SyslogEvent *model.SyslogEvent `json:"syslog_event,omitempty"`
	AppLogEvent *model.AppLogEvent `json:"applog_event,omitempty"`
}

// SendResult captures the outcome of a backend Send call.
type SendResult struct {
	Success    bool
	StatusCode int
	Error      error
	Duration   time.Duration
}

// Notifier is the interface every notification backend must implement.
type Notifier interface {
	// Send delivers a notification to the given channel.
	Send(ctx context.Context, channel Channel, payload Payload) SendResult

	// Validate checks that a channel's config is valid for this backend.
	Validate(channel Channel) error
}

// LogEntry represents a row in the notification_log audit table.
type LogEntry struct {
	ID         int64           `json:"id"`
	CreatedAt  time.Time       `json:"created_at"`
	RuleID     int64           `json:"rule_id"`
	ChannelID  int64           `json:"channel_id"`
	EventKind  string          `json:"event_kind"`
	EventID    int64           `json:"event_id"`
	Status     string          `json:"status"`
	Reason     *string         `json:"reason,omitempty"`
	EventCount int             `json:"event_count"`
	StatusCode *int            `json:"status_code,omitempty"`
	DurationMS int             `json:"duration_ms"`
	Payload    json.RawMessage `json:"payload,omitempty"`
}

// LogFilter holds query parameters for listing notification log entries.
type LogFilter struct {
	RuleID    *int64
	ChannelID *int64
	Status    string
	From      *time.Time
	To        *time.Time
}

// Store defines the data access interface used by the notification engine.
type Store interface {
	ListNotificationRules(ctx context.Context) ([]Rule, error)
	ListNotificationChannels(ctx context.Context) ([]Channel, error)
	InsertNotificationLog(ctx context.Context, entry LogEntry) error
}

// Config holds configuration for the notification engine.
type Config struct {
	Enabled             bool
	RuleRefreshInterval time.Duration
	DispatchWorkers     int
	DispatchBuffer      int
	DefaultBurstWindow  time.Duration
	DefaultCooldown     time.Duration
	SendTimeout         time.Duration
}
