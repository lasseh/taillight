// Package ingestbridge routes a LISTEN/NOTIFY notification to the right event
// fetch and fan-out. This logic previously lived as an untested anonymous
// goroutine body in cmd/taillight/serve.go, reachable only with a live
// Postgres; here it is one function testable with a fake fetcher.
//
// Scope note: this deliberately does NOT abstract the Listener itself. The
// Listener's reconnect and gap-fill are genuinely Postgres-bound and deep;
// mocking pgx.Conn would cost more than it returns. Only the shallow,
// untested dispatch step is extracted. The worker pool and metrics stay at
// the call site (trivial plumbing).
package ingestbridge

import (
	"context"
	"log/slog"
	"time"

	"github.com/lasseh/taillight/internal/model"
)

// NOTIFY channel names. These mirror the keys used by the Postgres listener;
// kept here so the dispatch routing owns the strings it switches on.
const (
	ChannelSrvlog = "srvlog_ingest"
	ChannelNetlog = "netlog_ingest"
)

// Notification is a row id arriving on a NOTIFY channel.
type Notification struct {
	Channel string
	ID      int64
}

// EventFetcher loads a full event by id. *postgres.Store satisfies this in
// production; tests use an in-memory fake.
type EventFetcher interface {
	GetSrvlog(ctx context.Context, id int64) (model.SrvlogEvent, error)
	GetNetlog(ctx context.Context, id int64) (model.NetlogEvent, error)
}

// Muter returns the id of the mute rule matching an event, or nil when no
// active rule matches. *mute.Set satisfies this in production.
type Muter interface {
	MatchSrvlog(model.SrvlogEvent) *int64
	MatchNetlog(model.NetlogEvent) *int64
}

// Sinks receive dispatched events. Broadcast fans an event out to SSE
// clients; Notify hands it to the notification engine. A nil Broadcast
// disables that feed, so its rows are not even fetched. A nil Notify means
// no engine.
type Sinks struct {
	BroadcastSrvlog, NotifySrvlog func(model.SrvlogEvent)
	BroadcastNetlog, NotifyNetlog func(model.NetlogEvent)
}

// Dispatch fetches the notified row, marks it with the mute rule that matches
// it, broadcasts it, and notifies only when it is not muted. A nil Muter
// means nothing is muted. Fetch failures are logged and swallowed so one bad
// row never breaks the worker loop. The caller owns worker concurrency and
// metrics.
func Dispatch(
	ctx context.Context,
	n Notification,
	f EventFetcher,
	m Muter,
	s Sinks,
	logger *slog.Logger,
	fetchTimeout time.Duration,
) {
	switch n.Channel {
	case ChannelSrvlog:
		if s.BroadcastSrvlog == nil {
			return
		}
		qctx, cancel := context.WithTimeout(ctx, fetchTimeout)
		event, err := f.GetSrvlog(qctx, n.ID)
		cancel()
		if err != nil {
			logger.Warn("fetch srvlog event for broadcast", "id", n.ID, "err", err)
			return
		}
		if m != nil {
			event.MutedBy = m.MatchSrvlog(event)
		}
		s.BroadcastSrvlog(event)
		if event.MutedBy == nil && s.NotifySrvlog != nil {
			s.NotifySrvlog(event)
		}
	case ChannelNetlog:
		if s.BroadcastNetlog == nil {
			return
		}
		qctx, cancel := context.WithTimeout(ctx, fetchTimeout)
		event, err := f.GetNetlog(qctx, n.ID)
		cancel()
		if err != nil {
			logger.Warn("fetch netlog event for broadcast", "id", n.ID, "err", err)
			return
		}
		if m != nil {
			event.MutedBy = m.MatchNetlog(event)
		}
		s.BroadcastNetlog(event)
		if event.MutedBy == nil && s.NotifyNetlog != nil {
			s.NotifyNetlog(event)
		}
	default:
		// Unknown channel — nothing to route. Metrics are recorded by the
		// caller before Dispatch, matching the prior behaviour.
	}
}
