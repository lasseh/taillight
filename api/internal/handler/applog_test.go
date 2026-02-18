package handler

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/jackc/pgx/v5"

	"github.com/lasseh/taillight/internal/model"
)

// mockAppLogStore implements AppLogStore for testing.
type mockAppLogStore struct {
	events    []model.AppLogEvent
	event     model.AppLogEvent
	listErr   error
	getErr    error
	inserted  []model.AppLogEvent
	insertErr error
}

func (m *mockAppLogStore) GetAppLog(_ context.Context, _ int64) (model.AppLogEvent, error) {
	return m.event, m.getErr
}

func (m *mockAppLogStore) ListAppLogs(_ context.Context, _ model.AppLogFilter, _ *model.Cursor, _ int) ([]model.AppLogEvent, *model.Cursor, error) {
	return m.events, nil, m.listErr
}

func (m *mockAppLogStore) ListAppLogsSince(_ context.Context, _ model.AppLogFilter, _ int64, _ int) ([]model.AppLogEvent, error) {
	return m.events, m.listErr
}

func (m *mockAppLogStore) ListServices(_ context.Context) ([]string, error) {
	return nil, nil
}

func (m *mockAppLogStore) ListComponents(_ context.Context) ([]string, error) {
	return nil, nil
}

func (m *mockAppLogStore) ListAppLogHosts(_ context.Context) ([]string, error) {
	return nil, nil
}

func (m *mockAppLogStore) GetAppLogVolume(_ context.Context, _ model.VolumeInterval, _ time.Duration) ([]model.VolumeBucket, error) {
	return nil, nil
}

func (m *mockAppLogStore) InsertLogBatch(_ context.Context, events []model.AppLogEvent) ([]model.AppLogEvent, error) {
	if m.insertErr != nil {
		return nil, m.insertErr
	}
	if m.inserted != nil {
		return m.inserted, nil
	}
	// Return events as-is with IDs set.
	result := make([]model.AppLogEvent, len(events))
	for i, e := range events {
		e.ID = int64(i + 1)
		e.ReceivedAt = time.Now()
		result[i] = e
	}
	return result, nil
}

func TestAppLogList(t *testing.T) {
	tests := []struct {
		name       string
		store      *mockAppLogStore
		wantStatus int
		wantEmpty  bool
	}{
		{
			name:       "success with events",
			store:      &mockAppLogStore{events: []model.AppLogEvent{{ID: 1, Service: "api"}}},
			wantStatus: http.StatusOK,
		},
		{
			name:       "success empty returns empty array not null",
			store:      &mockAppLogStore{events: nil},
			wantStatus: http.StatusOK,
			wantEmpty:  true,
		},
		{
			name:       "store error returns 500",
			store:      &mockAppLogStore{listErr: errors.New("db down")},
			wantStatus: http.StatusInternalServerError,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			h := NewAppLogHandler(tt.store)
			req := httptest.NewRequest(http.MethodGet, "/api/v1/applog", nil)
			rec := httptest.NewRecorder()

			h.List(rec, req)

			if rec.Code != tt.wantStatus {
				t.Errorf("got status %d, want %d; body: %s", rec.Code, tt.wantStatus, rec.Body.String())
			}

			if tt.wantStatus == http.StatusOK && tt.wantEmpty {
				var resp struct {
					Data json.RawMessage `json:"data"`
				}
				if err := json.NewDecoder(rec.Body).Decode(&resp); err != nil {
					t.Fatalf("decode response: %v", err)
				}
				if string(resp.Data) == jsonNull {
					t.Error("data should be [] not null")
				}
				if string(resp.Data) != "[]" {
					t.Errorf("expected empty array, got %s", string(resp.Data))
				}
			}
		})
	}
}

func TestAppLogGet(t *testing.T) {
	tests := []struct {
		name       string
		id         string
		store      *mockAppLogStore
		wantStatus int
	}{
		{
			name: "success",
			id:   "42",
			store: &mockAppLogStore{event: model.AppLogEvent{
				ID: 42, Service: "api", ReceivedAt: time.Now(),
			}},
			wantStatus: http.StatusOK,
		},
		{
			name:       "invalid id",
			id:         "abc",
			store:      &mockAppLogStore{},
			wantStatus: http.StatusBadRequest,
		},
		{
			name:       "not found",
			id:         "999",
			store:      &mockAppLogStore{getErr: pgx.ErrNoRows},
			wantStatus: http.StatusNotFound,
		},
		{
			name:       "store error",
			id:         "42",
			store:      &mockAppLogStore{getErr: errors.New("db error")},
			wantStatus: http.StatusInternalServerError,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			h := NewAppLogHandler(tt.store)

			r := chi.NewRouter()
			r.Get("/applog/{id}", h.Get)

			req := httptest.NewRequest(http.MethodGet, "/applog/"+tt.id, nil)
			rec := httptest.NewRecorder()

			r.ServeHTTP(rec, req)

			if rec.Code != tt.wantStatus {
				t.Errorf("got status %d, want %d; body: %s", rec.Code, tt.wantStatus, rec.Body.String())
			}
		})
	}
}
