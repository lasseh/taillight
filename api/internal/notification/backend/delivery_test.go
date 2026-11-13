package backend

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"sync"
	"testing"

	"github.com/lasseh/taillight/internal/model"
	"github.com/lasseh/taillight/internal/notification"
)

// publicTestURL is a TEST-NET-2 literal: it passes the SSRF URL check without
// a DNS lookup, and loopbackTransport makes sure nothing is ever sent to it.
const publicTestURL = "http://198.51.100.20/hook"

// loopbackTransport sends every request to a local test server whatever its
// URL says, so a backend's Send runs end to end against httptest. It forwards
// to a real http.Transport, which applies net/http's request checks.
type loopbackTransport struct {
	addr string
	next *http.Transport
}

func (lt loopbackTransport) RoundTrip(req *http.Request) (*http.Response, error) {
	r := req.Clone(req.Context())
	r.URL.Scheme = "http"
	r.URL.Host = lt.addr
	return lt.next.RoundTrip(r)
}

// routeToServer points client at srv for the rest of the test.
func routeToServer(t *testing.T, client *http.Client, srv *httptest.Server) {
	t.Helper()
	next := &http.Transport{}
	t.Cleanup(next.CloseIdleConnections)
	client.Transport = loopbackTransport{addr: srv.Listener.Addr().String(), next: next}
}

func mustJSON(t *testing.T, v any) json.RawMessage {
	t.Helper()
	b, err := json.Marshal(v)
	if err != nil {
		t.Fatal(err)
	}
	return b
}

// TestHTTPBackends_PayloadRejectionStatuses verifies that a response refusing
// the payload itself (400, 413, 422) is reported as ErrPayloadRejected, while
// responses that point at the channel or the destination still count as
// ordinary failures.
func TestHTTPBackends_PayloadRejectionStatuses(t *testing.T) {
	ntfy := NewNtfy(discardLogger())
	slack := NewSlack(discardLogger())
	webhook := NewWebhook(discardLogger())
	backends := []struct {
		name   string
		client *http.Client
		n      notification.Notifier
		config json.RawMessage
	}{
		{"ntfy", ntfy.client, ntfy, mustJSON(t, ntfyConfig{ServerURL: "http://198.51.100.20", Topic: "alerts"})},
		{"slack", slack.client, slack, mustJSON(t, slackConfig{WebhookURL: publicTestURL})},
		{"webhook", webhook.client, webhook, mustJSON(t, webhookConfig{URL: publicTestURL})},
	}

	statuses := []struct {
		code     int
		rejected bool
	}{
		{http.StatusBadRequest, true},
		{http.StatusRequestEntityTooLarge, true},
		{http.StatusUnprocessableEntity, true},
		{http.StatusUnauthorized, false},
		{http.StatusForbidden, false},
		{http.StatusNotFound, false},
		{http.StatusRequestTimeout, false},
		{http.StatusTooManyRequests, false},
		{http.StatusInternalServerError, false},
		{http.StatusBadGateway, false},
		{http.StatusServiceUnavailable, false},
	}

	payload := notification.Payload{
		Kind:       notification.EventKindSrvlog,
		EventCount: 1,
		SrvlogEvent: &model.SrvlogEvent{
			Hostname: "router1",
			Severity: 3,
			Message:  "link down",
		},
	}

	for _, b := range backends {
		for _, st := range statuses {
			t.Run(fmt.Sprintf("%s/%d", b.name, st.code), func(t *testing.T) {
				srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
					w.WriteHeader(st.code)
				}))
				defer srv.Close()
				routeToServer(t, b.client, srv)

				res := b.n.Send(context.Background(), notification.Channel{Config: b.config}, payload)
				if res.Success || res.Error == nil {
					t.Fatalf("status %d reported as success", st.code)
				}
				if res.StatusCode != st.code {
					t.Errorf("StatusCode = %d, want %d", res.StatusCode, st.code)
				}
				if got := errors.Is(res.Error, notification.ErrPayloadRejected); got != st.rejected {
					t.Errorf("errors.Is(%v, ErrPayloadRejected) = %v, want %v", res.Error, got, st.rejected)
				}
			})
		}
	}
}

// TestNtfySend_TitleControlBytesRemoved verifies that an event whose service
// carries a control byte is still delivered, with the byte removed from the
// Title header.
func TestNtfySend_TitleControlBytesRemoved(t *testing.T) {
	var (
		mu     sync.Mutex
		titles []string
	)
	srv := httptest.NewServer(http.HandlerFunc(func(_ http.ResponseWriter, r *http.Request) {
		mu.Lock()
		titles = append(titles, r.Header.Get("Title"))
		mu.Unlock()
	}))
	defer srv.Close()

	n := NewNtfy(discardLogger())
	routeToServer(t, n.client, srv)
	ch := notification.Channel{Config: mustJSON(t, ntfyConfig{ServerURL: "http://198.51.100.20", Topic: "alerts"})}

	var controls []byte
	for b := 0x00; b < 0x20; b++ {
		if b != '\t' {
			controls = append(controls, byte(b))
		}
	}
	controls = append(controls, 0x7f)

	const want = "[ERROR] svcx host1"
	for _, c := range controls {
		t.Run(fmt.Sprintf("0x%02x", c), func(t *testing.T) {
			payload := notification.Payload{
				Kind:       notification.EventKindAppLog,
				EventCount: 1,
				AppLogEvent: &model.AppLogEvent{
					Level:   "ERROR",
					Service: "svc" + string([]byte{c}) + "x",
					Host:    "host1",
					Msg:     "boom",
				},
			}
			res := n.Send(context.Background(), ch, payload)
			if !res.Success {
				t.Fatalf("send failed: %v", res.Error)
			}
			mu.Lock()
			got := titles[len(titles)-1]
			mu.Unlock()
			if got != want {
				t.Errorf("Title = %q, want %q", got, want)
			}
		})
	}
}

// TestWebhookSend_TemplateErrorIsPayloadRejection verifies that a custom
// template that fails on one event's fields is reported as a refused payload,
// so it does not count toward the channel's breaker, and sends nothing.
func TestWebhookSend_TemplateErrorIsPayloadRejection(t *testing.T) {
	webhook := NewWebhook(discardLogger())
	called := false
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		called = true
		w.WriteHeader(http.StatusOK)
	}))
	defer srv.Close()
	routeToServer(t, webhook.client, srv)

	cfg := mustJSON(t, webhookConfig{URL: publicTestURL, Template: `{"m":"{{slice .SrvlogEvent.Message 0 50}}"}`})
	payload := notification.Payload{
		Kind:        notification.EventKindSrvlog,
		EventCount:  1,
		SrvlogEvent: &model.SrvlogEvent{Hostname: "router1", Severity: 3, Message: "short"},
	}
	res := webhook.Send(context.Background(), notification.Channel{Config: cfg}, payload)
	if res.Success || res.Error == nil {
		t.Fatal("template error reported as success")
	}
	if !errors.Is(res.Error, notification.ErrPayloadRejected) {
		t.Errorf("errors.Is(%v, ErrPayloadRejected) = false, want true", res.Error)
	}
	if called {
		t.Error("webhook sent a request after the template failed")
	}
}
