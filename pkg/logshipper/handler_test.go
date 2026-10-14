package logshipper

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strconv"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"testing/slogtest"
	"time"
	"unicode/utf8"
)

func mustNew(t *testing.T, cfg Config) *Handler {
	t.Helper()
	h, err := New(cfg)
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	return h
}

// captureServer accepts every ingest request and returns the entries received
// so far. Call Shutdown on the handler first to flush.
func captureServer(t *testing.T) (endpoint string, entries func() []logEntry) {
	t.Helper()
	var mu sync.Mutex
	var got []logEntry
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var req ingestRequest
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			t.Errorf("decode request: %v", err)
		}
		mu.Lock()
		got = append(got, req.Logs...)
		mu.Unlock()
		w.WriteHeader(http.StatusAccepted)
	}))
	t.Cleanup(srv.Close)
	return srv.URL, func() []logEntry {
		mu.Lock()
		defer mu.Unlock()
		return append([]logEntry(nil), got...)
	}
}

func TestHandler_BatchSend(t *testing.T) {
	var mu sync.Mutex
	var received []ingestRequest

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		body, _ := io.ReadAll(r.Body)
		var req ingestRequest
		if err := json.Unmarshal(body, &req); err != nil {
			t.Errorf("unmarshal request: %v", err)
			w.WriteHeader(http.StatusBadRequest)
			return
		}
		mu.Lock()
		received = append(received, req)
		mu.Unlock()
		w.WriteHeader(http.StatusAccepted)
	}))
	defer srv.Close()

	h := mustNew(t, Config{
		Endpoint:    srv.URL,
		APIKey:      "test-key",
		Service:     "test-svc",
		BatchSize:   5,
		FlushPeriod: 100 * time.Millisecond,
		BufferSize:  100,
	})

	logger := slog.New(h)

	// Send 5 logs — should trigger a batch.
	for i := range 5 {
		logger.Info("test message", "count", i)
	}

	// Wait for flush.
	time.Sleep(300 * time.Millisecond)

	mu.Lock()
	defer mu.Unlock()

	if len(received) == 0 {
		t.Fatal("expected at least one batch")
	}

	total := 0
	for _, r := range received {
		total += len(r.Logs)
	}
	if total != 5 {
		t.Errorf("received %d entries, want 5", total)
	}

	// Verify fields.
	entry := received[0].Logs[0]
	if entry.Service != "test-svc" {
		t.Errorf("service = %q, want %q", entry.Service, "test-svc")
	}
	if entry.Level != "INFO" {
		t.Errorf("level = %q, want %q", entry.Level, "INFO")
	}
	if entry.Msg != "test message" {
		t.Errorf("msg = %q, want %q", entry.Msg, "test message")
	}
}

func TestHandler_FlushOnPeriod(t *testing.T) {
	var mu sync.Mutex
	var received []ingestRequest

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		body, _ := io.ReadAll(r.Body)
		var req ingestRequest
		json.Unmarshal(body, &req) //nolint:errcheck
		mu.Lock()
		received = append(received, req)
		mu.Unlock()
		w.WriteHeader(http.StatusAccepted)
	}))
	defer srv.Close()

	h := mustNew(t, Config{
		Endpoint:    srv.URL,
		APIKey:      "test-key",
		Service:     "test-svc",
		BatchSize:   1000, // Large batch size, flush should be timer-driven.
		FlushPeriod: 50 * time.Millisecond,
		BufferSize:  100,
	})

	logger := slog.New(h)
	logger.Info("single log")

	// Wait for timer flush.
	time.Sleep(200 * time.Millisecond)

	mu.Lock()
	defer mu.Unlock()

	if len(received) == 0 {
		t.Fatal("expected timer-triggered flush")
	}
	if received[0].Logs[0].Msg != "single log" {
		t.Errorf("msg = %q, want %q", received[0].Logs[0].Msg, "single log")
	}
}

func TestHandler_Shutdown(t *testing.T) {
	var mu sync.Mutex
	var received []ingestRequest

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		body, _ := io.ReadAll(r.Body)
		var req ingestRequest
		json.Unmarshal(body, &req) //nolint:errcheck
		mu.Lock()
		received = append(received, req)
		mu.Unlock()
		w.WriteHeader(http.StatusAccepted)
	}))
	defer srv.Close()

	h := mustNew(t, Config{
		Endpoint:    srv.URL,
		APIKey:      "test-key",
		Service:     "test-svc",
		BatchSize:   1000,      // Won't trigger batch by size.
		FlushPeriod: time.Hour, // Won't trigger by timer.
		BufferSize:  100,
	})

	logger := slog.New(h)
	logger.Warn("shutdown test")

	// Shutdown should flush.
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	if err := h.Shutdown(ctx); err != nil {
		t.Fatalf("shutdown error: %v", err)
	}

	mu.Lock()
	defer mu.Unlock()

	if len(received) == 0 {
		t.Fatal("expected shutdown flush")
	}
	if received[0].Logs[0].Msg != "shutdown test" {
		t.Errorf("msg = %q, want %q", received[0].Logs[0].Msg, "shutdown test")
	}
}

func TestHandler_WithAttrs(t *testing.T) {
	var mu sync.Mutex
	var received []ingestRequest

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		body, _ := io.ReadAll(r.Body)
		var req ingestRequest
		json.Unmarshal(body, &req) //nolint:errcheck
		mu.Lock()
		received = append(received, req)
		mu.Unlock()
		w.WriteHeader(http.StatusAccepted)
	}))
	defer srv.Close()

	h := mustNew(t, Config{
		Endpoint:    srv.URL,
		APIKey:      "test-key",
		Service:     "test-svc",
		BatchSize:   100,
		FlushPeriod: 50 * time.Millisecond,
		BufferSize:  100,
	})

	logger := slog.New(h).With("request_id", "abc-123")
	logger.Info("with attrs test", "extra", "value")

	time.Sleep(200 * time.Millisecond)

	mu.Lock()
	defer mu.Unlock()

	if len(received) == 0 {
		t.Fatal("expected at least one batch")
	}

	entry := received[0].Logs[0]
	var attrs map[string]any
	if err := json.Unmarshal(entry.Attrs, &attrs); err != nil {
		t.Fatalf("unmarshal attrs: %v", err)
	}
	if attrs["request_id"] != "abc-123" {
		t.Errorf("request_id = %v, want %q", attrs["request_id"], "abc-123")
	}
	if attrs["extra"] != "value" {
		t.Errorf("extra = %v, want %q", attrs["extra"], "value")
	}
}

func TestHandler_ErrorSerialization(t *testing.T) {
	var mu sync.Mutex
	var received []ingestRequest

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		body, _ := io.ReadAll(r.Body)
		var req ingestRequest
		json.Unmarshal(body, &req) //nolint:errcheck
		mu.Lock()
		received = append(received, req)
		mu.Unlock()
		w.WriteHeader(http.StatusAccepted)
	}))
	defer srv.Close()

	h := mustNew(t, Config{
		Endpoint:    srv.URL,
		APIKey:      "test-key",
		Service:     "test-svc",
		BatchSize:   100,
		FlushPeriod: 50 * time.Millisecond,
		BufferSize:  100,
	})

	logger := slog.New(h)

	// Log an error value — this should serialize as the error string, not {}.
	testErr := fmt.Errorf("connection refused: %w", io.ErrUnexpectedEOF)
	logger.Error("query failed", "err", testErr)

	time.Sleep(200 * time.Millisecond)

	mu.Lock()
	defer mu.Unlock()

	if len(received) == 0 {
		t.Fatal("expected at least one batch")
	}

	entry := received[0].Logs[0]
	var attrs map[string]any
	if err := json.Unmarshal(entry.Attrs, &attrs); err != nil {
		t.Fatalf("unmarshal attrs: %v", err)
	}

	errVal, ok := attrs["err"].(string)
	if !ok {
		t.Fatalf("err attr is %T, want string (got %v)", attrs["err"], attrs["err"])
	}
	if errVal != testErr.Error() {
		t.Errorf("err = %q, want %q", errVal, testErr.Error())
	}
}

