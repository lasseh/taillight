package analyzer

import (
	"context"
	"encoding/json"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/lasseh/taillight/internal/model"
	"github.com/lasseh/taillight/internal/ollama"
)

// fakeOllamaServer stands in for the real ollama HTTP server in run tests.
// It tracks how many times /api/chat is hit so the test can assert "Chat
// was never called" for the short-circuit path. /api/tags always succeeds
// so Ping() returns nil without exercising the model.
type fakeOllamaServer struct {
	srv       *httptest.Server
	chatCalls atomic.Int32
	// chatReply is what /api/chat returns when invoked. The "## " marker
	// makes it pass the structure validator for a daily report so the
	// non-empty test exercises the full happy path, not just the early
	// short-circuit.
	chatReply string
	// replies, when set, is served in order, one per /api/chat call (the last
	// entry repeats). An empty entry answers with HTTP 500 so a test can make
	// a single call fail.
	replies []string
}

func newFakeOllamaServer() *fakeOllamaServer {
	f := &fakeOllamaServer{
		chatReply: chatReplyDaily,
	}
	mux := http.NewServeMux()
	mux.HandleFunc("/api/tags", func(w http.ResponseWriter, _ *http.Request) {
		_, _ = w.Write([]byte(`{"models":[]}`))
	})
	mux.HandleFunc("/api/chat", func(w http.ResponseWriter, _ *http.Request) {
		n := int(f.chatCalls.Add(1))
		reply := f.chatReply
		if len(f.replies) > 0 {
			reply = f.replies[min(n, len(f.replies))-1]
			if reply == "" {
				http.Error(w, "model crashed", http.StatusInternalServerError)
				return
			}
		}
		_ = json.NewEncoder(w).Encode(ollama.ChatResponse{
			Message:         ollama.ChatMessage{Role: "assistant", Content: reply},
			PromptEvalCount: 100,
			EvalCount:       50,
		})
	})
	f.srv = httptest.NewServer(mux)
	return f
}

func (f *fakeOllamaServer) Close() { f.srv.Close() }

// chatReplyDaily is a minimal daily-mode reply that passes validateReport.
// Real model output is much longer; the validator only checks the header set
// and the Status: line in the first section.
const chatReplyDaily = `## TL;DR
**Status: NOMINAL** — nothing of concern observed in the period.

## Needs Action
_Nothing of concern this period._

## What Happened
_Nothing of concern this period._

## Watch
_Nothing of concern this period._

*Baseline: sev≤3 0/day vs 7-day 0/day (0%)*
`

// TestRunShortCircuitsOnEmptyData proves the analyzer skips the LLM when
// the gather returns an empty window. Without this guarantee, every empty
// report costs a model slot and risks the model inventing a verdict because
// the prompt structure demands one.
func TestRunShortCircuitsOnEmptyData(t *testing.T) {
	srv := newFakeOllamaServer()
	defer srv.Close()

	a := New(stubStore{}, ollama.New(srv.srv.URL, 5*time.Second), Config{Model: "test"}, slog.New(slog.NewTextHandler(io.Discard, nil)))
	res, err := a.Run(context.Background(), RunParams{
		Feed:   feedSrvlog,
		Hosts:  []string{"quiet.lab"},
		Period: time.Hour,
		Mode:   modeIncident,
	})
	if err != nil {
		t.Fatalf("Run: %v", err)
	}
	if got := srv.chatCalls.Load(); got != 0 {
		t.Errorf("Chat call count: got %d, want 0 (LLM must not be called on empty data)", got)
	}
	if res.PromptTokens != 0 || res.CompletionTokens != 0 {
		t.Errorf("tokens: got prompt=%d completion=%d, want 0/0", res.PromptTokens, res.CompletionTokens)
	}
	if !strings.Contains(res.Report, "No events recorded for the scoped host(s)") {
		t.Errorf("Report missing scoped empty-state body; got:\n%s", res.Report)
	}
	if !strings.Contains(res.Report, "# Incident Briefing") {
		t.Errorf("Report missing prepended title header; got:\n%s", res.Report)
	}
}

