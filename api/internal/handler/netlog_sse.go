package handler

import (
	"context"
	"log/slog"
	"net/http"
	"time"

	"github.com/lasseh/taillight/internal/broker"
	"github.com/lasseh/taillight/internal/model"
)

// NetlogSSEHandler handles the SSE streaming endpoint for netlog events.
type NetlogSSEHandler struct {
	broker *broker.NetlogBroker
	store  NetlogStore
	logger *slog.Logger
}

// NewNetlogSSEHandler creates a new NetlogSSEHandler.
func NewNetlogSSEHandler(b *broker.NetlogBroker, s NetlogStore, l *slog.Logger) *NetlogSSEHandler {
	return &NetlogSSEHandler{broker: b, store: s, logger: l}
}

// Stream handles GET /api/v1/netlog/stream.
func (h *NetlogSSEHandler) Stream(w http.ResponseWriter, r *http.Request) {
	flusher, ok := w.(http.Flusher)
	if !ok {
		writeError(w, http.StatusInternalServerError, "streaming_unsupported", "streaming unsupported")
		return
	}

	filter, err := model.ParseNetlogFilter(r)
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
	logger.Debug("netlog sse client connected",
		"remote_addr", r.RemoteAddr,
		"hostname", filter.Hostname,
		"programname", filter.Programname,
		"severity", filter.Severity,
		"facility", filter.Facility,
		"search", filter.Search,
	)
	defer func() {
		logger.Debug("netlog sse client disconnected",
			"remote_addr", r.RemoteAddr,
			"duration", time.Since(connectedAt).Round(time.Second),
		)
	}()

	streamer := sseStreamer[model.NetlogEvent, model.NetlogFilter]{
		broker:  h.broker,
		label:   "netlog",
		eventID: func(e model.NetlogEvent) int64 { return e.ID },
		logger:  logger,
		since:   h.store.ListNetlogsSince,
		recent: func(ctx context.Context, f model.NetlogFilter, limit int) ([]model.NetlogEvent, error) {
			events, _, err := h.store.ListNetlogs(ctx, f, nil, limit)
			return events, err
		},
	}
	if err := streamer.run(r.Context(), sink, filter, parseLastEventID(r)); err != nil {
		writeError(w, http.StatusServiceUnavailable, "too_many_connections", "too many active connections, try again later")
		return
	}
}