func TestHandler_Dropped(t *testing.T) {
	h := mustNew(t, Config{
		Endpoint:    "http://localhost:0/unreachable",
		APIKey:      "test-key",
		Service:     "test-svc",
		BatchSize:   100,
		FlushPeriod: time.Hour,
		BufferSize:  2, // Tiny buffer.
	})
	defer h.Shutdown(context.Background()) //nolint:errcheck

	logger := slog.New(h)

	// Overflow the buffer.
	for range 10 {
		logger.Info("overflow")
	}

	if h.Dropped() == 0 {
		t.Error("expected some dropped entries")
	}
}

func TestMultiHandler(t *testing.T) {
	var mu sync.Mutex
	var received []ingestRequest

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		body, _ := io.ReadAll(r.Body)
		var req ingestRequest
		json.Unmarshal(body, &req) //nolint:errcheck
		mu.Lock()
		received = append(received, req)
		mu.Unlock()
		w.WriteHeader(http.StatusAccepted)
	}))
	defer srv.Close()

	shipper := mustNew(t, Config{
		Endpoint:    srv.URL,
		APIKey:      "test-key",
		Service:     "multi-test",
		BatchSize:   100,
		FlushPeriod: 50 * time.Millisecond,
		BufferSize:  100,
	})

	logger := slog.New(MultiHandler(
		shipper,
		slog.NewTextHandler(io.Discard, nil),
	))

	logger.Info("multi handler test")

	time.Sleep(200 * time.Millisecond)

	mu.Lock()
	defer mu.Unlock()

	if len(received) == 0 {
		t.Fatal("expected shipper to receive the log")
	}
	if received[0].Logs[0].Msg != "multi handler test" {
		t.Errorf("msg = %q, want %q", received[0].Logs[0].Msg, "multi handler test")
	}
}

func TestHandler_LevelMapping(t *testing.T) {
	var mu sync.Mutex
	var received []ingestRequest

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		body, _ := io.ReadAll(r.Body)
		var req ingestRequest
		json.Unmarshal(body, &req) //nolint:errcheck
		mu.Lock()
		received = append(received, req)
		mu.Unlock()
		w.WriteHeader(http.StatusAccepted)
	}))
	defer srv.Close()

	h := mustNew(t, Config{
		Endpoint:    srv.URL,
		APIKey:      "test-key",
		Service:     "test-svc",
		MinLevel:    slog.LevelDebug,
		BatchSize:   100,
		FlushPeriod: 50 * time.Millisecond,
		BufferSize:  100,
	})

	logger := slog.New(h)
	logger.Debug("debug msg")
	logger.Info("info msg")
	logger.Warn("warn msg")
	logger.Error("error msg")
	logger.Log(context.Background(), LevelFatal, "fatal msg")

	time.Sleep(200 * time.Millisecond)

	mu.Lock()
	defer mu.Unlock()

	// Collect all entries.
	entries := make([]logEntry, 0, len(received)*5)
	for _, r := range received {
		entries = append(entries, r.Logs...)
	}

	if len(entries) != 5 {
		t.Fatalf("got %d entries, want 5", len(entries))
	}

	want := []string{"DEBUG", "INFO", "WARN", "ERROR", "FATAL"}
	for i, e := range entries {
		if e.Level != want[i] {
			t.Errorf("entry[%d].Level = %q, want %q", i, e.Level, want[i])
		}
	}
}

func TestHandler_CustomLevelMapping(t *testing.T) {
	tests := []struct {
		level slog.Level
		want  string
	}{
		{slog.LevelDebug - 4, "DEBUG"}, // Custom sub-debug.
		{slog.LevelDebug, "DEBUG"},
		{slog.LevelInfo, "INFO"},
		{slog.LevelInfo + 2, "INFO"}, // Non-canonical, still below WARN threshold.
		{slog.LevelWarn, "WARN"},
		{slog.LevelError, "ERROR"},
		{slog.LevelError + 2, "ERROR"}, // Non-canonical, between ERROR and FATAL.
		{LevelFatal, "FATAL"},
		{LevelFatal + 4, "FATAL"}, // Beyond FATAL.
	}

	for _, tt := range tests {
		got := levelString(tt.level)
		if got != tt.want {
			t.Errorf("levelString(%d) = %q, want %q", tt.level, got, tt.want)
		}
	}
}

func TestHandler_DurationSerialization(t *testing.T) {
	var mu sync.Mutex
	var received []ingestRequest

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		body, _ := io.ReadAll(r.Body)
		var req ingestRequest
		json.Unmarshal(body, &req) //nolint:errcheck
		mu.Lock()
		received = append(received, req)
		mu.Unlock()
		w.WriteHeader(http.StatusAccepted)
	}))
	defer srv.Close()

	h := mustNew(t, Config{
		Endpoint:    srv.URL,
		APIKey:      "test-key",
		Service:     "test-svc",
		BatchSize:   100,
		FlushPeriod: 50 * time.Millisecond,
		BufferSize:  100,
	})

	logger := slog.New(h)
	logger.Info("timing", "elapsed", 42*time.Millisecond)

	time.Sleep(200 * time.Millisecond)

	mu.Lock()
	defer mu.Unlock()

	if len(received) == 0 {
		t.Fatal("expected at least one batch")
	}

	entry := received[0].Logs[0]
	var attrs map[string]any
	if err := json.Unmarshal(entry.Attrs, &attrs); err != nil {
		t.Fatalf("unmarshal attrs: %v", err)
	}

	elapsed, ok := attrs["elapsed"].(string)
	if !ok {
		t.Fatalf("elapsed attr is %T, want string (got %v)", attrs["elapsed"], attrs["elapsed"])
	}
	if elapsed != "42ms" {
		t.Errorf("elapsed = %q, want %q", elapsed, "42ms")
	}
}

func TestHandler_StringerSerialization(t *testing.T) {
	var mu sync.Mutex
	var received []ingestRequest

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		body, _ := io.ReadAll(r.Body)
		var req ingestRequest
		json.Unmarshal(body, &req) //nolint:errcheck
		mu.Lock()
		received = append(received, req)
		mu.Unlock()
		w.WriteHeader(http.StatusAccepted)
	}))
	defer srv.Close()

	h := mustNew(t, Config{
		Endpoint:    srv.URL,
		APIKey:      "test-key",
		Service:     "test-svc",
		BatchSize:   100,
		FlushPeriod: 50 * time.Millisecond,
		BufferSize:  100,
	})

	u, _ := url.Parse("https://example.com/path?q=1")
	logger := slog.New(h)
	logger.Info("request", "url", u)

	time.Sleep(200 * time.Millisecond)

	mu.Lock()
	defer mu.Unlock()

	if len(received) == 0 {
		t.Fatal("expected at least one batch")
	}

	entry := received[0].Logs[0]
	var attrs map[string]any
	if err := json.Unmarshal(entry.Attrs, &attrs); err != nil {
		t.Fatalf("unmarshal attrs: %v", err)
	}

	urlStr, ok := attrs["url"].(string)
	if !ok {
		t.Fatalf("url attr is %T, want string (got %v)", attrs["url"], attrs["url"])
	}
	if urlStr != "https://example.com/path?q=1" {
		t.Errorf("url = %q, want %q", urlStr, "https://example.com/path?q=1")
	}
}

