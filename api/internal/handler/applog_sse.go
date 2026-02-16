package handler

import (
	"fmt"
	"log/slog"
	"net/http"
	"time"

	"github.com/lasseh/taillight/internal/broker"
	"github.com/lasseh/taillight/internal/model"
)

const (
	applogSSEBackfillLimit   = 100
	applogSSEHeartbeatPeriod = 15 * time.Second
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

	// Subscribe BEFORE backfill to avoid a race: events arriving between
	// the backfill query and the subscribe call would otherwise be lost.
	sub, err := h.broker.Subscribe(filter)
	if err != nil {
		writeError(w, http.StatusServiceUnavailable, "too_many_connections", err.Error())
		return
	}
	defer h.broker.Unsubscribe(sub)

	// Backfill: catch up from Last-Event-ID or send recent events.
	lastBackfilledID := h.backfill(w, r, filter, flusher)

	connectedAt := time.Now()
	logger := LoggerFromContext(r.Context())
	logger.Debug("applog sse client connected",
		"remote_addr", r.RemoteAddr,
		"service", filter.Service,
		"component", filter.Component,
		"level", filter.Level,
		"search", filter.Search,
	)
	defer func() {
		logger.Debug("applog sse client disconnected",
			"remote_addr", r.RemoteAddr,
			"duration", time.Since(connectedAt).Round(time.Second),
		)
	}()

	heartbeat := time.NewTicker(applogSSEHeartbeatPeriod)
	defer heartbeat.Stop()

	for {
		select {
		case msg, ok := <-sub.Chan():
			if !ok {
				return
			}
			// Skip events already sent during backfill.
			if msg.ID <= lastBackfilledID {
				continue
			}
			if err := writeSSEEvent(w, msg.ID, "applog", msg.Data); err != nil {
				return
			}
			flusher.Flush()
		case <-heartbeat.C:
			if _, err := fmt.Fprint(w, "event: heartbeat\ndata: \n\n"); err != nil {
				return
			}
			flusher.Flush()
		case <-r.Context().Done():
			return
		}
	}
}

// backfill sends recent log events to a newly connected client and returns the
// highest event ID sent, so the caller can skip duplicates from the live channel.
func (h *AppLogSSEHandler) backfill(w http.ResponseWriter, r *http.Request, filter model.AppLogFilter, flusher http.Flusher) int64 {
	logger := LoggerFromContext(r.Context())
	if lastID := parseLastEventID(r); lastID > 0 {
		// Resume from where the client left off.
		events, err := h.store.ListAppLogsSince(r.Context(), filter, lastID, applogSSEBackfillLimit)
		if err != nil {
			logger.Warn("applog backfill since id failed", "last_event_id", lastID, "err", err)
			return lastID
		}
		// Already in chronological order (ASC).
		for i := range events {
			data, ok := mustJSON(events[i])
			if !ok {
				continue
			}
			if err := writeSSEEvent(w, events[i].ID, "applog", data); err != nil {
				return lastID
			}
		}
		if len(events) > 0 {
			flusher.Flush()
			return events[len(events)-1].ID
		}
		return lastID
	}

	// Default: send recent matching events.
	recent, _, err := h.store.ListAppLogs(r.Context(), filter, nil, applogSSEBackfillLimit)
	if err != nil {
		logger.Warn("applog backfill failed", "err", err)
		return 0
	}
	// Send in chronological order (oldest first).
	for i := len(recent) - 1; i >= 0; i-- {
		data, ok := mustJSON(recent[i])
		if !ok {
			continue
		}
		if err := writeSSEEvent(w, recent[i].ID, "applog", data); err != nil {
			return 0
		}
	}
	if len(recent) > 0 {
		flusher.Flush()
		// recent is ordered DESC, so [0] has the highest ID.
		return recent[0].ID
	}
	return 0
}