// TestRunShortCircuitUsesAllHostsBodyWhenUnscoped guards the body phrasing
// branch — an unscoped empty run says "on this feed", not "scoped host(s)".
func TestRunShortCircuitUsesAllHostsBodyWhenUnscoped(t *testing.T) {
	srv := newFakeOllamaServer()
	defer srv.Close()

	a := New(stubStore{}, ollama.New(srv.srv.URL, 5*time.Second), Config{Model: "test"}, slog.New(slog.NewTextHandler(io.Discard, nil)))
	res, err := a.Run(context.Background(), RunParams{
		Feed:   feedSrvlog,
		Period: time.Hour,
		Mode:   modeDaily,
	})
	if err != nil {
		t.Fatalf("Run: %v", err)
	}
	if !strings.Contains(res.Report, "No events recorded on this feed") {
		t.Errorf("Report missing all-hosts empty-state body; got:\n%s", res.Report)
	}
}

// nonEmptyStore returns enough fixture data to defeat isEmptyData so the
// test can assert the LLM still gets called on real data. Without this
// regression check, a bug in the empty-detection logic would silently
// short-circuit every report.
type nonEmptyStore struct{ stubStore }

func (s nonEmptyStore) GetTopMsgIDs(context.Context, model.AnalysisScope, time.Time, int) ([]model.MsgIDCount, error) {
	return []model.MsgIDCount{
		{MsgID: "TEST_EVENT", Count: 5, SeverityCounts: map[int]int64{3: 5}},
	}, nil
}

// TestRunCallsLLMOnNonEmptyData verifies the short-circuit doesn't fire
// when data is present — a regression guard for the isEmptyData logic.
func TestRunCallsLLMOnNonEmptyData(t *testing.T) {
	srv := newFakeOllamaServer()
	defer srv.Close()

	a := New(nonEmptyStore{}, ollama.New(srv.srv.URL, 5*time.Second), Config{Model: "test"}, slog.New(slog.NewTextHandler(io.Discard, nil)))
	res, err := a.Run(context.Background(), RunParams{
		Feed:   feedSrvlog,
		Period: 24 * time.Hour,
		Mode:   modeDaily,
	})
	if err != nil {
		t.Fatalf("Run: %v", err)
	}
	if got := srv.chatCalls.Load(); got != 1 {
		t.Errorf("Chat call count: got %d, want 1 (LLM must be called when data is present)", got)
	}
	if res.CompletionTokens == 0 {
		t.Errorf("CompletionTokens should be non-zero on a real LLM run, got %d", res.CompletionTokens)
	}
}

// TestRunCorrectiveRetry covers the one corrective follow-up Run sends when the
// first reply fails validation: a valid retry replaces the reply and its tokens
// add to the first call's; an invalid or failed retry keeps the first reply.
func TestRunCorrectiveRetry(t *testing.T) {
	const invalid = "## TL;DR\nall quiet\n"
	tests := []struct {
		name       string
		replies    []string
		wantBody   string
		wantPrompt int
		wantCompl  int
	}{
		{"first reply valid, no retry", []string{chatReplyDaily}, "**Status: NOMINAL**", 100, 50},
		{"retry fixes the report", []string{invalid, chatReplyDaily}, "**Status: NOMINAL**", 200, 100},
		{"retry still invalid keeps first", []string{invalid, invalid}, "all quiet", 100, 50},
		{"retry errors keeps first", []string{invalid, ""}, "all quiet", 100, 50},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			srv := newFakeOllamaServer()
			defer srv.Close()
			srv.replies = tt.replies

			a := New(nonEmptyStore{}, ollama.New(srv.srv.URL, 5*time.Second), Config{Model: "test"}, slog.New(slog.NewTextHandler(io.Discard, nil)))
			res, err := a.Run(context.Background(), RunParams{Feed: feedSrvlog, Period: 24 * time.Hour, Mode: modeDaily})
			if err != nil {
				t.Fatalf("Run: %v", err)
			}
			if got, want := int(srv.chatCalls.Load()), len(tt.replies); got != want {
				t.Errorf("chat calls = %d, want %d", got, want)
			}
			if !strings.Contains(res.Report, tt.wantBody) {
				t.Errorf("report missing %q:\n%s", tt.wantBody, res.Report)
			}
			if res.PromptTokens != tt.wantPrompt || res.CompletionTokens != tt.wantCompl {
				t.Errorf("tokens = %d/%d, want %d/%d", res.PromptTokens, res.CompletionTokens, tt.wantPrompt, tt.wantCompl)
			}
		})
	}
}