func TestHandler_JSONMarshalerPreserved(t *testing.T) {
	var mu sync.Mutex
	var received []ingestRequest

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		body, _ := io.ReadAll(r.Body)
		var req ingestRequest
		json.Unmarshal(body, &req) //nolint:errcheck
		mu.Lock()
		received = append(received, req)
		mu.Unlock()
		w.WriteHeader(http.StatusAccepted)
	}))
	defer srv.Close()

	h := mustNew(t, Config{
		Endpoint:    srv.URL,
		APIKey:      "test-key",
		Service:     "test-svc",
		BatchSize:   100,
		FlushPeriod: 50 * time.Millisecond,
		BufferSize:  100,
	})

	// time.Time implements both json.Marshaler and fmt.Stringer.
	// It should keep its JSON form (RFC3339), not use String().
	ts := time.Date(2024, 6, 15, 10, 30, 0, 0, time.UTC)
	logger := slog.New(h)
	logger.Info("event", "created_at", ts)

	time.Sleep(200 * time.Millisecond)

	mu.Lock()
	defer mu.Unlock()

	if len(received) == 0 {
		t.Fatal("expected at least one batch")
	}

	entry := received[0].Logs[0]
	var attrs map[string]any
	if err := json.Unmarshal(entry.Attrs, &attrs); err != nil {
		t.Fatalf("unmarshal attrs: %v", err)
	}

	// time.Time with json.Marshaler should produce an RFC3339 string.
	createdAt, ok := attrs["created_at"].(string)
	if !ok {
		t.Fatalf("created_at attr is %T, want string (got %v)", attrs["created_at"], attrs["created_at"])
	}
	if _, err := time.Parse(time.RFC3339Nano, createdAt); err != nil {
		t.Errorf("created_at %q is not valid RFC3339: %v", createdAt, err)
	}
}

func TestHandler_SendFailedCounter(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusInternalServerError)
	}))
	defer srv.Close()

	h := mustNew(t, Config{
		Endpoint:    srv.URL,
		APIKey:      "test-key",
		Service:     "test-svc",
		BatchSize:   5,
		FlushPeriod: 50 * time.Millisecond,
		BufferSize:  100,
	})

	logger := slog.New(h)
	for range 5 {
		logger.Info("will fail")
	}

	// Wait for flush + retry attempts.
	time.Sleep(300 * time.Millisecond)

	if h.SendFailed() == 0 {
		t.Error("expected SendFailed > 0")
	}
}

func TestHandler_AddSource(t *testing.T) {
	var mu sync.Mutex
	var received []ingestRequest

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		body, _ := io.ReadAll(r.Body)
		var req ingestRequest
		json.Unmarshal(body, &req) //nolint:errcheck
		mu.Lock()
		received = append(received, req)
		mu.Unlock()
		w.WriteHeader(http.StatusAccepted)
	}))
	defer srv.Close()

	h := mustNew(t, Config{
		Endpoint:    srv.URL,
		APIKey:      "test-key",
		Service:     "test-svc",
		AddSource:   true,
		BatchSize:   100,
		FlushPeriod: 50 * time.Millisecond,
		BufferSize:  100,
	})

	logger := slog.New(h)
	logger.Info("source test") // This line's file:line should appear in Source.

	time.Sleep(200 * time.Millisecond)

	mu.Lock()
	defer mu.Unlock()

	if len(received) == 0 {
		t.Fatal("expected at least one batch")
	}

	entry := received[0].Logs[0]
	if entry.Source == "" {
		t.Fatal("expected source to be populated")
	}
	// Should point to this test file.
	if !strings.Contains(entry.Source, "handler_test.go:") {
		t.Errorf("source = %q, want it to contain handler_test.go:", entry.Source)
	}
}

func TestHandler_AddSourceDisabled(t *testing.T) {
	var mu sync.Mutex
	var received []ingestRequest

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		body, _ := io.ReadAll(r.Body)
		var req ingestRequest
		json.Unmarshal(body, &req) //nolint:errcheck
		mu.Lock()
		received = append(received, req)
		mu.Unlock()
		w.WriteHeader(http.StatusAccepted)
	}))
	defer srv.Close()

	h := mustNew(t, Config{
		Endpoint:    srv.URL,
		APIKey:      "test-key",
		Service:     "test-svc",
		AddSource:   false,
		BatchSize:   100,
		FlushPeriod: 50 * time.Millisecond,
		BufferSize:  100,
	})

	logger := slog.New(h)
	logger.Info("no source")

	time.Sleep(200 * time.Millisecond)

	mu.Lock()
	defer mu.Unlock()

	if len(received) == 0 {
		t.Fatal("expected at least one batch")
	}

	entry := received[0].Logs[0]
	if entry.Source != "" {
		t.Errorf("expected empty source, got %q", entry.Source)
	}
}

// recordingServer spins up an httptest.Server that unmarshals every incoming
// ingestRequest and appends it to a shared slice, returning 202. Shared
// between many tests to cut boilerplate.
func recordingServer(t *testing.T, received *[]ingestRequest, mu *sync.Mutex) *httptest.Server {
	t.Helper()
	return httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		body, _ := io.ReadAll(r.Body)
		var req ingestRequest
		if err := json.Unmarshal(body, &req); err != nil {
			t.Errorf("unmarshal request: %v", err)
			w.WriteHeader(http.StatusBadRequest)
			return
		}
		mu.Lock()
		*received = append(*received, req)
		mu.Unlock()
		w.WriteHeader(http.StatusAccepted)
	}))
}

func TestHandler_Redact(t *testing.T) {
	var mu sync.Mutex
	var received []ingestRequest
	srv := recordingServer(t, &received, &mu)
	defer srv.Close()

	h := mustNew(t, Config{
		Endpoint:    srv.URL,
		APIKey:      "test-key",
		Service:     "test-svc",
		BatchSize:   10,
		FlushPeriod: 50 * time.Millisecond,
		BufferSize:  100,
		Redact: func(key string, v any) any {
			switch key {
			case "password":
				return "[REDACTED]"
			case "secret":
				return nil
			}
			return v
		},
	})

	logger := slog.New(h)
	logger.Info("login", "user", "alice", "password", "hunter2", "secret", "drop-me")

	time.Sleep(200 * time.Millisecond)
	if err := h.Shutdown(context.Background()); err != nil {
		t.Fatalf("shutdown: %v", err)
	}

	mu.Lock()
	defer mu.Unlock()
	if len(received) == 0 {
		t.Fatal("no batch received")
	}
	var attrs map[string]any
	if err := json.Unmarshal(received[0].Logs[0].Attrs, &attrs); err != nil {
		t.Fatalf("unmarshal attrs: %v", err)
	}
	if attrs["password"] != "[REDACTED]" {
		t.Errorf("password = %v, want [REDACTED]", attrs["password"])
	}
	if _, present := attrs["secret"]; present {
		t.Errorf("secret should have been dropped, got %v", attrs["secret"])
	}
	if attrs["user"] != "alice" {
		t.Errorf("user = %v, want alice", attrs["user"])
	}
}

