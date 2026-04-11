package client

import (
	"context"
)

// Health checks connectivity to the API server.
func (c *Client) Health(ctx context.Context) error {
	return c.get(ctx, "/health", nil)
}

// Me returns the authenticated user's info.
func (c *Client) Me(ctx context.Context) (*UserInfo, error) {
	var resp ItemResponse[UserInfo]
	if err := c.get(ctx, "/api/v1/auth/me", &resp); err != nil {
		return nil, err
	}
	return &resp.Data, nil
}
