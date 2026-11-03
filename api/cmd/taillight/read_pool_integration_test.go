package main

import (
	"bufio"
	"context"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"os"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/lasseh/taillight/internal/broker"
	"github.com/lasseh/taillight/internal/config"
	"github.com/lasseh/taillight/internal/ingestbridge"
	"github.com/lasseh/taillight/internal/model"
	"github.com/lasseh/taillight/internal/mute"
	"github.com/lasseh/taillight/internal/postgres"
)

// srvlogStreamIDs reads an SSE stream and sends the id of each srvlog event
// on the returned channel, which is closed when the stream ends.
func srvlogStreamIDs(body io.Reader) <-chan int64 {
	ids := make(chan int64, 8)
	go func() {
		defer close(ids)
		var id string
		sc := bufio.NewScanner(body)
		for sc.Scan() {
			line := sc.Text()
			if v, ok := strings.CutPrefix(line, "id: "); ok {
				id = v
			}
			if line == "event: srvlog" {
				n, _ := strconv.ParseInt(id, 10, 64)
				ids <- n
			}
		}
	}()
	return ids
}

// TestIntegration_FullReadPoolLeavesAuthIngestAndHealth holds every
// connection of the read pool, as long-running read queries would, and
// checks on the production router that a log read is starved while a
// session check, applog ingest, an ingest-bridge dispatch and /health still
// succeed on the main pool, and that an SSE stream outlives the REST read
// timeout and still delivers the dispatched event.
func TestIntegration_FullReadPoolLeavesAuthIngestAndHealth(t *testing.T) {
	// A short REST read timeout keeps the "stream outlives it" check quick.
	defer func(d time.Duration) { readTimeout = d }(readTimeout)
	readTimeout = time.Second

	pool := migratedTestPool(t, "srvlog_events", "applog_events")
	ctx := context.Background()
	if _, err := pool.Exec(ctx, "TRUNCATE sessions, api_keys, users, mute_rules CASCADE"); err != nil {
		t.Fatalf("truncate: %v", err)
	}
	readCfg, err := pgxpool.ParseConfig(os.Getenv("TEST_DATABASE_URL"))
	if err != nil {
		t.Fatalf("parse read pool config: %v", err)
	}
	readCfg.MaxConns = 2
	readPool, err := pgxpool.NewWithConfig(ctx, readCfg)
	if err != nil {
		t.Fatalf("read pool: %v", err)
	}
	t.Cleanup(readPool.Close)

	logger := slog.New(slog.DiscardHandler)
	store := postgres.NewStore(pool)
	authStore := postgres.NewAuthStore(pool)
	t.Cleanup(authStore.Stop)
	srvlogBroker := broker.NewSrvlogBroker(logger)
	mutes := mute.NewSet(store, time.Now, logger)
	router := setupRouter(config.Config{AuthEnabled: true, AuthReadEndpoints: true}, logger,
		store, postgres.NewStore(readPool), authStore, &fakeDirectory{}, nil,
		srvlogBroker, broker.NewNetlogBroker(logger), broker.NewAppLogBroker(logger),
		nil, nil, nil, nil, mutes)
	srv := httptest.NewServer(router)
	defer srv.Close()

	session := sessionFrom(t, passwordLogin(t, router, "nina"))
	_, ingestKey := storeKey(t, authStore, "shipper", false, "ingest")

	// Take every read-pool connection and keep it.
	held := make([]*pgxpool.Conn, 0, readCfg.MaxConns)
	defer func() {
		for _, c := range held {
			c.Release()
		}
	}()
	for range readCfg.MaxConns {
		c, err := readPool.Acquire(ctx)
		if err != nil {
			t.Fatalf("acquire read connection: %v", err)
		}
		held = append(held, c)
	}

	// The stream connects through auth on the main pool. Its backfill waits
	// on the full read pool until the backfill deadline, then goes live.
	streamCtx, stopStream := context.WithCancel(ctx)
	defer stopStream()
	req, err := http.NewRequestWithContext(streamCtx, http.MethodGet, srv.URL+"/api/v1/srvlog/stream", nil)
	if err != nil {
		t.Fatalf("new stream request: %v", err)
	}
	req.AddCookie(session)
	resp, err := srv.Client().Do(req)
	if err != nil {
		t.Fatalf("connect stream: %v", err)
	}
	defer resp.Body.Close() //nolint:errcheck // test cleanup; closing ends the stream.
	connectedAt := time.Now()
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("stream status = %d, want 200", resp.StatusCode)
	}
	streamed := srvlogStreamIDs(resp.Body)

	// The read pool really is full: a log read times out at readTimeout.
	if rec := call(t, router, http.MethodGet, "/api/v1/srvlog?limit=1", "", "", session); rec.Code != http.StatusGatewayTimeout {
		t.Fatalf("GET /srvlog with the read pool full = %d, want 504; body %s", rec.Code, rec.Body.String())
	}

	if rec := call(t, router, http.MethodGet, "/api/v1/auth/me", "", "", session); rec.Code != http.StatusOK {
		t.Errorf("GET /auth/me = %d, want 200; body %s", rec.Code, rec.Body.String())
	}
	if rec := call(t, router, http.MethodGet, "/health", "", "", nil); rec.Code != http.StatusOK {
		t.Errorf("GET /health = %d, want 200; body %s", rec.Code, rec.Body.String())
	}
	ts := time.Now().UTC().Format(time.RFC3339)
	ingestBody := `{"logs":[{"timestamp":"` + ts + `","level":"INFO","msg":"ok","service":"api","host":"web01"}]}`
	if rec := call(t, router, http.MethodPost, "/api/v1/applog/ingest", ingestBody, ingestKey, nil); rec.Code != http.StatusAccepted {
		t.Errorf("POST /applog/ingest = %d, want 202; body %s", rec.Code, rec.Body.String())
	}

	// Past the REST read timeout, a new syslog row goes through the ingest
	// bridge and reaches the still-open stream.
	time.Sleep(2*readTimeout - time.Since(connectedAt))
	var id int64
	if err := pool.QueryRow(ctx,
		`INSERT INTO srvlog_events (received_at, reported_at, hostname, fromhost_ip, severity, facility, message)
		 VALUES (now(), now(), 'web01', '10.0.0.1'::inet, 3, 1, 'disk full') RETURNING id`).Scan(&id); err != nil {
		t.Fatalf("insert srvlog event: %v", err)
	}
	broadcast := false
	ingestbridge.Dispatch(ctx, ingestbridge.Notification{Channel: ingestbridge.ChannelSrvlog, ID: id}, store, mutes,
		ingestbridge.Sinks{BroadcastSrvlog: func(e model.SrvlogEvent) {
			broadcast = true
			srvlogBroker.Broadcast(e)
		}}, logger, 5*time.Second)
	if !broadcast {
		t.Fatal("ingest dispatch did not broadcast the event")
	}

	select {
	case got, ok := <-streamed:
		if !ok {
			t.Fatal("stream ended before delivering the dispatched event")
		}
		if got != id {
			t.Fatalf("stream delivered event %d, want %d", got, id)
		}
		if age := time.Since(connectedAt); age <= readTimeout {
			t.Errorf("event arrived %v after connect, want after the %v REST read timeout", age, readTimeout)
		}
	case <-time.After(15 * time.Second):
		t.Fatal("stream did not deliver the dispatched event")
	}
}
