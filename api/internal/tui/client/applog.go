package client

import (
	"context"
	"fmt"
	"net/url"
	"strconv"
)

// AppLogFilter holds query parameters for applog list/stream requests.
type AppLogFilter struct {
	Service   string
	Component string
	Host      string
	Level     string // minimum level: "WARN" returns WARN+ERROR+FATAL
	Search    string
}

// Params encodes the filter as URL query parameters.
func (f AppLogFilter) Params() url.Values {
	v := url.Values{}
	if f.Service != "" {
		v.Set("service", f.Service)
	}
	if f.Component != "" {
		v.Set("component", f.Component)
	}
	if f.Host != "" {
		v.Set("host", f.Host)
	}
	if f.Level != "" {
		v.Set("level", f.Level)
	}
	if f.Search != "" {
		v.Set("search", f.Search)
	}
	return v
}

// ListAppLogs fetches a paginated list of applog events.
func (c *Client) ListAppLogs(ctx context.Context, filter AppLogFilter, cursor string, limit int) (*ListResponse[AppLogEvent], error) {
	params := filter.Params()
	if cursor != "" {
		params.Set("cursor", cursor)
	}
	if limit > 0 {
		params.Set("limit", strconv.Itoa(limit))
	}

	var resp ListResponse[AppLogEvent]
	if err := c.getWithParams(ctx, "/api/v1/applog/", params, &resp); err != nil {
		return nil, fmt.Errorf("list applogs: %w", err)
	}
	return &resp, nil
}

// GetAppLog fetches a single applog event by ID.
func (c *Client) GetAppLog(ctx context.Context, id int64) (*AppLogEvent, error) {
	var resp ItemResponse[AppLogEvent]
	if err := c.get(ctx, fmt.Sprintf("/api/v1/applog/%d", id), &resp); err != nil {
		return nil, fmt.Errorf("get applog %d: %w", id, err)
	}
	return &resp.Data, nil
}

// AppLogServices fetches the list of known applog service names.
func (c *Client) AppLogServices(ctx context.Context) ([]string, error) {
	var resp ItemResponse[[]string]
	if err := c.get(ctx, "/api/v1/applog/meta/services", &resp); err != nil {
		return nil, fmt.Errorf("applog services: %w", err)
	}
	return resp.Data, nil
}

// AppLogComponents fetches the list of known applog component names.
func (c *Client) AppLogComponents(ctx context.Context) ([]string, error) {
	var resp ItemResponse[[]string]
	if err := c.get(ctx, "/api/v1/applog/meta/components", &resp); err != nil {
		return nil, fmt.Errorf("applog components: %w", err)
	}
	return resp.Data, nil
}

// AppLogHosts fetches the list of known applog hostnames.
func (c *Client) AppLogHosts(ctx context.Context) ([]string, error) {
	var resp ItemResponse[[]string]
	if err := c.get(ctx, "/api/v1/applog/meta/hosts", &resp); err != nil {
		return nil, fmt.Errorf("applog hosts: %w", err)
	}
	return resp.Data, nil
}

// AppLogSummary fetches aggregated applog statistics.
func (c *Client) AppLogSummary(ctx context.Context, rangeDur string) (*AppLogStatsSummary, error) {
	params := url.Values{}
	if rangeDur != "" {
		params.Set("range", rangeDur)
	}
	var resp ItemResponse[AppLogStatsSummary]
	if err := c.getWithParams(ctx, "/api/v1/applog/stats/summary", params, &resp); err != nil {
		return nil, fmt.Errorf("applog summary: %w", err)
	}
	return &resp.Data, nil
}
