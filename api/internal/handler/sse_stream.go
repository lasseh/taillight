package handler

import (
	"context"
	"fmt"
	"log/slog"
	"math/rand/v2"
	"net/http"
	"strconv"
	"time"

	"github.com/go-chi/chi/v5/middleware"

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
	// sseMaxLifetime ends every stream after this long, give or take
	// sseLifetimeJitter. Auth runs only when a stream connects, so the
	// client's reconnect is what re-checks its credential: a revoked key,
	// ended session or deactivated user stops receiving events within
	// sseMaxLifetime + sseLifetimeJitter.
	sseMaxLifetime = 5 * time.Minute
	// sseLifetimeJitter spreads the forced reconnects, so the streams one
	// client opens together, and every client's streams after an API restart,
	// do not end and resume in step.
	sseLifetimeJitter = sseMaxLifetime * 15 / 100
	// sseBackfillTimeout bounds the connect-time backfill query, including
	// the wait for a database connection, so a busy read pool delays a new
	// stream by at most this long before live delivery starts.
	sseBackfillTimeout = 5 * time.Second
)

// sseSink is the write side of an SSE connection. The production adapter wraps
// an http.ResponseWriter; tests use an in-memory adapter so the streaming
// invariants (subscribe-before-backfill ordering, backfill-vs-live dedup) are
// testable without standing up an HTTP server.
type sseSink interface {
	writeEvent(id int64, event string, data []byte) error
	writeHeartbeat() error
	// writeResync tells a resuming client that the backfill did not reach
	// the live stream, so it must reload history to close the gap.
	writeResync() error
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

// writeResync is framed like the heartbeat: no id, so it does not move the
// client's resume position, and empty data, so clients that decode every
// non-heartbeat payload as a log event never see it.
func (s *httpSSESink) writeResync() error {
	_, err := fmt.Fprint(s.w, "event: resync\ndata: \n\n")
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

	heartbeat       time.Duration // 0 → sseHeartbeatPeriod (overridable in tests)
	writeTimeout    time.Duration // 0 → sseWriteTimeout (overridable in tests)
	lifetime        time.Duration // 0 → sseMaxLifetime ± sseLifetimeJitter (overridable in tests)
	backfillTimeout time.Duration // 0 → sseBackfillTimeout (overridable in tests)
}

func (s sseStreamer[E, F]) heartbeatPeriod() time.Duration {
	if s.heartbeat > 0 {
		return s.heartbeat
	}
	return sseHeartbeatPeriod
}

func (s sseStreamer[E, F]) maxLifetime() time.Duration {
	if s.lifetime > 0 {
		return s.lifetime
	}
	return sseMaxLifetime - sseLifetimeJitter + rand.N(2*sseLifetimeJitter+1)
}

func (s sseStreamer[E, F]) backfillDeadline() time.Duration {
	if s.backfillTimeout > 0 {
		return s.backfillTimeout
	}
	return sseBackfillTimeout
}

func (s sseStreamer[E, F]) writeDeadline() time.Duration {
	if s.writeTimeout > 0 {
		return s.writeTimeout
	}
	return sseWriteTimeout
}

// sseClientKey derives the per-client throttle key for an SSE request. It
// prefers the authenticated user ID (so a logged-in user is capped regardless
// of source IP) and falls back to the client IP resolved by clientIPMiddleware.
func sseClientKey(r *http.Request) string {
	if u := auth.UserFromContext(r.Context()); u != nil && u.ID.Valid {
		return "user:" + formatUUID(u.ID.Bytes)
	}
	return "ip:" + middleware.GetClientIP(r.Context())
}

// run subscribes, backfills, then streams live events until ctx is done, the
// sink errors, or the stream reaches its maximum lifetime. Subscribe happens
// BEFORE backfill so events arriving during the backfill query are buffered on
// the subscription rather than lost.
//
// A non-nil error is returned only when the subscription is refused before any
// bytes are written, so the caller can still emit an HTTP error response.
func (s sseStreamer[E, F]) run(ctx context.Context, sink sseSink, filter F, lastEventID int64, clientKey string) error {
	sub, err := s.broker.Subscribe(filter, clientKey)
	if err != nil {
		return err
	}
	defer s.broker.Unsubscribe(sub)

	// Started before backfill so a slow backfill cannot extend the stream.
	// Ending the response makes the client reconnect through auth again.
	lifetime := time.NewTimer(s.maxLifetime())
	defer lifetime.Stop()

	// Flush the response headers on connect so the client's EventSource fires
	// onopen immediately, rather than waiting up to one heartbeat interval when
	// the stream is quiet and the backfill returns nothing. Subscribe already
	// succeeded, so committing the 200 here doesn't conflict with the
	// before-bytes error contract above (audit N4).
	sink.flush()

	backfilled := s.backfill(ctx, sink, filter, lastEventID)

	heartbeat := time.NewTicker(s.heartbeatPeriod())
	defer heartbeat.Stop()

	for {
		select {
		case msg, ok := <-sub.Chan():
			if !ok {
				return nil // the broker shut down or evicted this slow reader
			}
			sub.Consumed(msg)
			// Skip events already sent during backfill.
			if _, sent := backfilled[msg.ID]; sent {
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
		case <-lifetime.C:
			s.drain(sink, sub, backfilled)
			return nil
		case <-ctx.Done():
			return nil
		}
	}
}

// drain writes the events queued on sub when the lifetime runs out, so those
// broadcast just before the end are not discarded. It takes only what is
// queued when it starts, under one write deadline, so neither a busy feed nor
// a slow client keeps the stream open past one write timeout.
func (s sseStreamer[E, F]) drain(sink sseSink, sub *broker.Subscription[F], backfilled map[int64]struct{}) {
	queued := len(sub.Chan())
	if queued == 0 {
		return
	}
	if err := sink.setWriteDeadline(time.Now().Add(s.writeDeadline())); err != nil {
		s.logger.Warn(s.label+" sse: failed to set write deadline", "err", err)
		return
	}
	for range queued {
		// Only this goroutine receives, so the counted messages are there
		// unless an eviction closed the channel.
		msg, ok := <-sub.Chan()
		if !ok {
			break
		}
		sub.Consumed(msg)
		if _, sent := backfilled[msg.ID]; sent {
			continue
		}
		if err := sink.writeEvent(msg.ID, s.label, msg.Data); err != nil {
			return
		}
	}
	sink.flush()
}

// backfill sends recent events (or events since lastEventID) to a newly
// connected client and returns the IDs it sent, so the live loop skips those
// and only those. Events reach the broker out of ID order, so a live event
// below the highest backfilled ID, or below lastEventID, can still be new to
// the client. The query runs under its own deadline; a cancel is told apart
// from a failed query by the request context ctx, so a resume that runs out of
// time still gets its resync.
func (s sseStreamer[E, F]) backfill(ctx context.Context, sink sseSink, filter F, lastEventID int64) map[int64]struct{} {
	queryCtx, cancel := context.WithTimeout(ctx, s.backfillDeadline())
	defer cancel()

	if lastEventID > 0 {
		// Resume from where the client left off. The one extra row shows
		// whether the backfill reaches the live stream.
		events, err := s.since(queryCtx, filter, lastEventID, sseBackfillLimit+1)
		if err != nil {
			if ctx.Err() != nil {
				s.logger.Debug(s.label+" backfill canceled", "last_event_id", lastEventID, "err", err)
			} else {
				s.logger.Warn(s.label+" backfill since id failed", "last_event_id", lastEventID, "err", err)
				// The client is behind by an unknown amount: have it reload
				// history instead of leaving a silent gap.
				if err := sink.writeResync(); err == nil {
					sink.flush()
				}
			}
			return nil
		}
		truncated := len(events) > sseBackfillLimit
		if truncated {
			events = events[:sseBackfillLimit]
		}
		sent := make(map[int64]struct{}, len(events))
		// since() is already in chronological order (ASC).
		for i := range events {
			data, ok := mustJSON(events[i])
			if !ok {
				continue
			}
			id := s.eventID(events[i])
			if err := sink.writeEvent(id, s.label, data); err != nil {
				return sent
			}
			sent[id] = struct{}{}
		}
		if truncated {
			// Live delivery starts at the subscription, so the events past
			// the limit would be skipped silently.
			if err := sink.writeResync(); err != nil {
				return sent
			}
		}
		if len(events) > 0 {
			sink.flush()
		}
		return sent
	}

	// Default: send recent matching events.
	recent, err := s.recent(queryCtx, filter, sseBackfillLimit)
	if err != nil {
		if ctx.Err() != nil {
			s.logger.Debug(s.label+" backfill canceled", "err", err)
		} else {
			s.logger.Warn(s.label+" backfill failed", "err", err)
		}
		return nil
	}
	sent := make(map[int64]struct{}, len(recent))
	// recent() is newest-first (DESC); emit oldest-first.
	for i := len(recent) - 1; i >= 0; i-- {
		data, ok := mustJSON(recent[i])
		if !ok {
			continue
		}
		id := s.eventID(recent[i])
		if err := sink.writeEvent(id, s.label, data); err != nil {
			return sent
		}
		sent[id] = struct{}{}
	}
	if len(recent) > 0 {
		sink.flush()
	}
	return sent
}
