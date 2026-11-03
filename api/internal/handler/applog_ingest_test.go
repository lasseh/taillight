package handler

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/lasseh/taillight/internal/broker"
	"github.com/lasseh/taillight/internal/httputil"
	"github.com/lasseh/taillight/internal/model"
)

func TestIngest(t *testing.T) {
	now := time.Now().UTC().Truncate(time.Second)
	ts := now.Format(time.RFC3339)
	validEntry := `{"timestamp":"` + ts + `","level":"INFO","msg":"hello","service":"api","host":"web01"}`
	validBatch := `{"logs":[` + validEntry + `]}`

	tests := []struct {
		name       string
		body       string
		store      *mockAppLogStore
		wantStatus int
	}{
		{
			name:       "valid batch",
			body:       validBatch,
			store:      &mockAppLogStore{},
			wantStatus: http.StatusAccepted,
		},
		{
			name:       "invalid json",
			body:       `{bad json}`,
			store:      &mockAppLogStore{},
			wantStatus: http.StatusBadRequest,
		},
		{
			name:       "empty logs",
			body:       `{"logs":[]}`,
			store:      &mockAppLogStore{},
			wantStatus: http.StatusBadRequest,
		},
		{
			name:       "batch too large",
			body:       `{"logs":[` + strings.Repeat(validEntry+",", 1000) + validEntry + `]}`,
			store:      &mockAppLogStore{},
			wantStatus: http.StatusBadRequest,
		},
		{
			name:       "missing timestamp",
			body:       `{"logs":[{"level":"INFO","msg":"hello","service":"api","host":"web01"}]}`,
			store:      &mockAppLogStore{},
			wantStatus: http.StatusBadRequest,
		},
		{
			name:       "missing level",
			body:       `{"logs":[{"timestamp":"` + ts + `","msg":"hello","service":"api","host":"web01"}]}`,
			store:      &mockAppLogStore{},
			wantStatus: http.StatusBadRequest,
		},
		{
			name:       "invalid level",
			body:       `{"logs":[{"timestamp":"` + ts + `","level":"BOGUS","msg":"hello","service":"api","host":"web01"}]}`,
			store:      &mockAppLogStore{},
			wantStatus: http.StatusBadRequest,
		},
		{
			name:       "missing msg",
			body:       `{"logs":[{"timestamp":"` + ts + `","level":"INFO","service":"api","host":"web01"}]}`,
			store:      &mockAppLogStore{},
			wantStatus: http.StatusBadRequest,
		},
		{
			name:       "missing service",
			body:       `{"logs":[{"timestamp":"` + ts + `","level":"INFO","msg":"hello","host":"web01"}]}`,
			store:      &mockAppLogStore{},
			wantStatus: http.StatusBadRequest,
		},
		{
			name:       "missing host",
			body:       `{"logs":[{"timestamp":"` + ts + `","level":"INFO","msg":"hello","service":"api"}]}`,
			store:      &mockAppLogStore{},
			wantStatus: http.StatusBadRequest,
		},
		{
			name:       "msg too long",
			body:       `{"logs":[{"timestamp":"` + ts + `","level":"INFO","msg":"` + strings.Repeat("x", 65*1024) + `","service":"api","host":"web01"}]}`,
			store:      &mockAppLogStore{},
			wantStatus: http.StatusBadRequest,
		},
		{
			name:       "store error",
			body:       validBatch,
			store:      &mockAppLogStore{insertErr: errors.New("db error")},
			wantStatus: http.StatusInternalServerError,
		},
		{
			name:       "level alias WARNING accepted",
			body:       `{"logs":[{"timestamp":"` + ts + `","level":"WARNING","msg":"hello","service":"api","host":"web01"}]}`,
			store:      &mockAppLogStore{},
			wantStatus: http.StatusAccepted,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			b := broker.NewAppLogBroker(slog.Default())
			h := NewAppLogIngestHandler(tt.store, b, slog.Default(), nil)

			req := httptest.NewRequestWithContext(context.Background(), http.MethodPost, "/api/v1/applog/ingest", bytes.NewBufferString(tt.body))
			req.Header.Set("Content-Type", "application/json")
			rec := httptest.NewRecorder()

			h.Ingest(rec, req)

			if rec.Code != tt.wantStatus {
				t.Errorf("got status %d, want %d; body: %s", rec.Code, tt.wantStatus, rec.Body.String())
			}

			if tt.wantStatus == http.StatusAccepted {
				var resp map[string]int
				if err := json.NewDecoder(rec.Body).Decode(&resp); err != nil {
					t.Fatalf("decode: %v", err)
				}
				if resp["accepted"] < 1 {
					t.Errorf("expected accepted >= 1, got %d", resp["accepted"])
				}
			}
		})
	}
}

