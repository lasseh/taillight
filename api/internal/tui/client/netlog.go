package client

import (
	"context"
	"fmt"
	"net/url"
	"strconv"
)

// NetlogFilter holds query parameters for netlog list/stream requests.
// Identical to SrvlogFilter since netlogs use the same schema.
type NetlogFilter = SrvlogFilter

// ListNetlogs fetches a paginated list of netlog events.
func (c *Client) ListNetlogs(ctx context.Context, filter NetlogFilter, cursor string, limit int) (*ListResponse[NetlogEvent], error) {
	params := filter.params()
	if cursor != "" {
		params.Set("cursor", cursor)
	}
	if limit > 0 {
		params.Set("limit", strconv.Itoa(limit))
	}

	var resp ListResponse[NetlogEvent]
	if err := c.getWithParams(ctx, "/api/v1/netlog/", params, &resp); err != nil {
		return nil, fmt.Errorf("list netlogs: %w", err)
	}
	return &resp, nil
}

// GetNetlog fetches a single netlog event by ID.
func (c *Client) GetNetlog(ctx context.Context, id int64) (*NetlogEvent, error) {
	var resp ItemResponse[NetlogEvent]
	if err := c.get(ctx, fmt.Sprintf("/api/v1/netlog/%d", id), &resp); err != nil {
		return nil, fmt.Errorf("get netlog %d: %w", id, err)
	}
	return &resp.Data, nil
}

// NetlogHosts fetches the list of known netlog hostnames.
func (c *Client) NetlogHosts(ctx context.Context) ([]string, error) {
	var resp ItemResponse[[]string]
	if err := c.get(ctx, "/api/v1/netlog/meta/hosts", &resp); err != nil {
		return nil, fmt.Errorf("netlog hosts: %w", err)
	}
	return resp.Data, nil
}

// NetlogPrograms fetches the list of known netlog program names.
func (c *Client) NetlogPrograms(ctx context.Context) ([]string, error) {
	var resp ItemResponse[[]string]
	if err := c.get(ctx, "/api/v1/netlog/meta/programs", &resp); err != nil {
		return nil, fmt.Errorf("netlog programs: %w", err)
	}
	return resp.Data, nil
}

// NetlogSummary fetches aggregated netlog statistics.
func (c *Client) NetlogSummary(ctx context.Context, rangeDur string) (*StatsSummary, error) {
	params := url.Values{}
	if rangeDur != "" {
		params.Set("range", rangeDur)
	}
	var resp ItemResponse[StatsSummary]
	if err := c.getWithParams(ctx, "/api/v1/netlog/stats/summary", params, &resp); err != nil {
		return nil, fmt.Errorf("netlog summary: %w", err)
	}
	return &resp.Data, nil
}
