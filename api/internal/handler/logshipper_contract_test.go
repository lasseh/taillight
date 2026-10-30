package handler

// Contract test for pkg/logshipper (ADR 0004): the real shipper posts to the
// real ingest handler, so an entry the shipper emits that breaks a server rule
// fails here instead of drifting from limits copied by hand into the shipper.

import (
	"context"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/lasseh/taillight/internal/broker"
	"github.com/lasseh/taillight/internal/model"
	"github.com/lasseh/taillight/pkg/logshipper"
)

// recordingAppLogStore records every inserted event. The shipper posts from
// its own goroutine, so access is locked.
type recordingAppLogStore struct {
	mockAppLogStore
	mu     sync.Mutex
	events []model.AppLogEvent
}

func (s *recordingAppLogStore) InsertLogBatch(ctx context.Context, events []model.AppLogEvent) ([]model.AppLogEvent, error) {
	s.mu.Lock()
	s.events = append(s.events, events...)
	s.mu.Unlock()
	return s.mockAppLogStore.InsertLogBatch(ctx, events)
}

func (s *recordingAppLogStore) stored() []model.AppLogEvent {
	s.mu.Lock()
	defer s.mu.Unlock()
	return append([]model.AppLogEvent(nil), s.events...)
}

// holdFirstTransport holds the first request until release is closed, so
// entries queue up in the shipper behind it.
type holdFirstTransport struct {
	once    sync.Once
	started chan struct{}
	release chan struct{}
}

func (t *holdFirstTransport) RoundTrip(r *http.Request) (*http.Response, error) {
	first := false
	t.once.Do(func() { first = true })
	if first {
		close(t.started)
		<-t.release
	}
	return http.DefaultTransport.RoundTrip(r)
}

func TestLogshipperContract(t *testing.T) {
	held := &holdFirstTransport{started: make(chan struct{}), release: make(chan struct{})}

	tests := []struct {
		name       string
		cfg        func(*logshipper.Config)
		log        func(t *testing.T, h *logshipper.Handler)
		wantStored int
	}{
		{
			name:       "normal entry",
			log:        func(_ *testing.T, h *logshipper.Handler) { slog.New(h).Info("hello", "k", "v") },
			wantStored: 1,
		},
		{
			name:       "empty message",
			log:        func(_ *testing.T, h *logshipper.Handler) { slog.New(h).Info("") },
			wantStored: 1,
		},
		{
			// slog.Logger always stamps a time, so call Handle directly.
			name: "zero record time",
			log: func(t *testing.T, h *logshipper.Handler) {
				if err := h.Handle(context.Background(), slog.NewRecord(time.Time{}, slog.LevelInfo, "zero time", 0)); err != nil {
					t.Fatalf("Handle: %v", err)
				}
			},
			wantStored: 1,
		},
		{
			name:       "attr named source",
			log:        func(_ *testing.T, h *logshipper.Handler) { slog.New(h).Info("m", "source", "user value") },
			wantStored: 1,
		},
		{
			// 40 KB of mixed invalid bytes that JSON encoding would grow to 80 KB.
			name:       "invalid UTF-8 message",
			log:        func(_ *testing.T, h *logshipper.Handler) { slog.New(h).Info(strings.Repeat("\xffa", 20000)) },
			wantStored: 1,
		},
		{
			// The attrs ship as a stub; the record still arrives.
			name: "attrs over 64 KB",
			log: func(_ *testing.T, h *logshipper.Handler) {
				slog.New(h).Info("big", "body", strings.Repeat("x", 100*1024))
			},
			wantStored: 1,
		},
		{
			// The first full batch is held, so 1,400 entries are still
			// buffered when Shutdown starts. The drain must send them in
			// requests the server accepts (at most 1,000 entries each).
			name: "shutdown with over 1,000 buffered entries",
			cfg: func(c *logshipper.Config) {
				c.Client = &http.Client{Transport: held}
				c.BatchSize = 1000
				c.BufferSize = 2000
			},
			log: func(_ *testing.T, h *logshipper.Handler) {
				logger := slog.New(h)
				for range 1000 {
					logger.Info("m")
				}
				<-held.started
				for range 1400 {
					logger.Info("m")
				}
				// Release once the Shutdown that follows has begun.
				time.AfterFunc(50*time.Millisecond, func() { close(held.release) })
			},
			wantStored: 2400,
		},
		{
			// About 6 MB in one BatchSize worth of entries: over the 5 MB body cap.
			name: "large entries over the body cap",
			log: func(_ *testing.T, h *logshipper.Handler) {
				logger := slog.New(h)
				msg := strings.Repeat("x", 60*1024)
				for range 100 {
					logger.Info(msg)
				}
			},
			wantStored: 100,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			store := &recordingAppLogStore{}
			ingest := NewAppLogIngestHandler(store, broker.NewAppLogBroker(slog.Default()), slog.Default(), nil)
			srv := httptest.NewServer(http.HandlerFunc(ingest.Ingest))
			defer srv.Close()

			cfg := logshipper.Config{
				Endpoint:    srv.URL,
				Service:     "contract",
				Host:        "contract-host",
				AddSource:   true,
				FlushPeriod: time.Hour, // Only Shutdown flushes.
			}
			if tt.cfg != nil {
				tt.cfg(&cfg)
			}
			h, err := logshipper.New(cfg)
			if err != nil {
				t.Fatalf("logshipper.New: %v", err)
			}

			tt.log(t, h)

			ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
			defer cancel()
			if err := h.Shutdown(ctx); err != nil {
				t.Fatalf("Shutdown: %v", err)
			}

			if got := len(store.stored()); got != tt.wantStored {
				t.Errorf("store received %d entries, want %d", got, tt.wantStored)
			}
			if h.Dropped() != 0 || h.SendFailed() != 0 {
				t.Errorf("Dropped = %d, SendFailed = %d, want 0 and 0", h.Dropped(), h.SendFailed())
			}
		})
	}
}
