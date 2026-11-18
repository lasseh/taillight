// Package openai provides a minimal HTTP client for OpenAI-compatible chat
// servers (LiteLLM, vLLM, the OpenAI API itself). It takes and returns the
// ollama package's chat types, so the analyzer drives either backend through
// one interface.
package openai

import (
	"bytes"
	"context"
	"crypto/tls"
	"crypto/x509"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"os"
	"strings"
	"time"

	"github.com/lasseh/taillight/internal/ollama"
)

// ErrEmptyReply is returned by Chat when the server reports a finished reply
// with no text in it. A reasoning model can spend its whole reply on
// reasoning, which is not part of the content.
var ErrEmptyReply = errors.New("model returned an empty reply")

// finishReasonLength is the finish_reason of a reply that ran out of context
// (or reached max_tokens, which this client never sets).
const finishReasonLength = "length"

// Config configures a Client.
type Config struct {
	BaseURL string        // Server base URL, without /v1.
	APIKey  string        // Sent as "Authorization: Bearer <key>" on every call.
	CAFile  string        // PEM bundle trusted in addition to the system roots. Empty = system roots only.
	Timeout time.Duration // Bounds each HTTP request. Zero leaves only the request context.
}

// Client is a minimal HTTP client for the OpenAI chat completions API.
type Client struct {
	baseURL    string
	apiKey     string
	httpClient *http.Client
}

// chatRequest is the request body for POST /v1/chat/completions. It carries
// no max_tokens, so the server allows whatever context the prompt leaves.
type chatRequest struct {
	Model    string               `json:"model"`
	Messages []ollama.ChatMessage `json:"messages"`
	// Temperature is always sent: left out, the server uses its own default
	// instead of 0.
	Temperature float64 `json:"temperature"`
	Stream      bool    `json:"stream"`
}

// chatResponse is the part of a chat completion this client reads. A
// reasoning model's reasoning_content is left out on purpose.
type chatResponse struct {
	Choices []struct {
		Message struct {
			Content string `json:"content"`
		} `json:"message"`
		FinishReason string `json:"finish_reason"`
	} `json:"choices"`
	Usage struct {
		PromptTokens     int `json:"prompt_tokens"`
		CompletionTokens int `json:"completion_tokens"`
	} `json:"usage"`
}

// statusError is a non-200 reply. It unwraps to *ollama.StatusError, the type
// the analyzer and the worker match on, and words the message for this
// backend.
type statusError struct {
	err *ollama.StatusError
}

func (e *statusError) Error() string {
	return fmt.Sprintf("server returned status %d: %s", e.err.StatusCode, e.err.Body)
}

func (e *statusError) Unwrap() error { return e.err }

// New creates a client. It fails when the base URL or key is missing, or
// when CAFile is set and holds no readable certificate.
func New(cfg Config) (*Client, error) {
	if cfg.BaseURL == "" {
		return nil, errors.New("openai: base url is required")
	}
	if cfg.APIKey == "" {
		return nil, errors.New("openai: api key is required")
	}

	httpClient := &http.Client{Timeout: cfg.Timeout}
	if cfg.CAFile != "" {
		pool, err := x509.SystemCertPool()
		if err != nil || pool == nil {
			pool = x509.NewCertPool()
		}
		pem, err := os.ReadFile(cfg.CAFile)
		if err != nil {
			return nil, fmt.Errorf("read ca_file: %w", err)
		}
		if !pool.AppendCertsFromPEM(pem) {
			return nil, fmt.Errorf("ca_file %s: no certificates parsed", cfg.CAFile)
		}
		transport, ok := http.DefaultTransport.(*http.Transport)
		if !ok {
			return nil, errors.New("openai: http.DefaultTransport is not an *http.Transport")
		}
		transport = transport.Clone()
		transport.TLSClientConfig = &tls.Config{RootCAs: pool, MinVersion: tls.VersionTLS12}
		httpClient.Transport = transport
	}

	return &Client{
		baseURL:    strings.TrimRight(cfg.BaseURL, "/"),
		apiKey:     cfg.APIKey,
		httpClient: httpClient,
	}, nil
}

