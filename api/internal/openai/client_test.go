package openai

import (
	"context"
	"crypto/x509"
	"encoding/json"
	"encoding/pem"
	"errors"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/lasseh/taillight/internal/ollama"
)

const testKey = "sk-test-0123456789abcdef"

// contextRefusal is the gateway's answer to a 160k-token prompt, as measured
// on 2026-11-18. A raw string, so \n stays the two bytes it is on the wire.
const contextRefusal = `{"error":{"message":"litellm.BadRequestError: OpenAIException - Input length (160068) exceeds model's maximum context length (65536).\n\nLiteLLM: model group 'general' failed with the error above. No fallback was attempted.","type":"BadRequestError","param":null,"code":"400"}}`

// wrongKey is the gateway's answer to a key it does not know.
const wrongKey = `{"error":{"message":"Authentication Error, Invalid API key provided.","type":"token_not_found_in_db","param":"key","code":"401"}}`

func newTestClient(t *testing.T, baseURL string) *Client {
	t.Helper()
	c, err := New(Config{BaseURL: baseURL, APIKey: testKey, Timeout: 5 * time.Second})
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	return c
}

// serve starts a server that answers every request with status and body.
func serve(t *testing.T, status int, body string) *httptest.Server {
	t.Helper()
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(status)
		_, _ = w.Write([]byte(body))
	}))
	t.Cleanup(srv.Close)
	return srv
}

func TestNewRequiresURLAndKey(t *testing.T) {
	tests := []struct {
		name string
		cfg  Config
	}{
		{"no base url", Config{APIKey: testKey}},
		{"no key", Config{BaseURL: "https://gateway.example"}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if _, err := New(tt.cfg); err == nil {
				t.Fatal("New: want error")
			}
		})
	}
}

// TestChatRequest pins what goes on the wire: the path, the key, and a body
// of exactly model, messages, temperature and stream. The Ollama-only fields
// of the request are dropped, and no max_tokens is added.
func TestChatRequest(t *testing.T) {
	tests := []struct {
		name        string
		baseURL     func(string) string
		temperature float64
		wantTemp    string
	}{
		{"temperature set", func(u string) string { return u }, 0.3, "0.3"},
		{"temperature zero is still sent", func(u string) string { return u }, 0, "0"},
		{"trailing slash on the base url", func(u string) string { return u + "/" }, 0.3, "0.3"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			var gotMethod, gotPath, gotAuth, gotType string
			var gotBody map[string]json.RawMessage
			srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				gotMethod, gotPath = r.Method, r.URL.Path
				gotAuth, gotType = r.Header.Get("Authorization"), r.Header.Get("Content-Type")
				if err := json.NewDecoder(r.Body).Decode(&gotBody); err != nil {
					t.Errorf("decode request body: %v", err)
				}
				_, _ = w.Write([]byte(`{"choices":[{"message":{"content":"ok"},"finish_reason":"stop"}]}`))
			}))
			defer srv.Close()

			off := false
			_, err := newTestClient(t, tt.baseURL(srv.URL)).Chat(context.Background(), ollama.ChatRequest{
				Model: "general",
				Messages: []ollama.ChatMessage{
					{Role: "system", Content: "rules"},
					{Role: "user", Content: "data"},
				},
				Options:  ollama.Options{Temperature: tt.temperature, NumCtx: 32768},
				Truncate: &off,
				Shift:    &off,
			})
			if err != nil {
				t.Fatalf("Chat: %v", err)
			}

			if gotMethod != http.MethodPost || gotPath != "/v1/chat/completions" {
				t.Errorf("request = %s %s, want POST /v1/chat/completions", gotMethod, gotPath)
			}
			if gotAuth != "Bearer "+testKey {
				t.Errorf("Authorization = %q, want the bearer key", gotAuth)
			}
			if gotType != "application/json" {
				t.Errorf("Content-Type = %q, want application/json", gotType)
			}

			keys := make([]string, 0, len(gotBody))
			for k := range gotBody {
				keys = append(keys, k)
			}
			slices.Sort(keys)
			if want := []string{"messages", "model", "stream", "temperature"}; !slices.Equal(keys, want) {
				t.Errorf("body keys = %v, want %v", keys, want)
			}
			if got := string(gotBody["model"]); got != `"general"` {
				t.Errorf("model = %s, want \"general\"", got)
			}
			if got := string(gotBody["stream"]); got != "false" {
				t.Errorf("stream = %s, want false", got)
			}
			if got := string(gotBody["temperature"]); got != tt.wantTemp {
				t.Errorf("temperature = %s, want %s", got, tt.wantTemp)
			}
			wantMessages := `[{"role":"system","content":"rules"},{"role":"user","content":"data"}]`
			if got := string(gotBody["messages"]); got != wantMessages {
				t.Errorf("messages = %s, want %s", got, wantMessages)
			}
		})
	}
}