func TestHandler_WithGroupNesting(t *testing.T) {
	var mu sync.Mutex
	var received []ingestRequest
	srv := recordingServer(t, &received, &mu)
	defer srv.Close()

	h := mustNew(t, Config{
		Endpoint:    srv.URL,
		APIKey:      "test-key",
		Service:     "test-svc",
		BatchSize:   10,
		FlushPeriod: 50 * time.Millisecond,
		BufferSize:  100,
	})

	logger := slog.New(h).WithGroup("http").With("remote", "1.2.3.4")
	logger.Info("request", "method", "GET", "path", "/api/users")

	time.Sleep(200 * time.Millisecond)
	if err := h.Shutdown(context.Background()); err != nil {
		t.Fatalf("shutdown: %v", err)
	}

	mu.Lock()
	defer mu.Unlock()
	if len(received) == 0 {
		t.Fatal("no batch received")
	}
	var attrs map[string]any
	if err := json.Unmarshal(received[0].Logs[0].Attrs, &attrs); err != nil {
		t.Fatalf("unmarshal attrs: %v", err)
	}
	httpGroup, ok := attrs["http"].(map[string]any)
	if !ok {
		t.Fatalf("expected attrs[\"http\"] to be a map, got %T: %v", attrs["http"], attrs)
	}
	for k, want := range map[string]string{"remote": "1.2.3.4", "method": "GET", "path": "/api/users"} {
		if httpGroup[k] != want {
			t.Errorf("http.%s = %v, want %q", k, httpGroup[k], want)
		}
	}
}

func TestHandler_WithBeforeWithGroupStaysOutside(t *testing.T) {
	endpoint, entries := captureServer(t)
	h := mustNew(t, Config{Endpoint: endpoint, APIKey: "k", Service: "s", FlushPeriod: time.Hour})

	slog.New(h).With("a", 1).WithGroup("g").Info("m", "b", 2)
	slog.New(h).WithGroup("empty").Info("no attrs")
	if err := h.Shutdown(context.Background()); err != nil {
		t.Fatalf("Shutdown: %v", err)
	}

	got := entries()
	if len(got) != 2 {
		t.Fatalf("received %d entries, want 2", len(got))
	}
	if want := `{"a":1,"g":{"b":2}}`; string(got[0].Attrs) != want {
		t.Errorf("attrs = %s, want %s", got[0].Attrs, want)
	}
	if len(got[1].Attrs) != 0 {
		t.Errorf("group with no attrs was emitted: %s", got[1].Attrs)
	}
}

// TestHandler_SlogConformance runs the testing/slogtest suite. One case is an
// intended deviation: slog expects a zero record time to be left out, but the
// ingest API requires a timestamp, so the handler stamps the current time.
func TestHandler_SlogConformance(t *testing.T) {
	endpoint, entries := captureServer(t)
	h := mustNew(t, Config{Endpoint: endpoint, APIKey: "k", Service: "s", AddSource: true, FlushPeriod: time.Hour})

	results := func() []map[string]any {
		if err := h.Shutdown(context.Background()); err != nil {
			t.Fatalf("Shutdown: %v", err)
		}
		got := entries()
		out := make([]map[string]any, 0, len(got))
		for _, e := range got {
			m := map[string]any{}
			if len(e.Attrs) > 0 {
				if err := json.Unmarshal(e.Attrs, &m); err != nil {
					t.Fatalf("unmarshal attrs: %v", err)
				}
			}
			m[slog.TimeKey] = e.Timestamp
			m[slog.LevelKey] = e.Level
			m[slog.MessageKey] = e.Msg
			if e.Source != "" {
				m[slog.SourceKey] = e.Source
			}
			out = append(out, m)
		}
		return out
	}

	err := slogtest.TestHandler(h, results)
	if err == nil {
		t.Fatal("slogtest passed the zero-time case; drop the deviation from this test")
	}
	var errs []error
	if joined, ok := err.(interface{ Unwrap() []error }); ok { //nolint:errorlint // Splitting errors.Join output, not matching a cause.
		errs = joined.Unwrap()
	} else {
		errs = []error{err}
	}
	for _, e := range errs {
		if !strings.Contains(e.Error(), "a Handler should ignore a zero Record.Time") {
			t.Errorf("slogtest: %v", e)
		}
	}
	if len(errs) != 1 {
		t.Errorf("slogtest reported %d problems, want only the zero-time deviation", len(errs))
	}
}

// roundTripperFunc is a test hook for injecting a custom http.RoundTripper.
type roundTripperFunc func(*http.Request) (*http.Response, error)

func (f roundTripperFunc) RoundTrip(r *http.Request) (*http.Response, error) { return f(r) }

func TestHandler_CustomClient(t *testing.T) {
	var mu sync.Mutex
	var received []ingestRequest
	srv := recordingServer(t, &received, &mu)
	defer srv.Close()

	var customCalls atomic.Int64
	custom := &http.Client{
		Transport: roundTripperFunc(func(r *http.Request) (*http.Response, error) {
			customCalls.Add(1)
			return http.DefaultTransport.RoundTrip(r)
		}),
		Timeout: 5 * time.Second,
	}

	h := mustNew(t, Config{
		Endpoint:    srv.URL,
		APIKey:      "test-key",
		Service:     "test-svc",
		BatchSize:   10,
		FlushPeriod: 50 * time.Millisecond,
		BufferSize:  100,
		Client:      custom,
		// Must be ignored when Client is set; if it weren't, we'd see a
		// different client and customCalls would stay zero.
		InsecureSkipVerify: true,
	})

	logger := slog.New(h)
	logger.Info("via custom client")

	time.Sleep(200 * time.Millisecond)
	if err := h.Shutdown(context.Background()); err != nil {
		t.Fatalf("shutdown: %v", err)
	}

	if customCalls.Load() == 0 {
		t.Fatal("expected custom transport to be called, got 0 calls")
	}
	mu.Lock()
	defer mu.Unlock()
	if len(received) == 0 {
		t.Fatal("no batch received")
	}
}

func TestHandler_EndpointValidation(t *testing.T) {
	tests := []struct {
		name     string
		endpoint string
		wantErr  bool
	}{
		{"valid_https", "https://example.com/api", false},
		{"valid_http", "http://example.com/api", false},
		{"empty", "", true},
		{"bad_scheme_file", "file:///etc/passwd", true},
		{"bad_scheme_gopher", "gopher://example.com", true},
		{"no_host", "http://", true},
		{"host_only_missing_scheme", "example.com/api", true},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			h, err := New(Config{
				Endpoint: tt.endpoint,
				APIKey:   "k",
				Service:  "s",
			})
			if h != nil {
				defer h.Shutdown(context.Background()) //nolint:errcheck // Cleanup.
			}
			if (err != nil) != tt.wantErr {
				t.Errorf("New(%q) err = %v, wantErr = %v", tt.endpoint, err, tt.wantErr)
			}
		})
	}
}

func TestNew_RequiresService(t *testing.T) {
	// A failing hostname lookup must not mask the missing service.
	orig := osHostname
	osHostname = func() (string, error) { return "", errors.New("lookup failed") }
	t.Cleanup(func() { osHostname = orig })

	h, err := New(Config{
		Endpoint: "https://example.com/api",
		APIKey:   "k",
	})
	if h != nil {
		defer h.Shutdown(context.Background()) //nolint:errcheck // Cleanup.
	}
	if err == nil {
		t.Fatal("New with empty Service: expected error, got nil")
	}
	if !strings.Contains(err.Error(), "service is required") {
		t.Errorf("New with empty Service: err = %v, want the service error", err)
	}
}

func TestNew_ResolvesHost(t *testing.T) {
	errLookup := errors.New("lookup failed")
	tests := []struct {
		name     string
		host     string
		lookup   func() (string, error)
		wantHost string
		wantErr  bool
		wantIs   error
	}{
		{"explicit_host_skips_lookup", "prod-1", func() (string, error) { return "", errLookup }, "prod-1", false, nil},
		{"falls_back_to_hostname", "", func() (string, error) { return "box", nil }, "box", false, nil},
		{"lookup_error", "", func() (string, error) { return "", errLookup }, "", true, errLookup},
		{"empty_hostname", "", func() (string, error) { return "", nil }, "", true, nil},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			orig := osHostname
			osHostname = tt.lookup
			t.Cleanup(func() { osHostname = orig })

			h, err := New(Config{
				Endpoint: "https://example.com/api",
				APIKey:   "k",
				Service:  "s",
				Host:     tt.host,
			})
			if h != nil {
				defer h.Shutdown(context.Background()) //nolint:errcheck // Cleanup.
			}
			if (err != nil) != tt.wantErr {
				t.Fatalf("New err = %v, wantErr = %v", err, tt.wantErr)
			}
			if tt.wantIs != nil && !errors.Is(err, tt.wantIs) {
				t.Errorf("New err = %v, want it to wrap %v", err, tt.wantIs)
			}
			if err == nil && h.cfg.Host != tt.wantHost {
				t.Errorf("Host = %q, want %q", h.cfg.Host, tt.wantHost)
			}
		})
	}
}

