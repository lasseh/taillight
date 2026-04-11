//go:build integration

package client_test

import (
	"context"
	"os"
	"testing"
	"time"

	"github.com/lasseh/taillight/internal/tui/client"
)

func testClient(t *testing.T) *client.Client {
	t.Helper()
	url := os.Getenv("TAILLIGHT_URL")
	if url == "" {
		url = "http://localhost:8080"
	}
	return client.New(client.Config{BaseURL: url})
}

func TestHealth(t *testing.T) {
	c := testClient(t)
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	if err := c.Health(ctx); err != nil {
		t.Fatalf("Health: %v", err)
	}
}

func TestSrvlogHosts(t *testing.T) {
	c := testClient(t)
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	hosts, err := c.SrvlogHosts(ctx)
	if err != nil {
		t.Fatalf("SrvlogHosts: %v", err)
	}
	if len(hosts) == 0 {
		t.Fatal("SrvlogHosts returned empty list")
	}
	t.Logf("Got %d hosts: %v", len(hosts), hosts[:min(5, len(hosts))])
}

func TestSrvlogPrograms(t *testing.T) {
	c := testClient(t)
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	programs, err := c.SrvlogPrograms(ctx)
	if err != nil {
		t.Fatalf("SrvlogPrograms: %v", err)
	}
	if len(programs) == 0 {
		t.Fatal("SrvlogPrograms returned empty list")
	}
	t.Logf("Got %d programs: %v", len(programs), programs[:min(5, len(programs))])
}

func TestListSrvlogs(t *testing.T) {
	c := testClient(t)
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	resp, err := c.ListSrvlogs(ctx, client.SrvlogFilter{SeverityMax: -1, Facility: -1}, "", 10)
	if err != nil {
		t.Fatalf("ListSrvlogs: %v", err)
	}
	if len(resp.Data) == 0 {
		t.Fatal("ListSrvlogs returned no events")
	}
	t.Logf("Got %d events, has_more=%v", len(resp.Data), resp.HasMore)

	e := resp.Data[0]
	t.Logf("First event: id=%d host=%s sev=%d(%s) prog=%s msg=%.60s",
		e.ID, e.Hostname, e.Severity, e.SeverityLabel, e.Programname, e.Message)
}

func TestSrvlogSummary(t *testing.T) {
	c := testClient(t)
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	summary, err := c.SrvlogSummary(ctx, "24h")
	if err != nil {
		t.Fatalf("SrvlogSummary: %v", err)
	}
	t.Logf("Summary: total=%d errors=%d warnings=%d trend=%.1f%%",
		summary.Total, summary.Errors, summary.Warnings, summary.Trend)
	t.Logf("Severity breakdown: %d levels", len(summary.SeverityBreakdown))
	t.Logf("Top hosts: %d", len(summary.TopHosts))
}

func TestSSEStream(t *testing.T) {
	c := testClient(t)
	filter := client.SrvlogFilter{SeverityMax: -1, Facility: -1}
	stream := client.NewSSEStream(c, "/api/v1/srvlog/stream", filter.Params(), 0)
	defer stream.Close()

	// Wait for connection.
	deadline := time.After(5 * time.Second)
	for !stream.Connected() {
		select {
		case <-deadline:
			t.Fatal("SSE stream failed to connect within 5s")
		case <-time.After(100 * time.Millisecond):
		}
	}
	t.Log("SSE connected")

	// Read at least one event.
	select {
	case evt := <-stream.Events():
		t.Logf("Got SSE event: id=%d event=%s data_len=%d", evt.ID, evt.Event, len(evt.Data))
		if evt.ID <= 0 {
			t.Error("Event ID should be > 0")
		}
		if evt.Event != "srvlog" {
			t.Errorf("Event type = %q, want srvlog", evt.Event)
		}
	case <-time.After(10 * time.Second):
		t.Fatal("No SSE event received within 10s")
	}
}

func TestHosts(t *testing.T) {
	c := testClient(t)
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	hosts, err := c.Hosts(ctx, "24h")
	if err != nil {
		t.Fatalf("Hosts: %v", err)
	}
	t.Logf("Got %d hosts", len(hosts))
	if len(hosts) > 0 {
		h := hosts[0]
		t.Logf("First host: %s feed=%s total=%d errors=%d", h.Hostname, h.Feed, h.TotalCount, h.ErrorCount)
	}
}
