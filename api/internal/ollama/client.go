// Package ollama provides a minimal HTTP client for the Ollama API.
package ollama

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"time"
)

// ChatMessage represents a single message in a chat conversation.
type ChatMessage struct {
	Role    string `json:"role"`
	Content string `json:"content"`
}

// Options controls inference parameters.
type Options struct {
	Temperature float64 `json:"temperature,omitempty"`
	NumCtx      int     `json:"num_ctx,omitempty"`
}

// ChatRequest is the request body for POST /api/chat.
type ChatRequest struct {
	Model    string        `json:"model"`
	Messages []ChatMessage `json:"messages"`
	Stream   bool          `json:"stream"`
	Options  Options       `json:"options"`
}

// ChatResponse is the response body from POST /api/chat (non-streaming).
type ChatResponse struct {
	Message         ChatMessage `json:"message"`
	TotalDuration   int64       `json:"total_duration"`
	PromptEvalCount int         `json:"prompt_eval_count"`
	EvalCount       int         `json:"eval_count"`
}

// StatusError is returned by Chat when Ollama responds with a non-200 status.
// Body carries up to 1KB of the upstream response for server-side diagnostics;
// callers that surface errors to clients must not include it.
type StatusError struct {
	StatusCode int
	Body       string
}

func (e *StatusError) Error() string {
	return fmt.Sprintf("ollama returned status %d: %s", e.StatusCode, e.Body)
}

// Client is a minimal HTTP client for the Ollama API.
type Client struct {
	baseURL    string
	httpClient *http.Client
}

// defaultChatTimeout is used when New is called with a zero/negative timeout,
// e.g. from tests or other non-config call sites. Picked to cover weekly
// reports on commodity-GPU Ollama hosts without surprising callers that
// forget to set the value.
const defaultChatTimeout = 2 * time.Hour

// New creates a new Ollama client. timeout bounds each HTTP request; pass
// 0 to use defaultChatTimeout.
func New(baseURL string, timeout time.Duration) *Client {
	if timeout <= 0 {
		timeout = defaultChatTimeout
	}
	return &Client{
		baseURL: baseURL,
		httpClient: &http.Client{
			Timeout: timeout,
		},
	}
}

// Chat sends a non-streaming chat request and returns the response.
func (c *Client) Chat(ctx context.Context, req ChatRequest) (ChatResponse, error) {
	req.Stream = false

	body, err := json.Marshal(req)
	if err != nil {
		return ChatResponse{}, fmt.Errorf("marshal chat request: %w", err)
	}

	httpReq, err := http.NewRequestWithContext(ctx, http.MethodPost, c.baseURL+"/api/chat", bytes.NewReader(body))
	if err != nil {
		return ChatResponse{}, fmt.Errorf("create chat request: %w", err)
	}
	httpReq.Header.Set("Content-Type", "application/json")

	resp, err := c.httpClient.Do(httpReq)
	if err != nil {
		return ChatResponse{}, fmt.Errorf("ollama chat request: %w", err)
	}
	defer resp.Body.Close() //nolint:errcheck

	if resp.StatusCode != http.StatusOK {
		respBody, _ := io.ReadAll(io.LimitReader(resp.Body, 1024))
		return ChatResponse{}, &StatusError{StatusCode: resp.StatusCode, Body: string(respBody)}
	}

	var chatResp ChatResponse
	if err := json.NewDecoder(resp.Body).Decode(&chatResp); err != nil {
		return ChatResponse{}, fmt.Errorf("decode chat response: %w", err)
	}

	return chatResp, nil
}

// Ping checks Ollama availability by hitting GET /api/tags.
func (c *Client) Ping(ctx context.Context) error {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, c.baseURL+"/api/tags", nil)
	if err != nil {
		return fmt.Errorf("create ping request: %w", err)
	}

	resp, err := c.httpClient.Do(req)
	if err != nil {
		return fmt.Errorf("ollama ping: %w", err)
	}
	defer resp.Body.Close() //nolint:errcheck

	if resp.StatusCode != http.StatusOK {
		return fmt.Errorf("ollama ping returned status %d", resp.StatusCode)
	}
	return nil
}