func TestNew_RejectsOverLengthFields(t *testing.T) {
	tests := []struct {
		name    string
		set     func(c *Config, n int)
		limit   int
		wantErr string
	}{
		{"service", func(c *Config, n int) { c.Service = strings.Repeat("s", n) }, maxServiceBytes, "service is"},
		{"component", func(c *Config, n int) { c.Component = strings.Repeat("c", n) }, maxComponentBytes, "component is"},
		{"host", func(c *Config, n int) { c.Host = strings.Repeat("h", n) }, maxHostBytes, "host is"},
	}
	for _, tt := range tests {
		for _, over := range []bool{false, true} {
			n, name := tt.limit, tt.name+"_at_limit"
			if over {
				n, name = tt.limit+1, tt.name+"_over_limit"
			}
			t.Run(name, func(t *testing.T) {
				cfg := Config{Endpoint: "https://example.com/api", APIKey: "k", Service: "s"}
				tt.set(&cfg, n)
				h, err := New(cfg)
				if h != nil {
					defer h.Shutdown(context.Background()) //nolint:errcheck // Cleanup.
				}
				if (err != nil) != over {
					t.Fatalf("New with %d bytes: err = %v, wantErr = %v", n, err, over)
				}
				if over && !strings.Contains(err.Error(), tt.wantErr) {
					t.Errorf("New err = %v, want it to name the field (%q)", err, tt.wantErr)
				}
			})
		}
	}
}

func TestHandler_FillsRequiredEntryFields(t *testing.T) {
	endpoint, entries := captureServer(t)
	h := mustNew(t, Config{Endpoint: endpoint, APIKey: "k", Service: "s"})

	before := time.Now()
	r := slog.NewRecord(time.Time{}, slog.LevelInfo, "", 0)
	if err := h.Handle(context.Background(), r); err != nil {
		t.Fatalf("Handle: %v", err)
	}
	if err := h.Shutdown(context.Background()); err != nil {
		t.Fatalf("Shutdown: %v", err)
	}

	got := entries()
	if len(got) != 1 {
		t.Fatalf("received %d entries, want 1", len(got))
	}
	if got[0].Timestamp.Before(before) {
		t.Errorf("timestamp = %v, want the current time for a zero Record.Time", got[0].Timestamp)
	}
	if got[0].Msg != emptyMsgPlaceholder {
		t.Errorf("msg = %q, want %q for an empty message", got[0].Msg, emptyMsgPlaceholder)
	}
}

func TestHandler_KeepsUserSourceAttr(t *testing.T) {
	endpoint, entries := captureServer(t)
	h := mustNew(t, Config{Endpoint: endpoint, APIKey: "k", Service: "s", AddSource: true})

	slog.New(h).Info("m", "source", "kafka")
	if err := h.Shutdown(context.Background()); err != nil {
		t.Fatalf("Shutdown: %v", err)
	}

	got := entries()
	if len(got) != 1 {
		t.Fatalf("received %d entries, want 1", len(got))
	}
	var attrs map[string]any
	if err := json.Unmarshal(got[0].Attrs, &attrs); err != nil {
		t.Fatalf("unmarshal attrs: %v", err)
	}
	if attrs["source"] != "kafka" {
		t.Errorf("attrs[%q] = %v, want %q", "source", attrs["source"], "kafka")
	}
	if !strings.Contains(got[0].Source, "handler_test.go:") {
		t.Errorf("source = %q, want it to contain handler_test.go:", got[0].Source)
	}
}

// nilDerefError is an error whose Error method dereferences its receiver.
type nilDerefError struct{ msg string }

func (e *nilDerefError) Error() string { return e.msg }

func TestHandler_NilReceiverAttrDoesNotPanic(t *testing.T) {
	endpoint, entries := captureServer(t)
	h := mustNew(t, Config{Endpoint: endpoint, APIKey: "k", Service: "s"})

	var nilErr *nilDerefError
	slog.New(h).Info("nil receivers", "url", (*url.URL)(nil), "err", nilErr)
	if err := h.Shutdown(context.Background()); err != nil {
		t.Fatalf("Shutdown: %v", err)
	}

	got := entries()
	if len(got) != 1 {
		t.Fatalf("received %d entries, want 1", len(got))
	}
	var attrs map[string]any
	if err := json.Unmarshal(got[0].Attrs, &attrs); err != nil {
		t.Fatalf("unmarshal attrs: %v", err)
	}
	for _, key := range []string{"url", "err"} {
		if attrs[key] != "<nil>" {
			t.Errorf("attrs[%q] = %v, want %q", key, attrs[key], "<nil>")
		}
	}
}

func TestHandler_RedirectCountsAsSendFailure(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Location", "https://example.invalid/")
		w.WriteHeader(http.StatusMovedPermanently)
	}))
	defer srv.Close()

	h := mustNew(t, Config{Endpoint: srv.URL, APIKey: "k", Service: "s"})
	logger := slog.New(h)
	for range 3 {
		logger.Info("m")
	}
	if err := h.Shutdown(context.Background()); err != nil {
		t.Fatalf("Shutdown: %v", err)
	}

	if got := h.SendFailed(); got != 3 {
		t.Errorf("SendFailed = %d, want 3: a redirect is not a delivery", got)
	}
}

func TestTruncateMsg(t *testing.T) {
	tests := []struct {
		name string
		msg  string
	}{
		{"short", "hello"},
		{"exactly_at_cap", strings.Repeat("a", maxMsgBytes)},
		{"one_over_cap", strings.Repeat("a", maxMsgBytes+1)},
		{"far_over_cap", strings.Repeat("a", 3*maxMsgBytes)},
		{"multibyte_at_boundary", strings.Repeat("é", maxMsgBytes)},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := truncateMsg(tt.msg)
			if len(tt.msg) <= maxMsgBytes {
				if got != tt.msg {
					t.Errorf("msg under cap was modified: len %d → %d", len(tt.msg), len(got))
				}
				return
			}
			if len(got) > maxMsgBytes {
				t.Errorf("truncated msg is %d bytes, want <= %d", len(got), maxMsgBytes)
			}
			if !strings.HasSuffix(got, truncationSuffix) {
				t.Errorf("truncated msg missing %q suffix", truncationSuffix)
			}
			if !utf8.ValidString(got) {
				t.Error("truncated msg is not valid UTF-8")
			}
		})
	}
}

