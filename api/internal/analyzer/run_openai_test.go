package analyzer

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"slices"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	dto "github.com/prometheus/client_model/go"

	"github.com/lasseh/taillight/internal/metrics"
	"github.com/lasseh/taillight/internal/ollama"
	"github.com/lasseh/taillight/internal/openai"
)

const openAITestKey = "sk-test-0123456789abcdef"

// gatewayContextRefusal is the LiteLLM gateway's answer to a 160k-token
// prompt, as measured on 2026-11-18. A raw string, so \n stays the two bytes
// it is on the wire.
const gatewayContextRefusal = `{"error":{"message":"litellm.BadRequestError: OpenAIException - Input length (160068) exceeds model's maximum context length (65536).\n\nLiteLLM: model group 'general' failed with the error above. No fallback was attempted.","type":"BadRequestError","param":null,"code":"400"}}`

// gatewayWrongKey is the gateway's answer to a key it does not know.
const gatewayWrongKey = `{"error":{"message":"Authentication Error, Invalid API key provided.","type":"token_not_found_in_db","param":"key","code":"401"}}`

// fakeOpenAIServer stands in for an OpenAI-compatible gateway in run tests.
// Both endpoints refuse a request without the bearer key.
type fakeOpenAIServer struct {
	srv       *httptest.Server
	chatCalls atomic.Int32
	// replies is served in order, one per chat call (the last entry repeats).
	replies []string
	// cutOff, when set, marks that chat call (1-based) as having run out of
	// context: finish_reason "length".
	cutOff int
	// tooLong makes the chat endpoint refuse the prompt the way the gateway
	// does.
	tooLong bool
	// chatRejectsKey makes the chat endpoint answer 401 to a key the models
	// endpoint accepted.
	chatRejectsKey bool
	// bodies holds each chat request body, in order.
	bodies []map[string]json.RawMessage
}

func newFakeOpenAIServer(t *testing.T) *fakeOpenAIServer {
	t.Helper()
	f := &fakeOpenAIServer{replies: []string{chatReplyDaily}}
	authorized := func(w http.ResponseWriter, r *http.Request) bool {
		if r.Header.Get("Authorization") == "Bearer "+openAITestKey {
			return true
		}
		http.Error(w, gatewayWrongKey, http.StatusUnauthorized)
		return false
	}
	mux := http.NewServeMux()
	mux.HandleFunc("GET /v1/models", func(w http.ResponseWriter, r *http.Request) {
		if authorized(w, r) {
			_, _ = w.Write([]byte(`{"data":[{"id":"general","object":"model"}],"object":"list"}`))
		}
	})
	mux.HandleFunc("POST /v1/chat/completions", func(w http.ResponseWriter, r *http.Request) {
		n := int(f.chatCalls.Add(1))
		if !authorized(w, r) {
			return
		}
		if f.chatRejectsKey {
			http.Error(w, gatewayWrongKey, http.StatusUnauthorized)
			return
		}
		var body map[string]json.RawMessage
		if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
			t.Errorf("decode chat request: %v", err)
		}
		f.bodies = append(f.bodies, body)
		if f.tooLong {
			http.Error(w, gatewayContextRefusal, http.StatusBadRequest)
			return
		}
		finish := "stop"
		if n == f.cutOff {
			finish = "length"
		}
		_ = json.NewEncoder(w).Encode(map[string]any{
			"choices": []map[string]any{{
				"finish_reason": finish,
				"message": map[string]any{
					"role":              "assistant",
					"content":           f.replies[min(n, len(f.replies))-1],
					"reasoning_content": "thinking it over",
				},
			}},
			"usage": map[string]int{"prompt_tokens": 100, "completion_tokens": 50, "total_tokens": 150},
		})
	})
	f.srv = httptest.NewServer(mux)
	t.Cleanup(f.srv.Close)
	return f
}

// analyzer returns an Analyzer that talks to the fake with key, logging to
// logs.
func (f *fakeOpenAIServer) analyzer(t *testing.T, key string, logs io.Writer) *Analyzer {
	t.Helper()
	client, err := openai.New(openai.Config{BaseURL: f.srv.URL, APIKey: key, Timeout: 5 * time.Second})
	if err != nil {
		t.Fatalf("openai.New: %v", err)
	}
	return New(nonEmptyStore{}, client, Config{API: "openai", Model: "general", Temperature: 0.3, NumCtx: 32768},
		slog.New(slog.NewTextHandler(logs, nil)))
}

func completionTokensObserved(t *testing.T) (count uint64, sum float64) {
	t.Helper()
	var m dto.Metric
	if err := metrics.AnalysisCompletionTokens.Write(&m); err != nil {
		t.Fatalf("read completion tokens histogram: %v", err)
	}
	return m.GetHistogram().GetSampleCount(), m.GetHistogram().GetSampleSum()
}

