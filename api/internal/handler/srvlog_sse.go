package handler

import (
	"context"
	"log/slog"
	"net/http"
	"time"

	"github.com/lasseh/taillight/internal/broker"
	"github.com/lasseh/taillight/internal/model"
)

// SrvlogSSEHandler handles the SSE streaming endpoint.
type SrvlogSSEHandler struct {
	broker *broker.SrvlogBroker
	store  SrvlogStore
	logger *slog.Logger
}

// NewSrvlogSSEHandler creates a new SrvlogSSEHandler.
func NewSrvlogSSEHandler(b *broker.SrvlogBroker, s SrvlogStore, l *slog.Logger) *SrvlogSSEHandler {
	return &SrvlogSSEHandler{broker: b, store: s, logger: l}
}

// Stream handles GET /api/v1/srvlog/stream.
func (h *SrvlogSSEHandler) Stream(w http.ResponseWriter, r *http.Request) {
	flusher, ok := w.(http.Flusher)
	if !ok {
		writeError(w, http.StatusInternalServerError, "streaming_unsupported", "streaming unsupported")
		return
	}

	filter, err := model.ParseSrvlogFilter(r)
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
	logger.Debug("srvlog sse client connected",
		"remote_addr", r.RemoteAddr,
		"hostname", filter.Hostname,
		"programname", filter.Programname,
		"severity", filter.Severity,
		"facility", filter.Facility,
		"search", filter.Search,
	)
	defer func() {
		logger.Debug("srvlog sse client disconnected",
			"remote_addr", r.RemoteAddr,
			"duration", time.Since(connectedAt).Round(time.Second),
		)
	}()

	streamer := sseStreamer[model.SrvlogEvent, model.SrvlogFilter]{
		broker:  h.broker,
		label:   "srvlog",
		eventID: func(e model.SrvlogEvent) int64 { return e.ID },
		logger:  logger,
		since:   h.store.ListSrvlogsSince,
		recent: func(ctx context.Context, f model.SrvlogFilter, limit int) ([]model.SrvlogEvent, error) {
			events, _, err := h.store.ListSrvlogs(ctx, f, nil, limit)
			return events, err
		},
	}
	if err := streamer.run(r.Context(), sink, filter, parseLastEventID(r)); err != nil {
		writeError(w, http.StatusServiceUnavailable, "too_many_connections", "too many active connections, try again later")
		return
	}
}
