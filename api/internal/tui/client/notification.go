package client

import (
	"context"
	"encoding/json"
	"fmt"
	"time"
)

// Channel is a notification channel (slack, webhook, email, ntfy).
type Channel struct {
	ID        int64           `json:"id"`
	Name      string          `json:"name"`
	Type      string          `json:"type"`
	Config    json.RawMessage `json:"config"`
	Enabled   bool            `json:"enabled"`
	CreatedAt time.Time       `json:"created_at"`
	UpdatedAt time.Time       `json:"updated_at"`
}

// Rule is a notification alert rule.
type Rule struct {
	ID           int64   `json:"id"`
	Name         string  `json:"name"`
	Enabled      bool    `json:"enabled"`
	EventKind    string  `json:"event_kind"`
	Hostname     string  `json:"hostname,omitempty"`
	Programname  string  `json:"programname,omitempty"`
	Severity     *int    `json:"severity,omitempty"`
	SeverityMax  *int    `json:"severity_max,omitempty"`
	Search       string  `json:"search,omitempty"`
	Service      string  `json:"service,omitempty"`
	Component    string  `json:"component,omitempty"`
	Level        string  `json:"level,omitempty"`
	ChannelIDs   []int64 `json:"channel_ids"`
	GroupBy      string  `json:"group_by,omitempty"`
	SilenceMS    int     `json:"silence_ms"`
	SilenceMaxMS int     `json:"silence_max_ms"`
	CoalesceMS   int     `json:"coalesce_ms"`
}

// ListNotificationChannels fetches all notification channels.
func (c *Client) ListNotificationChannels(ctx context.Context) ([]Channel, error) {
	var resp ItemResponse[[]Channel]
	if err := c.get(ctx, "/api/v1/notifications/channels", &resp); err != nil {
		return nil, fmt.Errorf("list channels: %w", err)
	}
	return resp.Data, nil
}

// ListNotificationRules fetches all notification rules.
func (c *Client) ListNotificationRules(ctx context.Context) ([]Rule, error) {
	var resp ItemResponse[[]Rule]
	if err := c.get(ctx, "/api/v1/notifications/rules", &resp); err != nil {
		return nil, fmt.Errorf("list rules: %w", err)
	}
	return resp.Data, nil
}
