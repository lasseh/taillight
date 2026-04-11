package client

import (
	"context"
	"fmt"
	"net/url"
)

// Hosts fetches the host inventory with stats.
func (c *Client) Hosts(ctx context.Context, rangeDur string) ([]HostEntry, error) {
	params := url.Values{}
	if rangeDur != "" {
		params.Set("range", rangeDur)
	}
	var resp ItemResponse[[]HostEntry]
	if err := c.getWithParams(ctx, "/api/v1/srvlog/stats/hosts", params, &resp); err != nil {
		return nil, fmt.Errorf("hosts: %w", err)
	}
	return resp.Data, nil
}

// Volume fetches time-bucketed event counts for a feed.
func (c *Client) Volume(ctx context.Context, feed, interval, rangeDur string) ([]VolumeBucket, error) {
	params := url.Values{}
	if interval != "" {
		params.Set("interval", interval)
	}
	if rangeDur != "" {
		params.Set("range", rangeDur)
	}
	path := fmt.Sprintf("/api/v1/%s/stats/volume", feed)
	var resp ItemResponse[[]VolumeBucket]
	if err := c.getWithParams(ctx, path, params, &resp); err != nil {
		return nil, fmt.Errorf("%s volume: %w", feed, err)
	}
	return resp.Data, nil
}
