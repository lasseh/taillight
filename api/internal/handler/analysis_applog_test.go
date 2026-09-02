package handler

import (
	"bytes"
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func postReport(t *testing.T, h *AnalysisHandler, body map[string]any) *httptest.ResponseRecorder {
	t.Helper()
	raw, err := json.Marshal(body)
	if err != nil {
		t.Fatalf("marshal body: %v", err)
	}
	req := httptest.NewRequestWithContext(context.Background(), http.MethodPost, "/api/v1/analysis/reports", bytes.NewReader(raw))
	w := httptest.NewRecorder()
	h.Create(w, req)
	return w
}

// TestCreateApplogScopeRules pins the per-feed scope contract: applog takes
// services and rejects hosts, syslog feeds take hosts and reject services,
// and unknown service names fail before the worker sees the report.
func TestCreateApplogScopeRules(t *testing.T) {
	store := &stubAnalysisStore{
		knownHosts:    map[string][]string{"srvlog": {"a.lab"}},
		knownServices: []string{"api", "worker"},
	}
	cases := []struct {
		name     string
		body     map[string]any
		wantCode int
		wantBody string
	}{
		{"applog rejects hosts", map[string]any{"feed": "applog", "hosts": []string{"a.lab"}}, http.StatusBadRequest, "invalid_scope"},
		{"applog rejects unknown services", map[string]any{"feed": "applog", "services": []string{"api", "ghost"}}, http.StatusBadRequest, "unknown_services"},
		{"applog rejects weekly mode", map[string]any{"feed": "applog", "prompt_mode": "weekly"}, http.StatusBadRequest, "invalid_prompt_mode"},
		{"applog incident", map[string]any{"feed": "applog", "prompt_mode": "incident", "period_minutes": 60}, http.StatusCreated, `"prompt_mode":"incident"`},
		{"srvlog rejects services", map[string]any{"feed": "srvlog", "services": []string{"api"}}, http.StatusBadRequest, "invalid_scope"},
		{"applog unscoped daily", map[string]any{"feed": "applog"}, http.StatusCreated, `"feed":"applog"`},
		{"applog scoped daily", map[string]any{"feed": "applog", "services": []string{"worker"}}, http.StatusCreated, `"services":["worker"]`},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			h := NewAnalysisHandler(store, &stubEnqueuer{})
			w := postReport(t, h, tc.body)
			if w.Code != tc.wantCode {
				t.Fatalf("status: got %d, want %d; body=%s", w.Code, tc.wantCode, w.Body.String())
			}
			if !strings.Contains(w.Body.String(), tc.wantBody) {
				t.Errorf("body should contain %q: %s", tc.wantBody, w.Body.String())
			}
		})
	}
}

// TestCreateApplogNormalizesServices mirrors the host test: the enqueuer
// receives a sorted, deduped service list and an empty host list.
func TestCreateApplogNormalizesServices(t *testing.T) {
	store := &stubAnalysisStore{knownServices: []string{"api", "billing", "worker"}}
	enq := &stubEnqueuer{}
	h := NewAnalysisHandler(store, enq)

	w := postReport(t, h, map[string]any{
		"feed":     "applog",
		"services": []string{"worker", " api ", "api", "billing"},
	})
	if w.Code != http.StatusCreated {
		t.Fatalf("status: got %d, want 201; body=%s", w.Code, w.Body.String())
	}
	want := []string{"api", "billing", "worker"}
	if len(enq.got.Services) != len(want) {
		t.Fatalf("Services: got %v, want %v", enq.got.Services, want)
	}
	for i, svc := range want {
		if enq.got.Services[i] != svc {
			t.Errorf("Services[%d]: got %q, want %q", i, enq.got.Services[i], svc)
		}
	}
	if len(enq.got.Hosts) != 0 {
		t.Errorf("Hosts must be empty on an applog report, got %v", enq.got.Hosts)
	}
	if enq.got.PromptMode != "daily" {
		t.Errorf("PromptMode = %q, want daily", enq.got.PromptMode)
	}
}

func TestServicesReturnsEntries(t *testing.T) {
	store := &stubAnalysisStore{knownServices: []string{"api", "worker"}}
	h := NewAnalysisHandler(store, nil)

	req := httptest.NewRequestWithContext(context.Background(), http.MethodGet, "/api/v1/analysis/services", http.NoBody)
	w := httptest.NewRecorder()
	h.Services(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("status: got %d, want 200; body=%s", w.Code, w.Body.String())
	}
	var resp struct {
		Data []struct {
			Service string `json:"service"`
		} `json:"data"`
	}
	if err := json.Unmarshal(w.Body.Bytes(), &resp); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if len(resp.Data) != 2 || resp.Data[0].Service != "api" || resp.Data[1].Service != "worker" {
		t.Errorf("data = %+v, want api, worker", resp.Data)
	}
}

// TestServicesEmptyIsArray guards the nil-slice contract: no services
// serialises as [] rather than null.
func TestServicesEmptyIsArray(t *testing.T) {
	h := NewAnalysisHandler(&stubAnalysisStore{}, nil)
	req := httptest.NewRequestWithContext(context.Background(), http.MethodGet, "/api/v1/analysis/services", http.NoBody)
	w := httptest.NewRecorder()
	h.Services(w, req)
	if !strings.Contains(w.Body.String(), `"data":[]`) {
		t.Errorf("empty list should serialise as []: %s", w.Body.String())
	}
}
