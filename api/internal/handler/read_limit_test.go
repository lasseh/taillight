package handler

import (
	"context"
	"net/http"
	"net/http/httptest"
	"sync"
	"testing"
	"time"

	"github.com/jackc/pgx/v5/pgtype"

	"github.com/lasseh/taillight/internal/auth"
	"github.com/lasseh/taillight/internal/model"
)

// userRequest returns a GET request authenticated as the user with the given
// id byte.
func userRequest(id byte) *http.Request {
	u := &model.User{ID: pgtype.UUID{Bytes: [16]byte{id}, Valid: true}, Username: "u", IsActive: true}
	ctx := auth.WithUser(context.Background(), u)
	return httptest.NewRequestWithContext(ctx, http.MethodGet, "/api/v1/srvlog", nil)
}

// One principal at its cap waits and then gets 429, while another principal
// is served at once; a waiting request runs as soon as a slot frees.
func TestReadLimiterCapsEachPrincipal(t *testing.T) {
	l := NewReadLimiter(ReadLimitPerPrincipal)
	l.wait = 100 * time.Millisecond

	release := make(chan struct{})
	started := make(chan struct{}, ReadLimitPerPrincipal+1)
	h := l.Middleware(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("X-Hold") != "" {
			started <- struct{}{}
			<-release
		}
		w.WriteHeader(http.StatusOK)
	}))

	// Fill user 1's slots with held requests.
	var held sync.WaitGroup
	for range ReadLimitPerPrincipal {
		held.Go(func() {
			req := userRequest(1)
			req.Header.Set("X-Hold", "1")
			h.ServeHTTP(httptest.NewRecorder(), req)
		})
	}
	for range ReadLimitPerPrincipal {
		<-started
	}

	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, userRequest(1))
	if rec.Code != http.StatusTooManyRequests {
		t.Errorf("user at cap: status = %d, want 429", rec.Code)
	}
	if rec.Header().Get("Retry-After") == "" {
		t.Error("429 without Retry-After")
	}

	rec = httptest.NewRecorder()
	h.ServeHTTP(rec, userRequest(2))
	if rec.Code != http.StatusOK {
		t.Errorf("other user: status = %d, want 200", rec.Code)
	}

	// A request that starts waiting runs once a held one finishes.
	l.wait = 5 * time.Second
	done := make(chan int)
	go func() {
		rec := httptest.NewRecorder()
		h.ServeHTTP(rec, userRequest(1))
		done <- rec.Code
	}()
	time.Sleep(20 * time.Millisecond) // let it queue behind the held requests
	release <- struct{}{}
	select {
	case code := <-done:
		if code != http.StatusOK {
			t.Errorf("queued request: status = %d, want 200", code)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("queued request did not run after a slot freed")
	}

	close(release)
	held.Wait()
	l.mu.Lock()
	defer l.mu.Unlock()
	if len(l.slots) != 0 {
		t.Errorf("limiter keeps %d principal entries after all requests ended, want 0", len(l.slots))
	}
}

// A request whose client goes away while it waits for a slot leaves without
// running the handler or writing a response.
func TestReadLimiterWaitEndsWithRequest(t *testing.T) {
	l := NewReadLimiter(ReadLimitPerPrincipal)
	l.max = 0 // no free slot ever
	req := userRequest(1)
	ctx, cancel := context.WithCancel(req.Context())
	req = req.WithContext(ctx)
	cancel()

	rec := httptest.NewRecorder()
	l.Middleware(http.HandlerFunc(func(http.ResponseWriter, *http.Request) {
		t.Error("handler ran without a slot")
	})).ServeHTTP(rec, req)
	if rec.Body.Len() != 0 {
		t.Errorf("wrote %q after the client went away, want nothing", rec.Body.String())
	}
}

// Anonymous requests from one client IP share the anonymous cap, which can
// differ from the per-user cap.
func TestReadLimiterAnonymousCap(t *testing.T) {
	const anonMax = ReadLimitPerPrincipal + 2
	l := NewReadLimiter(anonMax)
	l.wait = 50 * time.Millisecond

	release := make(chan struct{})
	started := make(chan struct{}, anonMax)
	h := l.Middleware(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("X-Hold") != "" {
			started <- struct{}{}
			<-release
		}
		w.WriteHeader(http.StatusOK)
	}))
	anonRequest := func() *http.Request {
		return httptest.NewRequestWithContext(context.Background(), http.MethodGet, "/api/v1/srvlog", nil)
	}

	var held sync.WaitGroup
	for range anonMax {
		held.Go(func() {
			req := anonRequest()
			req.Header.Set("X-Hold", "1")
			h.ServeHTTP(httptest.NewRecorder(), req)
		})
	}
	for range anonMax {
		<-started
	}

	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, anonRequest())
	if rec.Code != http.StatusTooManyRequests {
		t.Errorf("anonymous client past %d in flight: status = %d, want 429", anonMax, rec.Code)
	}
	close(release)
	held.Wait()
}