// TestRunOpenAI covers a run on the OpenAI API: the system and user messages
// go to /v1/chat/completions with the key, the reply is stored, and its token
// counts reach the result, the metric and the "analysis complete" log line.
func TestRunOpenAI(t *testing.T) {
	srv := newFakeOpenAIServer(t)
	var logs bytes.Buffer
	countBefore, sumBefore := completionTokensObserved(t)

	res, err := srv.analyzer(t, openAITestKey, &logs).Run(context.Background(),
		RunParams{Feed: feedSrvlog, Period: 24 * time.Hour, Mode: modeDaily})
	if err != nil {
		t.Fatalf("Run: %v", err)
	}

	if !strings.Contains(res.Report, "**Status: NOMINAL**") {
		t.Errorf("report is not the reply:\n%s", res.Report)
	}
	if strings.Contains(res.Report, "thinking it over") {
		t.Errorf("report carries the reasoning:\n%s", res.Report)
	}
	if res.PromptTokens != 100 || res.CompletionTokens != 50 {
		t.Errorf("tokens = %d/%d, want 100/50", res.PromptTokens, res.CompletionTokens)
	}

	countAfter, sumAfter := completionTokensObserved(t)
	if countAfter-countBefore != 1 || sumAfter-sumBefore != 50 {
		t.Errorf("completion tokens metric grew by %d observations summing %v, want 1 summing 50",
			countAfter-countBefore, sumAfter-sumBefore)
	}

	var complete string
	for line := range strings.Lines(logs.String()) {
		if strings.Contains(line, `msg="analysis complete"`) {
			complete = line
		}
	}
	if !strings.Contains(complete, "prompt_tokens=100") || !strings.Contains(complete, "completion_tokens=50") {
		t.Errorf("analysis complete log line lacks the token counts: %q", complete)
	}
	if !strings.Contains(logs.String(), `msg="sending prompt to openai"`) {
		t.Errorf("log does not name the backend:\n%s", logs.String())
	}
	if strings.Contains(logs.String(), openAITestKey) {
		t.Errorf("log carries the key:\n%s", logs.String())
	}

	if len(srv.bodies) != 1 {
		t.Fatalf("chat calls = %d, want 1", len(srv.bodies))
	}
	body := srv.bodies[0]
	keys := make([]string, 0, len(body))
	for k := range body {
		keys = append(keys, k)
	}
	slices.Sort(keys)
	// No max_tokens, and none of num_ctx (options), truncate or shift.
	if want := []string{"messages", "model", "stream", "temperature"}; !slices.Equal(keys, want) {
		t.Errorf("chat body keys = %v, want %v", keys, want)
	}
	var messages []ollama.ChatMessage
	if err := json.Unmarshal(body["messages"], &messages); err != nil {
		t.Fatalf("decode messages: %v", err)
	}
	if len(messages) != 2 || messages[0].Role != "system" || messages[1].Role != "user" {
		t.Fatalf("messages = %+v, want a system and a user message", messages)
	}
	if !strings.Contains(messages[1].Content, "TEST_EVENT") {
		t.Errorf("user message lacks the gathered data:\n%s", messages[1].Content)
	}
}

// TestRunOpenAIFailsWhenContextExceeded is TestRunFailsWhenContextExceeded on
// the OpenAI API: the gateway refuses a prompt over the model's context, or
// ends a reply with finish_reason "length".
func TestRunOpenAIFailsWhenContextExceeded(t *testing.T) {
	tests := []struct {
		name  string
		setup func(*fakeOpenAIServer)
	}{
		{"prompt refused", func(f *fakeOpenAIServer) { f.tooLong = true }},
		{"reply cut off", func(f *fakeOpenAIServer) { f.cutOff = 1 }},
		{"reply cut off before any content", func(f *fakeOpenAIServer) { f.cutOff = 1; f.replies = []string{""} }},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			srv := newFakeOpenAIServer(t)
			tt.setup(srv)

			_, err := srv.analyzer(t, openAITestKey, io.Discard).Run(context.Background(),
				RunParams{Feed: feedSrvlog, Period: 24 * time.Hour, Mode: modeDaily})
			if !errors.Is(err, ErrContextExceeded) {
				t.Fatalf("Run error = %v, want ErrContextExceeded", err)
			}
			if got := srv.chatCalls.Load(); got != 1 {
				t.Errorf("chat calls = %d, want 1", got)
			}
		})
	}
}

