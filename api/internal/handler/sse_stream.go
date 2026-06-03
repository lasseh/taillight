package handler

import (
	"context"
	"fmt"
	"log/slog"
	"net/http"
	"strconv"
	"time"

	"github.com/lasseh/taillight/internal/auth"
	"github.com/lasseh/taillight/internal/broker"
)

// parseLastEventID extracts the SSE resume position from the Last-Event-ID
// header or the lastEventId query parameter. Returns 0 when absent or invalid.
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

func writeSSEEvent(w http.ResponseWriter, id int64, event string, data []byte) error {
	_, err := fmt.Fprintf(w, "id: %d\nevent: %s\ndata: %s\n\n", id, event, data)
	return err
}

const (
	sseBackfillLimit   = 100
	sseHeartbeatPeriod = 15 * time.Second
	sseWriteTimeout    = 30 * time.Second
)

// sseSink is the write side of an SSE connection. The production adapter wraps
// an http.ResponseWriter; tests use an in-memory adapter so the streaming
// invariants (subscribe-before-backfill ordering, backfill-vs-live dedup) are
// testable without standing up an HTTP server.
type sseSink interface {
	writeEvent(id int64, event string, data []byte) error
	writeHeartbeat() error
	flush()
	setWriteDeadline(t time.Time) error
}

// httpSSESink adapts an http.ResponseWriter into an sseSink.
type httpSSESink struct {
	w  http.ResponseWriter
	rc *http.ResponseController
	fl http.Flusher
}

func newHTTPSSESink(w http.ResponseWriter, fl http.Flusher) *httpSSESink {
	return &httpSSESink{w: w, rc: http.NewResponseController(w), fl: fl}
}

func (s *httpSSESink) writeEvent(id int64, event string, data []byte) error {
	return writeSSEEvent(s.w, id, event, data)
}

func (s *httpSSESink) writeHeartbeat() error {
	_, err := fmt.Fprint(s.w, "event: heartbeat\ndata: \n\n")
	return err
}

func (s *httpSSESink) flush() { s.fl.Flush() }

func (s *httpSSESink) setWriteDeadline(t time.Time) error {
	return s.rc.SetWriteDeadline(t)
}

// sseStreamer fans a broker subscription plus a one-shot backfill query out to
// an sseSink. It owns the subscribe-before-backfill ordering invariant and the
// backfill-vs-live dedup that were previously copy-pasted across the
// srvlog/netlog/applog SSE handlers — fixing a bug here fixes it for every plane.
//
// E is the event type; F is the per-client filter (must implement Matches(E)).
type sseStreamer[E any, F interface{ Matches(E) bool }] struct {
	broker  *broker.Broker[E, F]
	label   string // SSE event name, e.g. "srvlog"
	eventID func(E) int64
	logger  *slog.Logger

	// since returns events with ID > sinceID in chronological (ASC) order.
	since func(ctx context.Context, f F, sinceID int64, limit int) ([]E, error)
	// recent returns the most recent matching events, newest-first (DESC).
	recent func(ctx context.Context, f F, limit int) ([]E, error)

	heartbeat    time.Duration // 0 → sseHeartbeatPeriod (overridable in tests)
	writeTimeout time.Duration // 0 → sseWriteTimeout (overridable in tests)
}

func (s sseStreamer[E, F]) heartbeatPeriod() time.Duration {
	if s.heartbeat > 0 {
		return s.heartbeat
	}
	return sseHeartbeatPeriod
}

func (s sseStreamer[E, F]) writeDeadline() time.Duration {
	if s.writeTimeout > 0 {
		return s.writeTimeout
	}
	return sseWriteTimeout
}

// sseClientKey derives the per-client throttle key for an SSE request. It
// prefers the authenticated user ID (so a logged-in user is capped regardless
// of source IP) and falls back to the source IP, which chi's RealIP middleware
// has already resolved into r.RemoteAddr.
func sseClientKey(r *http.Request) string {
	if u := auth.UserFromContext(r.Context()); u != nil && u.ID.Valid {
		return "user:" + formatUUID(u.ID.Bytes)
	}
	return "ip:" + stripPort(r.RemoteAddr)
}

