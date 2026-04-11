package client

import (
	"context"
	"encoding/json"
	"fmt"
	"net/url"
	"time"
)

// AppLogDeviceSummary represents an applog device-level summary.
type AppLogDeviceSummary struct {
	Host           string         `json:"host"`
	LastSeenAt     *time.Time     `json:"last_seen_at"`
	TotalCount     int64          `json:"total_count"`
	ErrorCount     int64          `json:"error_count"`
	LevelBreakdown []LevelCount   `json:"level_breakdown"`
	TopMessages    []AppLogTopMsg `json:"top_messages"`
	ErrorLogs      []AppLogEvent  `json:"error_logs"`
}

// LevelCount is a level bucket in a breakdown.
type LevelCount struct {
	Level string  `json:"level"`
	Count int64   `json:"count"`
	Pct   float64 `json:"pct"`
}

// AppLogTopMsg is a frequently recurring applog message.
type AppLogTopMsg struct {
	Pattern  string          `json:"pattern"`
	Sample   string          `json:"sample"`
	Count    int64           `json:"count"`
	LatestID int64           `json:"latest_id"`
	LatestAt time.Time       `json:"latest_at"`
	Level    string          `json:"level"`
	Attrs    json.RawMessage `json:"attrs,omitempty"`
}

// AppLogDevice fetches an applog device-level summary.
func (c *Client) AppLogDevice(ctx context.Context, hostname string) (*AppLogDeviceSummary, error) {
	var resp ItemResponse[AppLogDeviceSummary]
	if err := c.get(ctx, "/api/v1/applog/device/"+url.PathEscape(hostname), &resp); err != nil {
		return nil, fmt.Errorf("applog device %s: %w", hostname, err)
	}
	return &resp.Data, nil
}

// NetlogDevice fetches a netlog device-level summary.
func (c *Client) NetlogDevice(ctx context.Context, hostname string) (*SrvlogDeviceSummary, error) {
	var resp ItemResponse[SrvlogDeviceSummary]
	if err := c.get(ctx, "/api/v1/netlog/device/"+url.PathEscape(hostname), &resp); err != nil {
		return nil, fmt.Errorf("netlog device %s: %w", hostname, err)
	}
	return &resp.Data, nil
}
