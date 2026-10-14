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

func TestLogshipperContract(t *testing.T) {
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
