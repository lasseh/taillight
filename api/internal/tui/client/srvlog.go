package client

import (
	"context"
	"fmt"
	"net/url"
	"strconv"
)

// SrvlogFilter holds query parameters for srvlog list/stream requests.
type SrvlogFilter struct {
	Hostname    string
	Programname string
	SeverityMax int // -1 means unset
	Facility    int // -1 means unset
	Search      string
}

// params encodes the filter as URL query parameters.
func (f SrvlogFilter) params() url.Values {
	v := url.Values{}
	if f.Hostname != "" {
		v.Set("hostname", f.Hostname)
	}
	if f.Programname != "" {
		v.Set("programname", f.Programname)
	}
	if f.SeverityMax >= 0 {
		v.Set("severity_max", strconv.Itoa(f.SeverityMax))
	}
	if f.Facility >= 0 {
		v.Set("facility", strconv.Itoa(f.Facility))
	}
	if f.Search != "" {
		v.Set("search", f.Search)
	}
	return v
}

// ListSrvlogs fetches a paginated list of srvlog events.
func (c *Client) ListSrvlogs(ctx context.Context, filter SrvlogFilter, cursor string, limit int) (*ListResponse[SrvlogEvent], error) {
	params := filter.params()
	if cursor != "" {
		params.Set("cursor", cursor)
	}
	if limit > 0 {
		params.Set("limit", strconv.Itoa(limit))
	}

	var resp ListResponse[SrvlogEvent]
	if err := c.getWithParams(ctx, "/api/v1/srvlog/", params, &resp); err != nil {
		return nil, fmt.Errorf("list srvlogs: %w", err)
	}
	return &resp, nil
}

// GetSrvlog fetches a single srvlog event by ID.
func (c *Client) GetSrvlog(ctx context.Context, id int64) (*SrvlogEvent, error) {
	var resp ItemResponse[SrvlogEvent]
	if err := c.get(ctx, fmt.Sprintf("/api/v1/srvlog/%d", id), &resp); err != nil {
		return nil, fmt.Errorf("get srvlog %d: %w", id, err)
	}
	return &resp.Data, nil
}

// SrvlogHosts fetches the list of known srvlog hostnames.
func (c *Client) SrvlogHosts(ctx context.Context) ([]string, error) {
	var resp ItemResponse[[]string]
	if err := c.get(ctx, "/api/v1/srvlog/meta/hosts", &resp); err != nil {
		return nil, fmt.Errorf("srvlog hosts: %w", err)
	}
	return resp.Data, nil
}

// SrvlogPrograms fetches the list of known srvlog program names.
func (c *Client) SrvlogPrograms(ctx context.Context) ([]string, error) {
	var resp ItemResponse[[]string]
	if err := c.get(ctx, "/api/v1/srvlog/meta/programs", &resp); err != nil {
		return nil, fmt.Errorf("srvlog programs: %w", err)
	}
	return resp.Data, nil
}

// SrvlogSummary fetches aggregated srvlog statistics.
func (c *Client) SrvlogSummary(ctx context.Context, rangeDur string) (*StatsSummary, error) {
	params := url.Values{}
	if rangeDur != "" {
		params.Set("range", rangeDur)
	}
	var resp ItemResponse[StatsSummary]
	if err := c.getWithParams(ctx, "/api/v1/srvlog/stats/summary", params, &resp); err != nil {
		return nil, fmt.Errorf("srvlog summary: %w", err)
	}
	return &resp.Data, nil
}

// SrvlogDevice fetches a device-level summary for a hostname.
func (c *Client) SrvlogDevice(ctx context.Context, hostname string) (*SrvlogDeviceSummary, error) {
	var resp ItemResponse[SrvlogDeviceSummary]
	if err := c.get(ctx, "/api/v1/srvlog/device/"+url.PathEscape(hostname), &resp); err != nil {
		return nil, fmt.Errorf("srvlog device %s: %w", hostname, err)
	}
	return &resp.Data, nil
}