func TestHandler_TruncatesOversizedMsg(t *testing.T) {
	var mu sync.Mutex
	var received []ingestRequest

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		body, _ := io.ReadAll(r.Body)
		var req ingestRequest
		json.Unmarshal(body, &req) //nolint:errcheck
		mu.Lock()
		received = append(received, req)
		mu.Unlock()
		w.WriteHeader(http.StatusAccepted)
	}))
	defer srv.Close()

	h := mustNew(t, Config{
		Endpoint:    srv.URL,
		APIKey:      "k",
		Service:     "test-svc",
		BatchSize:   1,
		FlushPeriod: 50 * time.Millisecond,
		BufferSize:  10,
	})

	logger := slog.New(h)
	logger.Info(strings.Repeat("x", maxMsgBytes+100))

	time.Sleep(200 * time.Millisecond)
	if err := h.Shutdown(context.Background()); err != nil {
		t.Fatalf("shutdown: %v", err)
	}

	mu.Lock()
	defer mu.Unlock()
	if len(received) == 0 || len(received[0].Logs) == 0 {
		t.Fatal("no batch received")
	}
	msg := received[0].Logs[0].Msg
	if len(msg) > maxMsgBytes {
		t.Errorf("shipped msg is %d bytes, want <= %d", len(msg), maxMsgBytes)
	}
	if !strings.HasSuffix(msg, truncationSuffix) {
		t.Errorf("shipped msg missing %q suffix", truncationSuffix)
	}
}

func TestHandler_InvalidUTF8MsgStaysUnderCap(t *testing.T) {
	endpoint, entries := captureServer(t)
	h := mustNew(t, Config{Endpoint: endpoint, APIKey: "k", Service: "s", FlushPeriod: time.Hour})

	// 40 KB on the client. JSON encoding turns each invalid byte into a
	// 3-byte U+FFFD, so the server would decode 80 KB, over the 64 KB cap.
	slog.New(h).Info(strings.Repeat("\xffa", 20000))
	if err := h.Shutdown(context.Background()); err != nil {
		t.Fatalf("Shutdown: %v", err)
	}

	got := entries()
	if len(got) != 1 {
		t.Fatalf("received %d entries, want 1", len(got))
	}
	msg := got[0].Msg
	if !utf8.ValidString(msg) {
		t.Error("shipped msg is not valid UTF-8")
	}
	if len(msg) > maxMsgBytes {
		t.Errorf("decoded msg is %d bytes, want <= %d", len(msg), maxMsgBytes)
	}
}

func TestHandler_OversizedAttrsShipAsStub(t *testing.T) {
	endpoint, entries := captureServer(t)
	h := mustNew(t, Config{Endpoint: endpoint, APIKey: "k", Service: "s", FlushPeriod: time.Hour})
	logger := slog.New(h)

	// `{"body":"` + value + `"}` adds 11 bytes around the value.
	atCap := strings.Repeat("y", maxAttrsBytes-11)
	logger.Info("small", "k", "v")
	logger.Info("big", "body", strings.Repeat("x", 100*1024)) // Encodes to 102,411 bytes.
	logger.Info("at cap", "body", atCap)
	if err := h.Shutdown(context.Background()); err != nil {
		t.Fatalf("Shutdown: %v", err)
	}

	got := entries()
	if len(got) != 3 {
		t.Fatalf("received %d entries, want 3", len(got))
	}
	if string(got[0].Attrs) != `{"k":"v"}` {
		t.Errorf("small attrs = %s, want {\"k\":\"v\"}", got[0].Attrs)
	}
	if want := `{"_attrs_dropped_bytes":102411}`; string(got[1].Attrs) != want {
		t.Errorf("big attrs = %.80s, want %s", got[1].Attrs, want)
	}
	if got[1].Msg != "big" {
		t.Errorf("big msg = %q, want %q", got[1].Msg, "big")
	}
	if want := `{"body":"` + atCap + `"}`; string(got[2].Attrs) != want {
		t.Errorf("attrs at the cap were changed: %d bytes, want %d", len(got[2].Attrs), len(want))
	}
	if h.EncodeFailed() != 1 {
		t.Errorf("EncodeFailed = %d, want 1", h.EncodeFailed())
	}
	if h.SendFailed() != 0 {
		t.Errorf("SendFailed = %d, want 0", h.SendFailed())
	}
}

func TestShortenSource(t *testing.T) {
	longDir := "/" + strings.Repeat("deep/", 60) // 301 bytes.
	tests := []struct {
		name   string
		source string
		want   string // Exact result; empty means check only the cap and tail.
		tail   string
	}{
		{name: "short", source: "/src/app/main.go:42", want: "/src/app/main.go:42"},
		{name: "exactly_at_cap", source: strings.Repeat("a", maxSourceBytes-len("x.go:1")) + "x.go:1", tail: "x.go:1"},
		{name: "long_path", source: longDir + "handler.go:120", tail: "/handler.go:120"},
		{name: "multibyte_at_cut", source: "/" + strings.Repeat("é", 200) + "/main.go:7", tail: "/main.go:7"},
		{name: "invalid_bytes", source: "/src/\xff/main.go:3", want: "/src/�/main.go:3"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := shortenSource(tt.source)
			if tt.want != "" && got != tt.want {
				t.Errorf("shortenSource = %q, want %q", got, tt.want)
			}
			if len(got) > maxSourceBytes {
				t.Errorf("result is %d bytes, want <= %d", len(got), maxSourceBytes)
			}
			if !strings.HasSuffix(got, tt.tail) {
				t.Errorf("result %q does not end with %q", got, tt.tail)
			}
			if !utf8.ValidString(got) {
				t.Error("result is not valid UTF-8")
			}
			if len(tt.source) > maxSourceBytes && !strings.HasPrefix(got, sourceCutPrefix) {
				t.Errorf("shortened result %q lacks the %q prefix", got, sourceCutPrefix)
			}
		})
	}
}

func TestHandler_SendTimeoutBoundsHungEndpoint(t *testing.T) {
	// Server that hangs longer than SendTimeout.
	srv := httptest.NewServer(http.HandlerFunc(func(_ http.ResponseWriter, r *http.Request) {
		select {
		case <-time.After(5 * time.Second):
		case <-r.Context().Done():
		}
	}))
	defer srv.Close()

	h := mustNew(t, Config{
		Endpoint:    srv.URL,
		APIKey:      "k",
		Service:     "s",
		SendTimeout: 150 * time.Millisecond,
		BatchSize:   1,
		FlushPeriod: 50 * time.Millisecond,
		BufferSize:  10,
	})

	logger := slog.New(h)
	logger.Info("hangs")

	start := time.Now()
	// Long enough for timeout + retry on next tick + retry-drop.
	time.Sleep(800 * time.Millisecond)
	if err := h.Shutdown(context.Background()); err != nil {
		t.Fatalf("shutdown: %v", err)
	}
	elapsed := time.Since(start)

	if elapsed > 3*time.Second {
		t.Errorf("SendTimeout did not bound drain: elapsed=%v", elapsed)
	}
	if h.SendFailed() == 0 {
		t.Error("expected SendFailed > 0 after hung endpoint + retry")
	}
}

func TestHandler_ConcurrentHandleAndShutdown(t *testing.T) {
	var serverReceived atomic.Int64
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		body, _ := io.ReadAll(r.Body)
		var req ingestRequest
		if err := json.Unmarshal(body, &req); err != nil {
			t.Errorf("unmarshal request: %v", err)
			w.WriteHeader(http.StatusBadRequest)
			return
		}
		serverReceived.Add(int64(len(req.Logs)))
		w.WriteHeader(http.StatusAccepted)
	}))
	defer srv.Close()

	h := mustNew(t, Config{
		Endpoint:    srv.URL,
		APIKey:      "k",
		Service:     "s",
		BatchSize:   50,
		FlushPeriod: 10 * time.Millisecond,
		BufferSize:  2000,
	})

	const numGoroutines = 20
	const logsPerGoroutine = 100
	logger := slog.New(h)

	var wg sync.WaitGroup
	for i := 0; i < numGoroutines; i++ {
		wg.Add(1)
		go func(gid int) {
			defer wg.Done()
			for j := 0; j < logsPerGoroutine; j++ {
				logger.Info("concurrent", "gid", gid, "j", j)
			}
		}(i)
	}

	// Shut down while logging is in progress — the whole point of this test.
	time.Sleep(5 * time.Millisecond)
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	if err := h.Shutdown(ctx); err != nil {
		t.Fatalf("shutdown: %v", err)
	}
	wg.Wait()

	total := int64(numGoroutines * logsPerGoroutine)
	accounted := serverReceived.Load() + h.Dropped() + h.SendFailed() + h.EncodeFailed()
	if accounted != total {
		t.Errorf("accounting mismatch: total=%d, received=%d, dropped=%d, send_failed=%d, encode_failed=%d (sum=%d)",
			total, serverReceived.Load(), h.Dropped(), h.SendFailed(), h.EncodeFailed(), accounted)
	}
}