func TestChatReply(t *testing.T) {
	tests := []struct {
		name           string
		body           string
		wantContent    string
		wantDoneReason string
		wantPrompt     int
		wantCompletion int
		wantErr        error
	}{
		{
			name: "finished reply, reasoning ignored",
			body: `{"choices":[{"finish_reason":"stop","message":{"content":"the report","reasoning_content":"thinking","refusal":null,"role":"assistant","provider_specific_fields":null}}],` +
				`"usage":{"prompt_tokens":30000,"completion_tokens":7500,"total_tokens":37500}}`,
			wantContent:    "the report",
			wantDoneReason: "stop",
			wantPrompt:     30000,
			wantCompletion: 7500,
		},
		{
			name:           "cut off reply",
			body:           `{"choices":[{"finish_reason":"length","message":{"content":"half a rep"}}],"usage":{"prompt_tokens":60000,"completion_tokens":5536}}`,
			wantContent:    "half a rep",
			wantDoneReason: ollama.DoneReasonLength,
			wantPrompt:     60000,
			wantCompletion: 5536,
		},
		{
			// Measured with max_tokens 20: the reasoning used the whole reply.
			name:           "cut off reply with no content is still a cut off reply",
			body:           `{"choices":[{"finish_reason":"length","message":{"content":"","reasoning_content":"thinking"}}],"usage":{"prompt_tokens":12,"completion_tokens":20}}`,
			wantDoneReason: ollama.DoneReasonLength,
			wantPrompt:     12,
			wantCompletion: 20,
		},
		{
			name:    "finished reply with no content",
			body:    `{"choices":[{"finish_reason":"stop","message":{"content":"","reasoning_content":"thinking"}}],"usage":{"prompt_tokens":12,"completion_tokens":20}}`,
			wantErr: ErrEmptyReply,
		},
		{
			name:    "finished reply of whitespace",
			body:    `{"choices":[{"finish_reason":"stop","message":{"content":"\n \n"}}]}`,
			wantErr: ErrEmptyReply,
		},
		{
			name:    "no choices",
			body:    `{"choices":[],"usage":{"prompt_tokens":12,"completion_tokens":0}}`,
			wantErr: ErrEmptyReply,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			srv := serve(t, http.StatusOK, tt.body)
			resp, err := newTestClient(t, srv.URL).Chat(context.Background(), ollama.ChatRequest{Model: "general"})
			if tt.wantErr != nil {
				if !errors.Is(err, tt.wantErr) {
					t.Fatalf("Chat error = %v, want %v", err, tt.wantErr)
				}
				return
			}
			if err != nil {
				t.Fatalf("Chat: %v", err)
			}
			if resp.Message.Content != tt.wantContent {
				t.Errorf("content = %q, want %q", resp.Message.Content, tt.wantContent)
			}
			if resp.DoneReason != tt.wantDoneReason {
				t.Errorf("DoneReason = %q, want %q", resp.DoneReason, tt.wantDoneReason)
			}
			if resp.PromptEvalCount != tt.wantPrompt || resp.EvalCount != tt.wantCompletion {
				t.Errorf("tokens = %d/%d, want %d/%d", resp.PromptEvalCount, resp.EvalCount, tt.wantPrompt, tt.wantCompletion)
			}
		})
	}
}

