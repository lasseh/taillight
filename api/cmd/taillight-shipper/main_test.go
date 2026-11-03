package main

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"testing/iotest"
	"time"

	"github.com/lasseh/taillight/pkg/logshipper"
)

func TestNewHandlers_ShutsDownEarlierHandlersOnError(t *testing.T) {
	var created []*logshipper.Handler
	orig := newShipper
	newShipper = func(cfg logshipper.Config) (*logshipper.Handler, error) {
		h, err := orig(cfg)
		if err == nil {
			created = append(created, h)
		}
		return h, err
	}
	t.Cleanup(func() { newShipper = orig })

	base := logshipper.Config{Endpoint: "http://127.0.0.1:1/api/v1/applog/ingest", FlushPeriod: time.Hour}
	specs := []handlerSpec{
		{service: "first", host: "host-a"},
		{service: "second", host: strings.Repeat("h", 257)}, // Over the ingest API's 256-byte cap.
	}

	handlers, err := newHandlers(base, specs, slog.New(slog.DiscardHandler))
	if err == nil {
		t.Fatal("newHandlers succeeded, want an error for the over-long host")
	}
	if !strings.Contains(err.Error(), `service "second"`) || !strings.Contains(err.Error(), "host is 257 bytes") {
		t.Errorf("error = %q, want the failing service and the library's cause", err)
	}
	if handlers != nil {
		t.Errorf("handlers = %v, want nil on error", handlers)
	}
	if len(created) != 1 {
		t.Fatalf("created %d handlers before the failure, want 1", len(created))
	}

	// A handler that has been shut down drops and counts new records.
	if err := created[0].Handle(context.Background(), slog.NewRecord(time.Now(), slog.LevelInfo, "after", 0)); err != nil {
		t.Fatalf("Handle: %v", err)
	}
	if got := created[0].Dropped(); got != 1 {
		t.Errorf("first handler Dropped = %d, want 1: it was left running without a shutdown", got)
	}
}

func TestShutdown_ReportsEntriesTheServerRejected(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusBadRequest)
	}))
	t.Cleanup(srv.Close)

	h, err := logshipper.New(logshipper.Config{Endpoint: srv.URL, Service: "s", Host: "h", FlushPeriod: time.Hour})
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	slog.New(h).Info("m")

	var buf bytes.Buffer
	shutdown([]*logshipper.Handler{h}, slog.New(slog.NewTextHandler(&buf, nil)))

	if got := buf.String(); !strings.Contains(got, "failed to ship log entries") || !strings.Contains(got, "count=1") {
		t.Errorf("shutdown logged %q, want the 1 entry that failed", got)
	}
}

// tls_skip_verify must keep the library's client: its redirect rule stops the
// POST becoming a GET and the API key reaching the redirect target.
func TestBaseConfig_SkipVerifyDoesNotFollowRedirects(t *testing.T) {
	var posts, redirected atomic.Int64
	mux := http.NewServeMux()
	mux.HandleFunc("/ingest", func(w http.ResponseWriter, r *http.Request) {
		posts.Add(1)
		http.Redirect(w, r, "/other", http.StatusFound)
	})
	mux.HandleFunc("/other", func(http.ResponseWriter, *http.Request) {
		redirected.Add(1)
	})
	srv := httptest.NewTLSServer(mux)
	t.Cleanup(srv.Close)

	base := baseConfig(config{Endpoint: srv.URL + "/ingest", APIKey: "k", TLSSkipVerify: true}, time.Hour)
	base.Service, base.Host = "s", "h"
	h, err := logshipper.New(base)
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	slog.New(h).Info("m")
	shutdown([]*logshipper.Handler{h}, slog.New(slog.DiscardHandler))

	if posts.Load() == 0 {
		t.Error("no request reached the server: the self-signed certificate was not accepted")
	}
	if got := redirected.Load(); got != 0 {
		t.Errorf("redirect target saw %d requests, want 0", got)
	}
}

// recordingHandler keeps the message of every record it handles.
type recordingHandler struct {
	mu   sync.Mutex
	msgs []string
}

func (h *recordingHandler) Enabled(context.Context, slog.Level) bool { return true }

func (h *recordingHandler) Handle(_ context.Context, r slog.Record) error {
	h.mu.Lock()
	defer h.mu.Unlock()
	h.msgs = append(h.msgs, r.Message)
	return nil
}

func (h *recordingHandler) WithAttrs([]slog.Attr) slog.Handler { return h }
func (h *recordingHandler) WithGroup(string) slog.Handler      { return h }

func (h *recordingHandler) messages() []string {
	h.mu.Lock()
	defer h.mu.Unlock()
	return slices.Clone(h.msgs)
}