func TestHandler_ShutdownHonoursContextWithStuckHandle(t *testing.T) {
	endpoint, entries := captureServer(t)
	entered := make(chan struct{})
	release := make(chan struct{})
	h := mustNew(t, Config{
		Endpoint:    endpoint,
		APIKey:      "k",
		Service:     "s",
		FlushPeriod: time.Hour,
		Redact: func(key string, v any) any {
			if key == "block" {
				close(entered)
				<-release
			}
			return v
		},
	})
	logger := slog.New(h)
	var releaseOnce sync.Once
	unblock := func() { releaseOnce.Do(func() { close(release) }) }
	t.Cleanup(unblock) // Unwind the stuck call even if an assertion fails.

	stuck := make(chan struct{})
	go func() {
		defer close(stuck)
		logger.Info("stuck", "block", true)
	}()
	<-entered

	// Two concurrent Shutdown calls, each must return at its own deadline.
	const slack = 500 * time.Millisecond
	deadlines := []time.Duration{100 * time.Millisecond, 300 * time.Millisecond}
	var wg sync.WaitGroup
	for _, d := range deadlines {
		wg.Add(1)
		go func(d time.Duration) {
			defer wg.Done()
			ctx, cancel := context.WithTimeout(context.Background(), d)
			defer cancel()
			start := time.Now()
			done := make(chan error, 1)
			go func() { done <- h.Shutdown(ctx) }()
			select {
			case err := <-done:
				if !errors.Is(err, context.DeadlineExceeded) {
					t.Errorf("Shutdown(%v) = %v, want context.DeadlineExceeded", d, err)
				}
				if elapsed := time.Since(start); elapsed < d || elapsed > d+slack {
					t.Errorf("Shutdown(%v) returned after %v", d, elapsed)
				}
			case <-time.After(d + slack):
				t.Errorf("Shutdown(%v) had not returned after %v", d, d+slack)
			}
		}(d)
	}
	wg.Wait()

	// A Handle call after the expired Shutdown is dropped and never blocks.
	later := make(chan struct{})
	go func() {
		defer close(later)
		logger.Info("later")
	}()
	select {
	case <-later:
	case <-time.After(slack):
		t.Fatal("Handle blocked after an expired Shutdown")
	}
	if got := h.Dropped(); got != 1 {
		t.Errorf("Dropped = %d, want 1 for the call after Shutdown", got)
	}

	// Once released, the stuck call finishes its push, the drain runs, and
	// its entry is accounted for: delivered, or failed because the first
	// Shutdown context had already expired.
	unblock()
	<-stuck
	if err := h.Shutdown(context.Background()); err != nil {
		t.Fatalf("final Shutdown: %v", err)
	}
	if got := int64(len(entries())) + h.SendFailed(); got != 1 {
		t.Errorf("stuck entry: delivered + SendFailed = %d, want 1", got)
	}
}

func TestHandler_RetryOnFirstFailure(t *testing.T) {
	var mu sync.Mutex
	var received []ingestRequest
	var callCount atomic.Int64

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if callCount.Add(1) == 1 {
			// First call fails; retry on next tick should succeed.
			w.WriteHeader(http.StatusServiceUnavailable)
			return
		}
		body, _ := io.ReadAll(r.Body)
		var req ingestRequest
		if err := json.Unmarshal(body, &req); err != nil {
			t.Errorf("unmarshal: %v", err)
			w.WriteHeader(http.StatusBadRequest)
			return
		}
		mu.Lock()
		received = append(received, req)
		mu.Unlock()
		w.WriteHeader(http.StatusAccepted)
	}))
	defer srv.Close()

	h := mustNew(t, Config{
		Endpoint:    srv.URL,
		APIKey:      "k",
		Service:     "s",
		BatchSize:   3,
		FlushPeriod: 50 * time.Millisecond,
		BufferSize:  100,
	})

	logger := slog.New(h)
	logger.Info("e1")
	logger.Info("e2")
	logger.Info("e3")
	// Wait for the first-failure retry to land.
	time.Sleep(300 * time.Millisecond)
	if err := h.Shutdown(context.Background()); err != nil {
		t.Fatalf("shutdown: %v", err)
	}

	mu.Lock()
	defer mu.Unlock()
	total := 0
	for _, r := range received {
		total += len(r.Logs)
	}
	if total < 3 {
		t.Errorf("retry delivered %d entries, want >= 3", total)
	}
	if h.SendFailed() != 0 {
		t.Errorf("SendFailed = %d, want 0 (retry succeeded)", h.SendFailed())
	}
}

// firstStatusServer answers code to the first request and 202 to the rest. It
// returns the endpoint, the entries accepted so far, and the request count.
func firstStatusServer(t *testing.T, code int) (string, func() []logEntry, *atomic.Int64) {
	t.Helper()
	var mu sync.Mutex
	var got []logEntry
	var calls atomic.Int64
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if calls.Add(1) == 1 {
			w.WriteHeader(code)
			return
		}
		var req ingestRequest
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			t.Errorf("decode request: %v", err)
		}
		mu.Lock()
		got = append(got, req.Logs...)
		mu.Unlock()
		w.WriteHeader(http.StatusAccepted)
	}))
	t.Cleanup(srv.Close)
	return srv.URL, func() []logEntry {
		mu.Lock()
		defer mu.Unlock()
		return append([]logEntry(nil), got...)
	}, &calls
}

// waitForCalls waits until the server has seen at least n requests, so a test
// knows a batch went out on the normal path rather than in the shutdown drain.
func waitForCalls(t *testing.T, calls *atomic.Int64, n int64) {
	t.Helper()
	deadline := time.Now().Add(5 * time.Second)
	for calls.Load() < n {
		if time.Now().After(deadline) {
			t.Fatalf("server saw %d requests after 5s, want %d", calls.Load(), n)
		}
		time.Sleep(time.Millisecond)
	}
}

func TestHandler_RejectedBatchNotMergedIntoNext(t *testing.T) {
	endpoint, entries, calls := firstStatusServer(t, http.StatusBadRequest)
	h := mustNew(t, Config{Endpoint: endpoint, APIKey: "k", Service: "s", BatchSize: 3, FlushPeriod: time.Hour})

	logger := slog.New(h)
	for i := range 3 {
		logger.Info("m", "i", i)
	}
	waitForCalls(t, calls, 1)
	for i := 3; i < 6; i++ {
		logger.Info("m", "i", i)
	}
	if err := h.Shutdown(context.Background()); err != nil {
		t.Fatalf("Shutdown: %v", err)
	}

	got := entries()
	if len(got) != 3 {
		t.Fatalf("delivered %d entries, want the 3 of the second batch", len(got))
	}
	if h.SendFailed() != 3 {
		t.Errorf("SendFailed = %d, want 3 (the rejected first batch)", h.SendFailed())
	}
}

