package handler

import (
	"fmt"
	"log/slog"
	"net/http"
	"strconv"
	"time"

	"github.com/lasseh/taillight/internal/broker"
	"github.com/lasseh/taillight/internal/model"
)

const (
	sseBackfillLimit   = 100
	sseHeartbeatPeriod = 15 * time.Second
)

// SyslogSSEHandler handles the SSE streaming endpoint.
type SyslogSSEHandler struct {
	broker *broker.SyslogBroker
	store  SyslogStore
	logger *slog.Logger
}

// NewSyslogSSEHandler creates a new SyslogSSEHandler.
func NewSyslogSSEHandler(b *broker.SyslogBroker, s SyslogStore, l *slog.Logger) *SyslogSSEHandler {
	return &SyslogSSEHandler{broker: b, store: s, logger: l}
}

// Stream handles GET /api/v1/syslog/stream.
func (h *SyslogSSEHandler) Stream(w http.ResponseWriter, r *http.Request) {
	flusher, ok := w.(http.Flusher)
	if !ok {
		writeError(w, http.StatusInternalServerError, "streaming_unsupported", "streaming unsupported")
		return
	}

	filter, err := model.ParseSyslogFilter(r)
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
	sub := h.broker.Subscribe(filter)
	defer h.broker.Unsubscribe(sub)

	// Backfill: catch up from Last-Event-ID or send recent events.
	lastBackfilledID := h.backfill(w, r, filter, flusher)

	connectedAt := time.Now()
	logger := LoggerFromContext(r.Context())
	logger.Debug("syslog sse client connected",
		"remote_addr", r.RemoteAddr,
		"hostname", filter.Hostname,
		"programname", filter.Programname,
		"severity", filter.Severity,
		"facility", filter.Facility,
		"search", filter.Search,
	)
	defer func() {
		logger.Debug("syslog sse client disconnected",
			"remote_addr", r.RemoteAddr,
			"duration", time.Since(connectedAt).Round(time.Second),
		)
	}()

	heartbeat := time.NewTicker(sseHeartbeatPeriod)
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
			if err := writeSSE(w, msg.ID, msg.Data); err != nil {
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

// backfill sends recent events to a newly connected client and returns the
// highest event ID sent, so the caller can skip duplicates from the live channel.
func (h *SyslogSSEHandler) backfill(w http.ResponseWriter, r *http.Request, filter model.SyslogFilter, flusher http.Flusher) int64 {
	logger := LoggerFromContext(r.Context())
	if lastID := parseLastEventID(r); lastID > 0 {
		// Resume from where the client left off.
		events, err := h.store.ListSyslogsSince(r.Context(), filter, lastID, sseBackfillLimit)
		if err != nil {
			logger.Warn("syslog backfill since id failed", "last_event_id", lastID, "err", err)
			return lastID
		}
		// Already in chronological order (ASC).
		for i := range events {
			data, ok := mustJSON(events[i])
			if !ok {
				continue
			}
			if err := writeSSE(w, events[i].ID, data); err != nil {
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
	recent, _, err := h.store.ListSyslogs(r.Context(), filter, nil, sseBackfillLimit)
	if err != nil {
		logger.Warn("syslog backfill failed", "err", err)
		return 0
	}
	// Send in chronological order (oldest first).
	for i := len(recent) - 1; i >= 0; i-- {
		data, ok := mustJSON(recent[i])
		if !ok {
			continue
		}
		if err := writeSSE(w, recent[i].ID, data); err != nil {
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

func parseLastEventID(r *http.Request) int64 {
	v := r.Header.Get("Last-Event-ID")
	if v == "" {
		v = r.URL.Query().Get("lastEventId")
	}
	if v == "" {
		return 0
	}
	id, err := strconv.ParseInt(v, 10, 64)
	if err != nil {
		return 0
	}
	return id
}

func writeSSE(w http.ResponseWriter, id int64, data []byte) error {
	_, err := fmt.Fprintf(w, "id: %d\nevent: syslog\ndata: %s\n\n", id, data)
	return err
}
