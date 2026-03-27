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

// jsonNull is the JSON null literal, used across tests to verify nil slices
// serialize as [] (via emptySlice) rather than null.
const jsonNull = "null"

// mockSrvlogStore implements SrvlogStore for testing.
type mockSrvlogStore struct {
	events  []model.SrvlogEvent
	event   model.SrvlogEvent
	listErr error
	getErr  error
}

func (m *mockSrvlogStore) GetSrvlog(_ context.Context, _ int64) (model.SrvlogEvent, error) {
	return m.event, m.getErr
}

func (m *mockSrvlogStore) ListSrvlogs(_ context.Context, _ model.SrvlogFilter, _ *model.Cursor, _ int) ([]model.SrvlogEvent, *model.Cursor, error) {
	return m.events, nil, m.listErr
}

func (m *mockSrvlogStore) ListSrvlogsSince(_ context.Context, _ model.SrvlogFilter, _ int64, _ int) ([]model.SrvlogEvent, error) {
	return m.events, m.listErr
}

func (m *mockSrvlogStore) ListSrvlogHosts(_ context.Context) ([]string, error) {
	return nil, nil
}

func (m *mockSrvlogStore) ListSrvlogPrograms(_ context.Context) ([]string, error) {
	return nil, nil
}

func (m *mockSrvlogStore) ListSrvlogTags(_ context.Context) ([]string, error) {
	return nil, nil
}

func (m *mockSrvlogStore) ListSrvlogFacilities(_ context.Context) ([]int, error) {
	return nil, nil
}

func (m *mockSrvlogStore) GetSrvlogDeviceSummary(_ context.Context, _ string) (model.SrvlogDeviceSummary, error) {
	return model.SrvlogDeviceSummary{}, nil
}

func TestSrvlogList(t *testing.T) {
	tests := []struct {
		name       string
		store      *mockSrvlogStore
		wantStatus int
		wantEmpty  bool // expect data to be []
	}{
		{
			name:       "success with events",
			store:      &mockSrvlogStore{events: []model.SrvlogEvent{{ID: 1, Hostname: "web01"}}},
			wantStatus: http.StatusOK,
		},
		{
			name:       "success empty returns empty array not null",
			store:      &mockSrvlogStore{events: nil},
			wantStatus: http.StatusOK,
			wantEmpty:  true,
		},
		{
			name:       "store error returns 500",
			store:      &mockSrvlogStore{listErr: errors.New("db down")},
			wantStatus: http.StatusInternalServerError,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			h := NewSrvlogHandler(tt.store)
			req := httptest.NewRequestWithContext(context.Background(), http.MethodGet, "/api/v1/srvlog", nil)
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

func TestSrvlogGet(t *testing.T) {
	tests := []struct {
		name       string
		id         string
		store      *mockSrvlogStore
		wantStatus int
	}{
		{
			name: "success",
			id:   "42",
			store: &mockSrvlogStore{event: model.SrvlogEvent{
				ID: 42, Hostname: "web01", ReceivedAt: time.Now(),
			}},
			wantStatus: http.StatusOK,
		},
		{
			name:       "invalid id",
			id:         "abc",
			store:      &mockSrvlogStore{},
			wantStatus: http.StatusBadRequest,
		},
		{
			name:       "not found",
			id:         "999",
			store:      &mockSrvlogStore{getErr: pgx.ErrNoRows},
			wantStatus: http.StatusNotFound,
		},
		{
			name:       "store error",
			id:         "42",
			store:      &mockSrvlogStore{getErr: errors.New("db error")},
			wantStatus: http.StatusInternalServerError,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			h := NewSrvlogHandler(tt.store)

			r := chi.NewRouter()
			r.Get("/srvlog/{id}", h.Get)

			req := httptest.NewRequestWithContext(context.Background(), http.MethodGet, "/srvlog/"+tt.id, nil)
			rec := httptest.NewRecorder()

			r.ServeHTTP(rec, req)

			if rec.Code != tt.wantStatus {
				t.Errorf("got status %d, want %d; body: %s", rec.Code, tt.wantStatus, rec.Body.String())
			}
		})
	}
}
