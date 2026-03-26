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
	netlogSSEBackfillLimit   = 100
	netlogSSEHeartbeatPeriod = 15 * time.Second
	netlogSSEWriteTimeout    = 30 * time.Second
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

	// Extend write deadline for long-lived SSE connection.
	rc := http.NewResponseController(w)
	if err := rc.SetWriteDeadline(time.Now().Add(netlogSSEWriteTimeout)); err != nil {
		writeError(w, http.StatusInternalServerError, "streaming_unsupported", "streaming unsupported")
		return
	}

	// Subscribe BEFORE backfill to avoid a race: events arriving between
	// the backfill query and the subscribe call would otherwise be lost.
	sub, err := h.broker.Subscribe(filter)
	if err != nil {
		writeError(w, http.StatusServiceUnavailable, "too_many_connections", "too many active connections, try again later")
		return
	}
	defer h.broker.Unsubscribe(sub)

	// Backfill: catch up from Last-Event-ID or send recent events.
	lastBackfilledID := h.backfill(w, r, filter, flusher)

	connectedAt := time.Now()
	logger := LoggerFromContext(r.Context())
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

	heartbeat := time.NewTicker(netlogSSEHeartbeatPeriod)
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
			if err := rc.SetWriteDeadline(time.Now().Add(netlogSSEWriteTimeout)); err != nil {
				logger.Warn("netlog sse: failed to set write deadline", "err", err)
				return
			}
			if err := writeSSEEvent(w, msg.ID, "netlog", msg.Data); err != nil {
				return
			}
			flusher.Flush()
		case <-heartbeat.C:
			if err := rc.SetWriteDeadline(time.Now().Add(netlogSSEWriteTimeout)); err != nil {
				logger.Warn("netlog sse: failed to set write deadline", "err", err)
				return
			}
			if _, err := fmt.Fprint(w, "event: heartbeat\ndata: \n\n"); err != nil {
				return
			}
			flusher.Flush()
		case <-r.Context().Done():
			return
		}
	}
}

// backfill sends recent events to a newly connected client and returns the
// highest event ID sent, so the caller can skip duplicates from the live channel.
func (h *NetlogSSEHandler) backfill(w http.ResponseWriter, r *http.Request, filter model.NetlogFilter, flusher http.Flusher) int64 {
	logger := LoggerFromContext(r.Context())
	if lastID := parseLastEventID(r); lastID > 0 {
		// Resume from where the client left off.
		events, err := h.store.ListNetlogsSince(r.Context(), filter, lastID, netlogSSEBackfillLimit)
		if err != nil {
			// Client disconnect during backfill is expected; don't warn.
			if r.Context().Err() != nil {
				logger.Debug("netlog backfill canceled", "last_event_id", lastID, "err", err)
			} else {
				logger.Warn("netlog backfill since id failed", "last_event_id", lastID, "err", err)
			}
			return lastID
		}
		// Already in chronological order (ASC).
		for i := range events {
			data, ok := mustJSON(events[i])
			if !ok {
				continue
			}
			if err := writeSSEEvent(w, events[i].ID, "netlog", data); err != nil {
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
	recent, _, err := h.store.ListNetlogs(r.Context(), filter, nil, netlogSSEBackfillLimit)
	if err != nil {
		if r.Context().Err() != nil {
			logger.Debug("netlog backfill canceled", "err", err)
		} else {
			logger.Warn("netlog backfill failed", "err", err, "hostname", filter.Hostname, "programname", filter.Programname, "severity", filter.Severity)
		}
		return 0
	}
	// Send in chronological order (oldest first).
	for i := len(recent) - 1; i >= 0; i-- {
		data, ok := mustJSON(recent[i])
		if !ok {
			continue
		}
		if err := writeSSEEvent(w, recent[i].ID, "netlog", data); err != nil {
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