// run subscribes, backfills, then streams live events until ctx is done or the
// sink errors. Subscribe happens BEFORE backfill so events arriving during the
// backfill query are buffered on the subscription rather than lost.
//
// A non-nil error is returned only when the subscription is refused before any
// bytes are written, so the caller can still emit an HTTP error response.
func (s sseStreamer[E, F]) run(ctx context.Context, sink sseSink, filter F, lastEventID int64, clientKey string) error {
	sub, err := s.broker.Subscribe(filter, clientKey)
	if err != nil {
		return err
	}
	defer s.broker.Unsubscribe(sub)

	// Flush the response headers on connect so the client's EventSource fires
	// onopen immediately, rather than waiting up to one heartbeat interval when
	// the stream is quiet and the backfill returns nothing. Subscribe already
	// succeeded, so committing the 200 here doesn't conflict with the
	// before-bytes error contract above (audit N4).
	sink.flush()

	lastBackfilledID := s.backfill(ctx, sink, filter, lastEventID)

	heartbeat := time.NewTicker(s.heartbeatPeriod())
	defer heartbeat.Stop()

	for {
		select {
		case msg, ok := <-sub.Chan():
			if !ok {
				return nil
			}
			// Skip events already sent during backfill.
			if msg.ID <= lastBackfilledID {
				continue
			}
			if err := sink.setWriteDeadline(time.Now().Add(s.writeDeadline())); err != nil {
				s.logger.Warn(s.label+" sse: failed to set write deadline", "err", err)
				return nil
			}
			if err := sink.writeEvent(msg.ID, s.label, msg.Data); err != nil {
				return nil
			}
			sink.flush()
		case <-heartbeat.C:
			if err := sink.setWriteDeadline(time.Now().Add(s.writeDeadline())); err != nil {
				s.logger.Warn(s.label+" sse: failed to set write deadline", "err", err)
				return nil
			}
			if err := sink.writeHeartbeat(); err != nil {
				return nil
			}
			sink.flush()
		case <-ctx.Done():
			return nil
		}
	}
}

// backfill sends recent events (or events since lastEventID) to a newly
// connected client and returns the highest event ID sent, so the live loop can
// skip duplicates.
func (s sseStreamer[E, F]) backfill(ctx context.Context, sink sseSink, filter F, lastEventID int64) int64 {
	if lastEventID > 0 {
		// Resume from where the client left off.
		events, err := s.since(ctx, filter, lastEventID, sseBackfillLimit)
		if err != nil {
			if ctx.Err() != nil {
				s.logger.Debug(s.label+" backfill canceled", "last_event_id", lastEventID, "err", err)
			} else {
				s.logger.Warn(s.label+" backfill since id failed", "last_event_id", lastEventID, "err", err)
			}
			return lastEventID
		}
		// since() is already in chronological order (ASC).
		for i := range events {
			data, ok := mustJSON(events[i])
			if !ok {
				continue
			}
			if err := sink.writeEvent(s.eventID(events[i]), s.label, data); err != nil {
				return lastEventID
			}
		}
		if len(events) > 0 {
			sink.flush()
			return s.eventID(events[len(events)-1])
		}
		return lastEventID
	}

	// Default: send recent matching events.
	recent, err := s.recent(ctx, filter, sseBackfillLimit)
	if err != nil {
		if ctx.Err() != nil {
			s.logger.Debug(s.label+" backfill canceled", "err", err)
		} else {
			s.logger.Warn(s.label+" backfill failed", "err", err)
		}
		return 0
	}
	// recent() is newest-first (DESC); emit oldest-first.
	for i := len(recent) - 1; i >= 0; i-- {
		data, ok := mustJSON(recent[i])
		if !ok {
			continue
		}
		if err := sink.writeEvent(s.eventID(recent[i]), s.label, data); err != nil {
			return 0
		}
	}
	if len(recent) > 0 {
		sink.flush()
		// recent is ordered DESC, so [0] has the highest ID.
		return s.eventID(recent[0])
	}
	return 0
}