func TestIngestTimestampRange(t *testing.T) {
	tests := []struct {
		timestamp  string
		wantStatus int
	}{
		{"9998-12-31T23:59:59Z", http.StatusAccepted},
		{"0001-01-01T00:00:01Z", http.StatusAccepted},
		{"9998-12-31T23:59:59+14:00", http.StatusAccepted},
		{"0001-01-01T00:00:01-12:00", http.StatusAccepted},
		{"9999-06-01T00:00:00Z", http.StatusBadRequest},
		{"9999-12-31T23:59:59-23:59", http.StatusBadRequest},
		{"0000-06-01T00:00:00Z", http.StatusBadRequest},
		{"0000-01-01T00:00:00+23:59", http.StatusBadRequest},
		{"0001-01-01T00:00:00Z", http.StatusBadRequest}, // the zero time
	}

	for _, tt := range tests {
		t.Run(tt.timestamp, func(t *testing.T) {
			body := `{"logs":[{"timestamp":"` + tt.timestamp + `","level":"INFO","msg":"m","service":"api","host":"web01"}]}`
			h := NewAppLogIngestHandler(&mockAppLogStore{}, broker.NewAppLogBroker(slog.Default()), slog.Default(), nil)

			req := httptest.NewRequestWithContext(context.Background(), http.MethodPost, "/api/v1/applog/ingest", bytes.NewBufferString(body))
			rec := httptest.NewRecorder()
			h.Ingest(rec, req)

			if rec.Code != tt.wantStatus {
				t.Errorf("got status %d, want %d; body: %s", rec.Code, tt.wantStatus, rec.Body.String())
			}
		})
	}
}

// numberArray returns a JSON array holding n copies of num.
func numberArray(num string, n int) string {
	return "[" + strings.TrimSuffix(strings.Repeat(num+",", n), ",") + "]"
}

func TestIngestAttrsLimits(t *testing.T) {
	ts := time.Now().UTC().Format(time.RFC3339)

	tests := []struct {
		name       string
		attrs      string
		wantStatus int
	}{
		{name: "ordinary attrs", attrs: `{"user":"alice","count":42,"ratio":0.25,"ok":true,"tags":["a","b"],"nested":{"n":-7}}`, wantStatus: http.StatusAccepted},
		{name: "smallest float64", attrs: `{"n":5e-324}`, wantStatus: http.StatusAccepted},
		{name: "largest float64", attrs: `{"n":1.797e308}`, wantStatus: http.StatusAccepted},
		{name: "go float encoding", attrs: `{"a":1e+21,"b":1.5e-07}`, wantStatus: http.StatusAccepted},
		{name: "numeric-looking key and string", attrs: `{"1e131071":"1e131071"}`, wantStatus: http.StatusAccepted},
		{name: "huge positive exponent", attrs: `{"n":1e131071}`, wantStatus: http.StatusBadRequest},
		{name: "huge positive exponent upper case and nested", attrs: `{"a":{"b":[1E+131071]}}`, wantStatus: http.StatusBadRequest},
		{name: "huge negative exponent", attrs: `{"n":1e-16383}`, wantStatus: http.StatusBadRequest},
		{name: "exponent past int64", attrs: `{"n":1e99999999999999999999}`, wantStatus: http.StatusBadRequest},
		{name: "exponent at the bound", attrs: `{"n":1e-400}`, wantStatus: http.StatusAccepted},
		{name: "exponent past the bound", attrs: `{"n":1e-401}`, wantStatus: http.StatusBadRequest},
		// Each 1e-400 is charged its raw bytes plus 400. 161 of them fit in
		// 64 KB, 162 do not.
		{name: "printed length just under the cap", attrs: `{"n":` + numberArray("1e-400", 161) + `}`, wantStatus: http.StatusAccepted},
		{name: "printed length just over the cap", attrs: `{"n":` + numberArray("1e-400", 162) + `}`, wantStatus: http.StatusBadRequest},
		{name: "raw size under the cap but printed far over", attrs: numberArray("1e400", 10922), wantStatus: http.StatusBadRequest},
		{name: "raw size over the cap", attrs: numberArray("1", 32769), wantStatus: http.StatusBadRequest},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			body := `{"logs":[{"timestamp":"` + ts + `","level":"INFO","msg":"m","service":"api","host":"web01","attrs":` + tt.attrs + `}]}`
			h := NewAppLogIngestHandler(&mockAppLogStore{}, broker.NewAppLogBroker(slog.Default()), slog.Default(), nil)

			req := httptest.NewRequestWithContext(context.Background(), http.MethodPost, "/api/v1/applog/ingest", bytes.NewBufferString(body))
			rec := httptest.NewRecorder()
			h.Ingest(rec, req)

			if rec.Code != tt.wantStatus {
				t.Fatalf("got status %d, want %d; body: %.300s", rec.Code, tt.wantStatus, rec.Body.String())
			}
			if tt.wantStatus == http.StatusBadRequest {
				var got httputil.ErrorBody
				if err := json.Unmarshal(rec.Body.Bytes(), &got); err != nil {
					t.Fatalf("decode error body: %v", err)
				}
				if got.Error.Code != "validation_failed" {
					t.Errorf("error code = %q, want validation_failed", got.Error.Code)
				}
			}
		})
	}
}

