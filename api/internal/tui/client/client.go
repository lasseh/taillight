// Package client provides a typed HTTP client for the Taillight API.
package client

import (
	"context"
	"crypto/tls"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"time"
)

// ErrUnauthorized is returned when the API responds with 401 or 403.
// Callers can check with errors.Is to show persistent auth errors and stop
// automatic retries.
var ErrUnauthorized = errors.New("unauthorized: check API key")

// Config holds connection settings for the API client.
type Config struct {
	BaseURL       string
	APIKey        string
	TLSSkipVerify bool
}

// Client is a typed HTTP client for the Taillight REST + SSE API.
type Client struct {
	http    *http.Client
	baseURL string
	apiKey  string
}

// New creates a new API client.
func New(cfg Config) *Client {
	transport := http.DefaultTransport.(*http.Transport).Clone()
	if cfg.TLSSkipVerify {
		transport.TLSClientConfig = &tls.Config{InsecureSkipVerify: true} //nolint:gosec // user-requested skip
	}

	return &Client{
		http: &http.Client{
			Transport: transport,
			Timeout:   30 * time.Second,
		},
		baseURL: cfg.BaseURL,
		apiKey:  cfg.APIKey,
	}
}

// newRequest builds an authenticated HTTP request.
func (c *Client) newRequest(ctx context.Context, method, path string, body io.Reader) (*http.Request, error) {
	u, err := url.JoinPath(c.baseURL, path)
	if err != nil {
		return nil, fmt.Errorf("build URL: %w", err)
	}

	req, err := http.NewRequestWithContext(ctx, method, u, body)
	if err != nil {
		return nil, err
	}

	if c.apiKey != "" {
		req.Header.Set("Authorization", "Bearer "+c.apiKey)
	}
	req.Header.Set("Accept", "application/json")
	return req, nil
}

// do executes a request and decodes the JSON response into dst.
func (c *Client) do(req *http.Request, dst any) error {
	resp, err := c.http.Do(req)
	if err != nil {
		return fmt.Errorf("request %s %s: %w", req.Method, req.URL.String(), err)
	}
	defer resp.Body.Close() //nolint:errcheck // response body close rarely fails

	if resp.StatusCode == http.StatusUnauthorized || resp.StatusCode == http.StatusForbidden {
		return fmt.Errorf("%s %s: %w", req.Method, req.URL.String(), ErrUnauthorized)
	}

	if resp.StatusCode >= 400 {
		body, _ := io.ReadAll(resp.Body)
		return fmt.Errorf("API %s %s: %d %s", req.Method, req.URL.String(), resp.StatusCode, string(body))
	}

	if dst != nil {
		if err := json.NewDecoder(resp.Body).Decode(dst); err != nil {
			return fmt.Errorf("decode response: %w", err)
		}
	}
	return nil
}

// get is a convenience method for GET requests.
func (c *Client) get(ctx context.Context, path string, dst any) error {
	req, err := c.newRequest(ctx, http.MethodGet, path, nil)
	if err != nil {
		return err
	}
	return c.do(req, dst)
}

// getWithParams is a convenience method for GET requests with query parameters.
func (c *Client) getWithParams(ctx context.Context, path string, params url.Values, dst any) error {
	req, err := c.newRequest(ctx, http.MethodGet, path, nil)
	if err != nil {
		return err
	}
	req.URL.RawQuery = params.Encode()
	return c.do(req, dst)
}

// BaseURL returns the configured base URL.
func (c *Client) BaseURL() string {
	return c.baseURL
}

// SSEClient returns an HTTP client without timeout for long-lived SSE
// connections.
func (c *Client) SSEClient() *http.Client {
	transport := http.DefaultTransport.(*http.Transport).Clone()
	if c.http.Transport != nil {
		if t, ok := c.http.Transport.(*http.Transport); ok {
			transport = t.Clone()
		}
	}
	return &http.Client{
		Transport: transport,
		// No timeout — SSE streams are long-lived.
	}
}

// APIKey returns the configured API key for use in SSE requests.
func (c *Client) APIKey() string {
	return c.apiKey
}
