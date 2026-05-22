package handler

import (
	"bytes"
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/lasseh/taillight/internal/model"
)

// stubAnalysisStore satisfies AnalysisReportStore for the handler create-path
// tests. Only ListAnalysisHosts and DeleteReport are exercised here; the read
// methods are not invoked on the create code path so they return zero values.
type stubAnalysisStore struct {
	knownHosts map[string][]string // feed → host list
}

func (s *stubAnalysisStore) ListReports(context.Context, int) ([]model.AnalysisReportSummary, error) {
	return nil, nil
}

func (s *stubAnalysisStore) GetReportBySlug(context.Context, string) (model.AnalysisReport, error) {
	return model.AnalysisReport{}, nil
}

func (s *stubAnalysisStore) DeleteReport(context.Context, int64) error { return nil }

func (s *stubAnalysisStore) ListAnalysisHosts(_ context.Context, feed string) ([]string, error) {
	return s.knownHosts[feed], nil
}

// stubEnqueuer captures what the handler hands to Enqueue so tests can assert
// the host list arrived normalized and the feed/mode wiring is intact.
type stubEnqueuer struct {
	got model.AnalysisReport
}

func (e *stubEnqueuer) Enqueue(_ context.Context, req model.AnalysisReport) (model.AnalysisReport, error) {
	e.got = req
	// Fill in the bits the handler echoes back to the caller so the JSON
	// response body is well-formed.
	req.ID = 1
	req.Slug = "srvlog-incident-20260101-0000"
	req.Status = model.AnalysisStatusPending
	return req, nil
}

// TestCreateRejectsUnknownHosts is the load-bearing assertion for slice 01's
// validation contract: a host that isn't in the feed's metadata cache must
// produce 400 unknown_hosts before the worker ever sees the report. The
// alternative (silently producing an empty report) is exactly what the
// short-circuit in slice 04 is supposed to be a backstop for, not the
// primary failure mode.
func TestCreateRejectsUnknownHosts(t *testing.T) {
	store := &stubAnalysisStore{
		knownHosts: map[string][]string{
			"srvlog": {"edge01.lab", "edge02.lab"},
		},
	}
	enq := &stubEnqueuer{}
	h := NewAnalysisHandler(store, enq, true)

	body, _ := json.Marshal(map[string]any{
		"feed":           "srvlog",
		"prompt_mode":    "incident",
		"period_minutes": 60,
		"hosts":          []string{"edge01.lab", "ghost.lab"},
	})
	req := httptest.NewRequestWithContext(context.Background(), http.MethodPost, "/api/v1/analysis/reports", bytes.NewReader(body))
	w := httptest.NewRecorder()
	h.Create(w, req)

	if w.Code != http.StatusBadRequest {
		t.Fatalf("status: got %d, want 400", w.Code)
	}
	if !strings.Contains(w.Body.String(), "unknown_hosts") {
		t.Errorf("body should contain unknown_hosts code: %s", w.Body.String())
	}
	if !strings.Contains(w.Body.String(), "ghost.lab") {
		t.Errorf("body should name the bad host: %s", w.Body.String())
	}
	if enq.got.Feed != "" {
		t.Errorf("Enqueue must not be called on validation failure, got %+v", enq.got)
	}
}

// TestCreateNormalizesHosts confirms the handler hands a sorted, deduped host
// list to the enqueuer. The active-report uniqueness constraint relies on
// this — without it ["b","a"] and ["a","b"] would produce distinct rows for
// the same logical scope.
func TestCreateNormalizesHosts(t *testing.T) {
	store := &stubAnalysisStore{
		knownHosts: map[string][]string{
			"srvlog": {"a.lab", "b.lab", "c.lab"},
		},
	}
	enq := &stubEnqueuer{}
	h := NewAnalysisHandler(store, enq, true)

	body, _ := json.Marshal(map[string]any{
		"feed":           "srvlog",
		"prompt_mode":    "incident",
		"period_minutes": 60,
		"hosts":          []string{"c.lab", "a.lab", "a.lab", "b.lab"},
	})
	req := httptest.NewRequestWithContext(context.Background(), http.MethodPost, "/api/v1/analysis/reports", bytes.NewReader(body))
	w := httptest.NewRecorder()
	h.Create(w, req)

	if w.Code != http.StatusCreated {
		t.Fatalf("status: got %d, want 201; body=%s", w.Code, w.Body.String())
	}
	want := []string{"a.lab", "b.lab", "c.lab"}
	if len(enq.got.Hosts) != len(want) {
		t.Fatalf("Hosts len: got %v, want %v", enq.got.Hosts, want)
	}
	for i, h := range want {
		if enq.got.Hosts[i] != h {
			t.Errorf("Hosts[%d]: got %q, want %q", i, enq.got.Hosts[i], h)
		}
	}
}

// TestCreateAcceptsEmptyHostsAsAllHosts covers the default path: omitted /
// empty hosts means "all hosts on the feed," not validation failure.
func TestCreateAcceptsEmptyHostsAsAllHosts(t *testing.T) {
	store := &stubAnalysisStore{
		knownHosts: map[string][]string{"srvlog": {"a.lab"}},
	}
	enq := &stubEnqueuer{}
	h := NewAnalysisHandler(store, enq, true)

	body, _ := json.Marshal(map[string]any{
		"feed":        "srvlog",
		"prompt_mode": "daily",
	})
	req := httptest.NewRequestWithContext(context.Background(), http.MethodPost, "/api/v1/analysis/reports", bytes.NewReader(body))
	w := httptest.NewRecorder()
	h.Create(w, req)

	if w.Code != http.StatusCreated {
		t.Fatalf("status: got %d, want 201; body=%s", w.Code, w.Body.String())
	}
	if len(enq.got.Hosts) != 0 {
		t.Errorf("Hosts must be empty for the all-hosts path, got %v", enq.got.Hosts)
	}
}