// TestRunOpenAICorrectiveRetry covers the validation retry on the OpenAI API:
// a retry that is cut off, or comes back empty, is discarded and the first
// reply kept; a valid one replaces it.
func TestRunOpenAICorrectiveRetry(t *testing.T) {
	const invalid = "## TL;DR\nall quiet\n"
	tests := []struct {
		name     string
		replies  []string
		cutOff   int
		wantBody string
	}{
		{"retry fixes the report", []string{invalid, chatReplyDaily}, 0, "**Status: NOMINAL**"},
		{"retry cut off keeps first", []string{invalid, chatReplyDaily}, 2, "all quiet"},
		{"retry empty keeps first", []string{invalid, ""}, 0, "all quiet"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			srv := newFakeOpenAIServer(t)
			srv.replies, srv.cutOff = tt.replies, tt.cutOff

			res, err := srv.analyzer(t, openAITestKey, io.Discard).Run(context.Background(),
				RunParams{Feed: feedSrvlog, Period: 24 * time.Hour, Mode: modeDaily})
			if err != nil {
				t.Fatalf("Run: %v", err)
			}
			if got := srv.chatCalls.Load(); got != 2 {
				t.Errorf("chat calls = %d, want 2", got)
			}
			if !strings.Contains(res.Report, tt.wantBody) {
				t.Errorf("report missing %q:\n%s", tt.wantBody, res.Report)
			}
		})
	}
}

// TestRunOpenAIRejectedKey covers a 401: the run fails with an error that
// names analysis.api_key and does not print the key, whether the models
// endpoint or the chat endpoint refuses it.
func TestRunOpenAIRejectedKey(t *testing.T) {
	const badKey = "sk-wrong-fedcba9876543210"
	tests := []struct {
		name          string
		key           string
		setup         func(*fakeOpenAIServer)
		wantChatCalls int32
	}{
		{"ping refused", badKey, func(*fakeOpenAIServer) {}, 0},
		{"chat refused", openAITestKey, func(f *fakeOpenAIServer) { f.chatRejectsKey = true }, 1},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			srv := newFakeOpenAIServer(t)
			tt.setup(srv)

			_, err := srv.analyzer(t, tt.key, io.Discard).Run(context.Background(),
				RunParams{Feed: feedSrvlog, Period: 24 * time.Hour, Mode: modeDaily})
			if !errors.Is(err, ErrUnauthorized) {
				t.Fatalf("Run error = %v, want ErrUnauthorized", err)
			}
			if !strings.Contains(err.Error(), "analysis.api_key") {
				t.Errorf("error does not name the setting: %v", err)
			}
			if strings.Contains(err.Error(), tt.key) {
				t.Errorf("error prints the key: %v", err)
			}
			if strings.Contains(err.Error(), "not available") {
				t.Errorf("error reports a rejected key as an unavailable backend: %v", err)
			}
			if got := srv.chatCalls.Load(); got != tt.wantChatCalls {
				t.Errorf("chat calls = %d, want %d", got, tt.wantChatCalls)
			}
		})
	}
}

// TestRunOllama401IsNotARejectedKey covers the default backend behind
// something that answers 401: Ollama's API takes no key, so the run fails as
// the backend error it always was, not with advice about analysis.api_key.
func TestRunOllama401IsNotARejectedKey(t *testing.T) {
	mux := http.NewServeMux()
	mux.HandleFunc("/api/tags", func(w http.ResponseWriter, _ *http.Request) {
		_, _ = w.Write([]byte(`{"models":[]}`))
	})
	mux.HandleFunc("/api/chat", func(w http.ResponseWriter, _ *http.Request) {
		http.Error(w, "authorization required", http.StatusUnauthorized)
	})
	srv := httptest.NewServer(mux)
	defer srv.Close()

	a := New(nonEmptyStore{}, ollama.New(srv.URL, 5*time.Second), Config{Model: "test"}, slog.New(slog.NewTextHandler(io.Discard, nil)))
	_, err := a.Run(context.Background(), RunParams{Feed: feedSrvlog, Period: 24 * time.Hour, Mode: modeDaily})
	if err == nil {
		t.Fatal("Run: want error")
	}
	if errors.Is(err, ErrUnauthorized) {
		t.Errorf("Run error = %v, want it not to be ErrUnauthorized", err)
	}
	if !strings.HasPrefix(err.Error(), "ollama chat: ollama returned status 401") {
		t.Errorf("Run error = %q, want the ollama chat status error", err)
	}
}

// TestRunOpenAIFailsOnEmptyReply covers a reply that finished with no content
// (a reasoning model that spent the whole reply reasoning): the run fails
// instead of storing an empty report.
func TestRunOpenAIFailsOnEmptyReply(t *testing.T) {
	srv := newFakeOpenAIServer(t)
	srv.replies = []string{""}

	_, err := srv.analyzer(t, openAITestKey, io.Discard).Run(context.Background(),
		RunParams{Feed: feedSrvlog, Period: 24 * time.Hour, Mode: modeDaily})
	if !errors.Is(err, openai.ErrEmptyReply) {
		t.Fatalf("Run error = %v, want openai.ErrEmptyReply", err)
	}
	if got := srv.chatCalls.Load(); got != 1 {
		t.Errorf("chat calls = %d, want 1", got)
	}
}