func TestReadStdin_OversizedLineIsTruncatedAndShippingContinues(t *testing.T) {
	in := strings.Repeat("x", 2<<20) + "\nnext\nlast without newline"
	var h recordingHandler
	var logs bytes.Buffer

	err := readStdin(context.Background(), strings.NewReader(in), &h, false, slog.New(slog.NewTextHandler(&logs, nil)))
	if err != nil {
		t.Fatalf("readStdin: %v", err)
	}

	got := h.messages()
	if len(got) != 3 {
		t.Fatalf("shipped %d lines, want 3", len(got))
	}
	if got[0] != strings.Repeat("x", maxLineBytes) {
		t.Errorf("first line shipped as %d bytes, want the first %d", len(got[0]), maxLineBytes)
	}
	if got[1] != "next" || got[2] != "last without newline" {
		t.Errorf("later lines = %q, want next and the last line", got[1:])
	}
	if n := strings.Count(logs.String(), "line over max_bytes truncated"); n != 1 || !strings.Contains(logs.String(), "truncated_lines=1") {
		t.Errorf("logged %d truncation warnings, want 1 counting 1 line: %s", n, logs.String())
	}
}

func TestReadStdin_ReadError(t *testing.T) {
	errRead := errors.New("device error")
	var h recordingHandler

	err := readStdin(context.Background(), io.MultiReader(strings.NewReader("first\n"), iotest.ErrReader(errRead)), &h, false, slog.New(slog.DiscardHandler))
	if !errors.Is(err, errRead) {
		t.Fatalf("readStdin = %v, want the read error", err)
	}
	if got := h.messages(); !slices.Equal(got, []string{"first"}) {
		t.Errorf("shipped %q, want the line before the error", got)
	}
}

// At shutdown stdin is closed under the reader; that read error is not a
// failure.
func TestReadStdin_ClosedAtShutdownIsNotAnError(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	var h recordingHandler

	if err := readStdin(ctx, iotest.ErrReader(os.ErrClosed), &h, false, slog.New(slog.DiscardHandler)); err != nil {
		t.Fatalf("readStdin = %v, want nil after cancellation", err)
	}
}

// A stdin read that fails with anything but EOF ends the command with an
// error, file tailers included, instead of leaving stdin unread.
func TestRun_StdinReadErrorEndsTheCommand(t *testing.T) {
	var posts atomic.Int64
	sink := httptest.NewServer(http.HandlerFunc(func(http.ResponseWriter, *http.Request) {
		posts.Add(1)
	}))
	t.Cleanup(sink.Close)

	dir := t.TempDir()
	tailed := filepath.Join(dir, "app.log")
	if err := os.WriteFile(tailed, nil, 0o600); err != nil {
		t.Fatal(err)
	}
	cfg := fmt.Sprintf("endpoint: %s\nservice: s\nhost: h\nflush_period: 10ms\nfiles:\n  - path: %s\n", sink.URL, tailed)
	cfgPath := filepath.Join(dir, "config.yml")
	if err := os.WriteFile(cfgPath, []byte(cfg), 0o600); err != nil {
		t.Fatal(err)
	}

	r, w, err := os.Pipe()
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		r.Close() //nolint:errcheck // test teardown
		w.Close() //nolint:errcheck // test teardown
	})
	origStdin, origConfig := os.Stdin, configPath
	os.Stdin, configPath = r, cfgPath
	t.Cleanup(func() { os.Stdin, configPath = origStdin, origConfig })

	done := make(chan error, 1)
	go func() { done <- run(nil, nil) }()

	// Once a line has shipped, the command is reading stdin.
	if _, err := w.WriteString("first\n"); err != nil {
		t.Fatal(err)
	}
	for deadline := time.Now().Add(5 * time.Second); posts.Load() == 0; time.Sleep(10 * time.Millisecond) {
		if time.Now().After(deadline) {
			t.Fatal("the first stdin line never shipped")
		}
	}
	r.Close() //nolint:errcheck // makes the next read fail with an error other than EOF

	select {
	case err := <-done:
		if err == nil || !strings.Contains(err.Error(), "reading stdin") {
			t.Fatalf("run = %v, want the stdin read error", err)
		}
	case <-time.After(10 * time.Second):
		t.Fatal("run still running after stdin failed")
	}
}

func TestNewHandlers_EmptyHostIsResolvedByTheLibrary(t *testing.T) {
	base := logshipper.Config{Endpoint: "http://127.0.0.1:1/api/v1/applog/ingest", FlushPeriod: time.Hour}
	handlers, err := newHandlers(base, []handlerSpec{{service: "svc"}}, slog.New(slog.DiscardHandler))
	if err != nil {
		t.Fatalf("newHandlers: %v", err)
	}
	shutdown(handlers, slog.New(slog.DiscardHandler))
	if len(handlers) != 1 {
		t.Fatalf("got %d handlers, want 1", len(handlers))
	}
}