// TestStatusError covers a non-200 from either call: it unwraps to the
// *ollama.StatusError the analyzer matches on, keeps at most 1KB of the body,
// and never carries the key.
func TestStatusError(t *testing.T) {
	calls := map[string]func(*Client) error{
		"chat": func(c *Client) error {
			_, err := c.Chat(context.Background(), ollama.ChatRequest{Model: "general"})
			return err
		},
		"ping": func(c *Client) error { return c.Ping(context.Background()) },
	}
	tests := []struct {
		name     string
		status   int
		body     string
		wantBody string
	}{
		{"prompt over the context", http.StatusBadRequest, contextRefusal, contextRefusal},
		{"wrong key", http.StatusUnauthorized, wrongKey, wrongKey},
		{"body over 1KB is cut", http.StatusBadGateway, strings.Repeat("x", 4096), strings.Repeat("x", 1024)},
		{"echoed key is cut out", http.StatusUnauthorized, "no such key: " + testKey, "no such key: [redacted]"},
	}
	for call, do := range calls {
		for _, tt := range tests {
			t.Run(call+"/"+tt.name, func(t *testing.T) {
				srv := serve(t, tt.status, tt.body)
				err := do(newTestClient(t, srv.URL))

				var statusErr *ollama.StatusError
				if !errors.As(err, &statusErr) {
					t.Fatalf("error = %v, want one that unwraps to *ollama.StatusError", err)
				}
				if statusErr.StatusCode != tt.status {
					t.Errorf("StatusCode = %d, want %d", statusErr.StatusCode, tt.status)
				}
				if statusErr.Body != tt.wantBody {
					t.Errorf("Body = %q, want %q", statusErr.Body, tt.wantBody)
				}
				if strings.Contains(err.Error(), testKey) {
					t.Errorf("error text carries the key: %v", err)
				}
				if strings.Contains(err.Error(), "ollama") {
					t.Errorf("error text names the wrong backend: %v", err)
				}
			})
		}
	}
}

func TestPing(t *testing.T) {
	var gotMethod, gotPath, gotAuth string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotMethod, gotPath, gotAuth = r.Method, r.URL.Path, r.Header.Get("Authorization")
		_, _ = w.Write([]byte(`{"data":[{"id":"general","object":"model"}],"object":"list"}`))
	}))
	defer srv.Close()

	if err := newTestClient(t, srv.URL).Ping(context.Background()); err != nil {
		t.Fatalf("Ping: %v", err)
	}
	if gotMethod != http.MethodGet || gotPath != "/v1/models" {
		t.Errorf("request = %s %s, want GET /v1/models", gotMethod, gotPath)
	}
	if gotAuth != "Bearer "+testKey {
		t.Errorf("Authorization = %q, want the bearer key", gotAuth)
	}
}

// TestCAFile covers a server whose certificate comes from a private CA: it
// verifies with ca_file pointing at that CA and fails without, and a ca_file
// that cannot supply a certificate stops New.
func TestCAFile(t *testing.T) {
	srv := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		_, _ = w.Write([]byte(`{"data":[]}`))
	}))
	defer srv.Close()

	dir := t.TempDir()
	caFile := filepath.Join(dir, "ca.pem")
	caPEM := pem.EncodeToMemory(&pem.Block{Type: "CERTIFICATE", Bytes: srv.Certificate().Raw})
	if err := os.WriteFile(caFile, caPEM, 0o600); err != nil {
		t.Fatal(err)
	}
	notPEM := filepath.Join(dir, "empty.pem")
	if err := os.WriteFile(notPEM, []byte("not a certificate\n"), 0o600); err != nil {
		t.Fatal(err)
	}

	t.Run("with ca_file the server verifies", func(t *testing.T) {
		c, err := New(Config{BaseURL: srv.URL, APIKey: testKey, CAFile: caFile, Timeout: 5 * time.Second})
		if err != nil {
			t.Fatalf("New: %v", err)
		}
		if err := c.Ping(context.Background()); err != nil {
			t.Fatalf("Ping: %v", err)
		}
	})

	t.Run("without ca_file only the system roots are trusted", func(t *testing.T) {
		err := newTestClient(t, srv.URL).Ping(context.Background())
		var unknownCA x509.UnknownAuthorityError
		if !errors.As(err, &unknownCA) {
			t.Fatalf("Ping error = %v, want x509.UnknownAuthorityError", err)
		}
	})

	for name, path := range map[string]string{
		"missing ca_file stops New":               filepath.Join(dir, "absent.pem"),
		"ca_file without a certificate stops New": notPEM,
	} {
		t.Run(name, func(t *testing.T) {
			if _, err := New(Config{BaseURL: srv.URL, APIKey: testKey, CAFile: path}); err == nil {
				t.Fatal("New: want error")
			}
		})
	}
}
