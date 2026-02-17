package logshipper

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"sync"
	"testing"
	"time"
)

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

	h := New(Config{
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

	h := New(Config{
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

	h := New(Config{
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

	h := New(Config{
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

	h := New(Config{
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
	h := New(Config{
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

	shipper := New(Config{
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
