package handler

import (
	"context"
	"log/slog"
	"net/http"
	"time"

	"github.com/go-chi/chi/v5/middleware"

	"github.com/lasseh/taillight/internal/broker"
	"github.com/lasseh/taillight/internal/model"
)

// AppLogSSEHandler handles the SSE streaming endpoint for log events.
type AppLogSSEHandler struct {
	broker *broker.AppLogBroker
	store  AppLogStore
	logger *slog.Logger
}

// NewAppLogSSEHandler creates a new AppLogSSEHandler.
func NewAppLogSSEHandler(b *broker.AppLogBroker, s AppLogStore, l *slog.Logger) *AppLogSSEHandler {
	return &AppLogSSEHandler{broker: b, store: s, logger: l}
}

// Stream handles GET /api/v1/applog/stream.
func (h *AppLogSSEHandler) Stream(w http.ResponseWriter, r *http.Request) {
	flusher, ok := w.(http.Flusher)
	if !ok {
		writeError(w, http.StatusInternalServerError, "streaming_unsupported", "streaming unsupported")
		return
	}

	filter, err := model.ParseAppLogFilter(r)
	if err != nil {
		writeError(w, http.StatusBadRequest, "invalid_filter", err.Error())
		return
	}

	w.Header().Set("Content-Type", "text/event-stream")
	w.Header().Set("Cache-Control", "no-cache")
	w.Header().Set("Connection", "keep-alive")
	w.Header().Set("X-Accel-Buffering", "no")

	sink := newHTTPSSESink(w, flusher)
	if err := sink.setWriteDeadline(time.Now().Add(sseWriteTimeout)); err != nil {
		writeError(w, http.StatusInternalServerError, "streaming_unsupported", "streaming unsupported")
		return
	}

	logger := LoggerFromContext(r.Context())
	connectedAt := time.Now()
	logger.Debug("applog sse client connected",
		"remote_addr", middleware.GetClientIP(r.Context()),
		"service", filter.Service,
		"component", filter.Component,
		"level", filter.Level,
		"search", filter.Search,
	)
	defer func() {
		logger.Debug("applog sse client disconnected",
			"remote_addr", middleware.GetClientIP(r.Context()),
			"duration", time.Since(connectedAt).Round(time.Second),
		)
	}()

	// SSE backfill goes through the same attrs-preview transform as the list
	// endpoint, so the browser buffer stays bounded even on resume / fresh load.
	streamer := sseStreamer[model.AppLogEvent, model.AppLogFilter]{
		broker:  h.broker,
		label:   "applog",
		eventID: func(e model.AppLogEvent) int64 { return e.ID },
		logger:  logger,
		since: func(ctx context.Context, f model.AppLogFilter, sinceID int64, limit int) ([]model.AppLogEvent, error) {
			events, err := h.store.ListAppLogsSince(ctx, f, sinceID, limit)
			return previewAppLogAttrs(events), err
		},
		recent: func(ctx context.Context, f model.AppLogFilter, limit int) ([]model.AppLogEvent, error) {
			events, _, err := h.store.ListAppLogs(ctx, f, nil, limit)
			return previewAppLogAttrs(events), err
		},
	}
	if err := streamer.run(r.Context(), sink, filter, parseLastEventID(r), sseClientKey(r)); err != nil {
		writeError(w, http.StatusServiceUnavailable, "too_many_connections", "too many active connections, try again later")
		return
	}
}
