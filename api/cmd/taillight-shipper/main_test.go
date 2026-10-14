package main

import (
	"context"
	"log/slog"
	"strings"
	"testing"
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