// capturingAppLogStore records the events handed to InsertLogBatch.
type capturingAppLogStore struct {
	mockAppLogStore
	got []model.AppLogEvent
}

func (s *capturingAppLogStore) InsertLogBatch(ctx context.Context, events []model.AppLogEvent) ([]model.AppLogEvent, error) {
	s.got = events
	return s.mockAppLogStore.InsertLogBatch(ctx, events)
}

func TestIngestReplacesNULBytes(t *testing.T) {
	ts := time.Now().UTC().Format(time.RFC3339)
	body := `{"logs":[{"timestamp":"` + ts + `","level":"INFO","msg":"a\u0000b","service":"s\u0000","component":"c\u0000",` +
		`"host":"h\u0000","source":"f\u0000","attrs":{"k\u0000":"v\u0000","path":"C:\\u0000"}}]}`

	store := &capturingAppLogStore{}
	h := NewAppLogIngestHandler(store, broker.NewAppLogBroker(slog.Default()), slog.Default(), nil)

	req := httptest.NewRequestWithContext(context.Background(), http.MethodPost, "/api/v1/applog/ingest", bytes.NewBufferString(body))
	rec := httptest.NewRecorder()
	h.Ingest(rec, req)

	if rec.Code != http.StatusAccepted {
		t.Fatalf("got status %d, want %d; body: %s", rec.Code, http.StatusAccepted, rec.Body.String())
	}
	if len(store.got) != 1 {
		t.Fatalf("store got %d events, want 1", len(store.got))
	}

	e := store.got[0]
	for name, got := range map[string]string{"msg": e.Msg, "service": e.Service, "component": e.Component, "host": e.Host, "source": e.Source} {
		if strings.ContainsRune(got, 0) {
			t.Errorf("%s = %q still holds a NUL byte", name, got)
		}
	}
	if e.Msg != "a\uFFFDb" {
		t.Errorf("msg = %q, want a\\uFFFDb", e.Msg)
	}

	var attrs map[string]string
	if err := json.Unmarshal(e.Attrs, &attrs); err != nil {
		t.Fatalf("decode attrs %s: %v", e.Attrs, err)
	}
	if attrs["k\uFFFD"] != "v\uFFFD" {
		t.Errorf("attrs = %v, want the NUL in key and value replaced", attrs)
	}
	if attrs["path"] != `C:\u0000` {
		t.Errorf("attrs path = %q, an escaped backslash must not be read as a NUL", attrs["path"])
	}
}

func TestIngestMultipleEntries(t *testing.T) {
	now := time.Now().UTC().Truncate(time.Second)
	ts := now.Format(time.RFC3339)
	body := `{"logs":[
		{"timestamp":"` + ts + `","level":"INFO","msg":"first","service":"api","host":"web01"},
		{"timestamp":"` + ts + `","level":"ERROR","msg":"second","service":"api","host":"web02"}
	]}`

	store := &mockAppLogStore{
		inserted: []model.AppLogEvent{
			{ID: 1, Level: "INFO", Msg: "first", Service: "api", Host: "web01", ReceivedAt: now},
			{ID: 2, Level: "ERROR", Msg: "second", Service: "api", Host: "web02", ReceivedAt: now},
		},
	}
	b := broker.NewAppLogBroker(slog.Default())
	h := NewAppLogIngestHandler(store, b, slog.Default(), nil)

	req := httptest.NewRequestWithContext(context.Background(), http.MethodPost, "/api/v1/applog/ingest", bytes.NewBufferString(body))
	req.Header.Set("Content-Type", "application/json")
	rec := httptest.NewRecorder()

	h.Ingest(rec, req)

	if rec.Code != http.StatusAccepted {
		t.Fatalf("got status %d, want %d; body: %s", rec.Code, http.StatusAccepted, rec.Body.String())
	}

	var resp map[string]int
	if err := json.NewDecoder(rec.Body).Decode(&resp); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if resp["accepted"] != 2 {
		t.Errorf("expected accepted=2, got %d", resp["accepted"])
	}
}