func TestHandler_RetryPolicyByStatus(t *testing.T) {
	tests := []struct {
		code      int
		wantRetry bool
	}{
		{http.StatusBadRequest, false},
		{http.StatusUnauthorized, false},
		{http.StatusForbidden, false},
		{http.StatusRequestEntityTooLarge, false},
		{http.StatusRequestTimeout, true},
		{http.StatusTooManyRequests, true},
		{http.StatusInternalServerError, true},
		{http.StatusServiceUnavailable, true},
	}
	for _, tt := range tests {
		t.Run(strconv.Itoa(tt.code), func(t *testing.T) {
			endpoint, entries, calls := firstStatusServer(t, tt.code)
			h := mustNew(t, Config{Endpoint: endpoint, APIKey: "k", Service: "s", BatchSize: 3, FlushPeriod: time.Hour})

			logger := slog.New(h)
			for range 3 {
				logger.Info("m")
			}
			waitForCalls(t, calls, 1)
			// The final flush in Shutdown is where a retained batch is retried.
			if err := h.Shutdown(context.Background()); err != nil {
				t.Fatalf("Shutdown: %v", err)
			}

			wantDelivered, wantFailed, wantCalls := 0, int64(3), int64(1)
			if tt.wantRetry {
				wantDelivered, wantFailed, wantCalls = 3, 0, 2
			}
			if got := len(entries()); got != wantDelivered {
				t.Errorf("delivered %d entries, want %d", got, wantDelivered)
			}
			if got := h.SendFailed(); got != wantFailed {
				t.Errorf("SendFailed = %d, want %d", got, wantFailed)
			}
			if got := calls.Load(); got != wantCalls {
				t.Errorf("server saw %d requests, want %d", got, wantCalls)
			}
		})
	}
}

func TestHandler_EncodeFailedShipsStub(t *testing.T) {
	var mu sync.Mutex
	var received []ingestRequest
	srv := recordingServer(t, &received, &mu)
	defer srv.Close()

	h := mustNew(t, Config{
		Endpoint:    srv.URL,
		APIKey:      "k",
		Service:     "s",
		BatchSize:   10,
		FlushPeriod: 50 * time.Millisecond,
		BufferSize:  100,
	})

	logger := slog.New(h)
	// chan cannot be JSON-marshalled — forces an encode failure.
	ch := make(chan int)
	logger.Info("unmarshalable", "ch", ch)

	time.Sleep(200 * time.Millisecond)
	if err := h.Shutdown(context.Background()); err != nil {
		t.Fatalf("shutdown: %v", err)
	}

	if h.EncodeFailed() != 1 {
		t.Errorf("EncodeFailed = %d, want 1", h.EncodeFailed())
	}
	mu.Lock()
	defer mu.Unlock()
	if len(received) == 0 || len(received[0].Logs) == 0 {
		t.Fatal("expected entry to still ship despite encode failure")
	}
	var attrs map[string]any
	if err := json.Unmarshal(received[0].Logs[0].Attrs, &attrs); err != nil {
		t.Fatalf("unmarshal stub attrs: %v", err)
	}
	if _, ok := attrs["_encode_error"]; !ok {
		t.Errorf("expected _encode_error key in stub attrs, got %v", attrs)
	}
	if received[0].Logs[0].Msg != "unmarshalable" {
		t.Errorf("msg lost: got %q", received[0].Logs[0].Msg)
	}
}

func TestHandler_MaxAttrBytesTruncatesStrings(t *testing.T) {
	var mu sync.Mutex
	var received []ingestRequest
	srv := recordingServer(t, &received, &mu)
	defer srv.Close()

	h := mustNew(t, Config{
		Endpoint:     srv.URL,
		APIKey:       "k",
		Service:      "s",
		BatchSize:    10,
		FlushPeriod:  50 * time.Millisecond,
		BufferSize:   100,
		MaxAttrBytes: 16,
	})

	logger := slog.New(h)
	longStr := strings.Repeat("x", 500)
	logger.Info("big", "payload", longStr, "short", "ok")

	time.Sleep(200 * time.Millisecond)
	if err := h.Shutdown(context.Background()); err != nil {
		t.Fatalf("shutdown: %v", err)
	}

	mu.Lock()
	defer mu.Unlock()
	if len(received) == 0 {
		t.Fatal("no batch received")
	}
	var attrs map[string]any
	if err := json.Unmarshal(received[0].Logs[0].Attrs, &attrs); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	got, ok := attrs["payload"].(string)
	if !ok {
		t.Fatalf("payload = %T, want string", attrs["payload"])
	}
	if !strings.Contains(got, "[truncated]") {
		t.Errorf("expected truncation marker, got %q", got)
	}
	if !strings.HasPrefix(got, strings.Repeat("x", 16)) {
		t.Errorf("expected 16-byte prefix, got %q", got[:min(len(got), 32)])
	}
	if attrs["short"] != "ok" {
		t.Errorf("short attr was modified: %v", attrs["short"])
	}
}

func TestHandler_CountersSharedAcrossWithChain(t *testing.T) {
	// A server that blocks until released keeps the drain goroutine stuck in
	// its first send (BatchSize 1 flushes immediately), so the tiny channel
	// buffer reliably overflows and Dropped > 0 is deterministic rather than
	// a producer-vs-consumer scheduling race.
	release := make(chan struct{})
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		select {
		case <-release:
		case <-r.Context().Done():
		}
		w.WriteHeader(http.StatusAccepted)
	}))
	defer srv.Close()

	h := mustNew(t, Config{
		Endpoint:    srv.URL,
		APIKey:      "k",
		Service:     "s",
		BatchSize:   1,
		FlushPeriod: time.Hour,
		BufferSize:  2,
		SendTimeout: time.Second,
	})
	defer h.Shutdown(context.Background()) //nolint:errcheck // Cleanup.
	defer close(release)                   // Unblock the in-flight send before Shutdown drains.

	root := slog.New(h)
	child := root.With("req", "1")
	grandchild := child.WithGroup("g1")

	for i := 0; i < 20; i++ {
		root.Info("root")
		child.Info("child")
		grandchild.Info("grand")
	}

	// All three loggers share the same state — the original handle's
	// counter must see drops caused by any of them.
	if h.Dropped() == 0 {
		t.Error("expected Dropped > 0 across chained loggers")
	}
}

func TestHandler_InsecureSkipVerify(t *testing.T) {
	var received atomic.Int64
	srv := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		received.Add(1)
		w.WriteHeader(http.StatusAccepted)
	}))
	defer srv.Close()

	t.Run("without skip fails TLS", func(t *testing.T) {
		h := mustNew(t, Config{
			Endpoint:    srv.URL,
			APIKey:      "k",
			Service:     "s",
			BatchSize:   1,
			FlushPeriod: 50 * time.Millisecond,
			BufferSize:  10,
		})
		slog.New(h).Info("should fail")
		time.Sleep(400 * time.Millisecond)
		if err := h.Shutdown(context.Background()); err != nil {
			t.Fatalf("shutdown: %v", err)
		}
		if h.SendFailed() == 0 {
			t.Error("expected TLS verification failure without InsecureSkipVerify")
		}
	})

	received.Store(0)
	t.Run("with skip succeeds", func(t *testing.T) {
		h := mustNew(t, Config{
			Endpoint:           srv.URL,
			APIKey:             "k",
			Service:            "s",
			InsecureSkipVerify: true,
			BatchSize:          1,
			FlushPeriod:        50 * time.Millisecond,
			BufferSize:         10,
		})
		slog.New(h).Info("should succeed")
		time.Sleep(400 * time.Millisecond)
		if err := h.Shutdown(context.Background()); err != nil {
			t.Fatalf("shutdown: %v", err)
		}
		if received.Load() == 0 {
			t.Error("server never received a request")
		}
		if h.SendFailed() != 0 {
			t.Errorf("SendFailed = %d, want 0 with InsecureSkipVerify", h.SendFailed())
		}
	})
}
