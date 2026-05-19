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

// Dispatch fetches the notified row and hands it to the matching sink. A nil
// sink means that plane is disabled — the row is not even fetched. Fetch
// failures are logged and swallowed so one bad row never breaks the worker
// loop. The caller owns worker concurrency and metrics.
func Dispatch(
	ctx context.Context,
	n Notification,
	f EventFetcher,
	onSrvlog func(model.SrvlogEvent),
	onNetlog func(model.NetlogEvent),
	logger *slog.Logger,
	fetchTimeout time.Duration,
) {
	switch n.Channel {
	case ChannelSrvlog:
		if onSrvlog == nil {
			return
		}
		qctx, cancel := context.WithTimeout(ctx, fetchTimeout)
		event, err := f.GetSrvlog(qctx, n.ID)
		cancel()
		if err != nil {
			logger.Warn("fetch srvlog event for broadcast", "id", n.ID, "err", err)
			return
		}
		onSrvlog(event)
	case ChannelNetlog:
		if onNetlog == nil {
			return
		}
		qctx, cancel := context.WithTimeout(ctx, fetchTimeout)
		event, err := f.GetNetlog(qctx, n.ID)
		cancel()
		if err != nil {
			logger.Warn("fetch netlog event for broadcast", "id", n.ID, "err", err)
			return
		}
		onNetlog(event)
	default:
		// Unknown channel — nothing to route. Metrics are recorded by the
		// caller before Dispatch, matching the prior behaviour.
	}
}