// Chat sends a non-streaming chat completion and returns it as an
// ollama.ChatResponse: finish_reason "length" becomes DoneReasonLength, and
// the usage counts become PromptEvalCount and EvalCount. Of the request it
// sends the model, the messages and the temperature; num_ctx, truncate and
// shift have no counterpart, because the server owns the context size and
// refuses a prompt that does not fit.
func (c *Client) Chat(ctx context.Context, req ollama.ChatRequest) (ollama.ChatResponse, error) {
	body, err := json.Marshal(chatRequest{
		Model:       req.Model,
		Messages:    req.Messages,
		Temperature: req.Options.Temperature,
	})
	if err != nil {
		return ollama.ChatResponse{}, fmt.Errorf("marshal chat request: %w", err)
	}

	httpReq, err := http.NewRequestWithContext(ctx, http.MethodPost, c.baseURL+"/v1/chat/completions", bytes.NewReader(body))
	if err != nil {
		return ollama.ChatResponse{}, fmt.Errorf("create chat request: %w", err)
	}
	httpReq.Header.Set("Content-Type", "application/json")

	resp, err := c.do(httpReq)
	if err != nil {
		return ollama.ChatResponse{}, fmt.Errorf("chat request: %w", err)
	}
	defer resp.Body.Close() //nolint:errcheck // response body close error is not actionable.

	var chatResp chatResponse
	if err := json.NewDecoder(resp.Body).Decode(&chatResp); err != nil {
		return ollama.ChatResponse{}, fmt.Errorf("decode chat response: %w", err)
	}
	if len(chatResp.Choices) == 0 {
		return ollama.ChatResponse{}, fmt.Errorf("%w: no choices", ErrEmptyReply)
	}
	choice := chatResp.Choices[0]

	out := ollama.ChatResponse{
		Message:         ollama.ChatMessage{Role: "assistant", Content: choice.Message.Content},
		PromptEvalCount: chatResp.Usage.PromptTokens,
		EvalCount:       chatResp.Usage.CompletionTokens,
		DoneReason:      choice.FinishReason,
	}
	// A cut-off reply is reported as one even when it is empty: the caller
	// treats it as a context overflow, which is what it is.
	if choice.FinishReason == finishReasonLength {
		out.DoneReason = ollama.DoneReasonLength
		return out, nil
	}
	if strings.TrimSpace(choice.Message.Content) == "" {
		return ollama.ChatResponse{}, fmt.Errorf("%w (finish_reason %q, %d completion tokens)",
			ErrEmptyReply, choice.FinishReason, chatResp.Usage.CompletionTokens)
	}
	return out, nil
}

// Ping checks that the server is reachable and accepts the key, by hitting
// GET /v1/models.
func (c *Client) Ping(ctx context.Context) error {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, c.baseURL+"/v1/models", nil)
	if err != nil {
		return fmt.Errorf("create ping request: %w", err)
	}

	resp, err := c.do(req)
	if err != nil {
		return fmt.Errorf("ping: %w", err)
	}
	defer resp.Body.Close() //nolint:errcheck // response body close error is not actionable.
	return nil
}

// do sends req with the key and returns the response when it is a 200. Any
// other status is returned as an error that unwraps to *ollama.StatusError,
// carrying up to 1KB of the body for the server log. The key is cut out of
// that body in case the server echoes it.
func (c *Client) do(req *http.Request) (*http.Response, error) {
	req.Header.Set("Authorization", "Bearer "+c.apiKey)

	resp, err := c.httpClient.Do(req)
	if err != nil {
		return nil, err
	}
	if resp.StatusCode != http.StatusOK {
		defer resp.Body.Close() //nolint:errcheck // response body close error is not actionable.
		respBody, _ := io.ReadAll(io.LimitReader(resp.Body, 1024))
		return nil, &statusError{err: &ollama.StatusError{
			StatusCode: resp.StatusCode,
			Body:       strings.ReplaceAll(string(respBody), c.apiKey, "[redacted]"),
		}}
	}
	return resp, nil
}
