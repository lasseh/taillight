package logshipper

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"
)

func mustNew(t *testing.T, cfg Config) *Handler {
	t.Helper()
	h, err := New(cfg)
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	return h
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
	// Unreachable endpoint + tiny buffer guarantees Dropped > 0.
	h := mustNew(t, Config{
		Endpoint:    "http://127.0.0.1:1/unreachable",
		APIKey:      "k",
		Service:     "s",
		BatchSize:   100,
		FlushPeriod: time.Hour,
		BufferSize:  2,
	})
	defer h.Shutdown(context.Background()) //nolint:errcheck // Cleanup.

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
