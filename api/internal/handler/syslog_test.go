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

// mockSyslogStore implements SyslogStore for testing.
type mockSyslogStore struct {
	events     []model.SyslogEvent
	event      model.SyslogEvent
	listErr    error
	getErr     error
	juniperRef []model.JuniperSyslogRef
	juniperErr error
}

func (m *mockSyslogStore) GetSyslog(_ context.Context, _ int64) (model.SyslogEvent, error) {
	return m.event, m.getErr
}

func (m *mockSyslogStore) ListSyslogs(_ context.Context, _ model.SyslogFilter, _ *model.Cursor, _ int) ([]model.SyslogEvent, *model.Cursor, error) {
	return m.events, nil, m.listErr
}

func (m *mockSyslogStore) ListSyslogsSince(_ context.Context, _ model.SyslogFilter, _ int64, _ int) ([]model.SyslogEvent, error) {
	return m.events, m.listErr
}

func (m *mockSyslogStore) ListHosts(_ context.Context) ([]string, error) {
	return nil, nil
}

func (m *mockSyslogStore) ListPrograms(_ context.Context) ([]string, error) {
	return nil, nil
}

func (m *mockSyslogStore) ListTags(_ context.Context) ([]string, error) {
	return nil, nil
}

func (m *mockSyslogStore) ListFacilities(_ context.Context) ([]int, error) {
	return nil, nil
}

func (m *mockSyslogStore) GetVolume(_ context.Context, _ model.VolumeInterval, _ time.Duration) ([]model.VolumeBucket, error) {
	return nil, nil
}

func (m *mockSyslogStore) LookupJuniperRef(_ context.Context, _ string) ([]model.JuniperSyslogRef, error) {
	return m.juniperRef, m.juniperErr
}

func TestSyslogList(t *testing.T) {
	tests := []struct {
		name       string
		store      *mockSyslogStore
		wantStatus int
		wantEmpty  bool // expect data to be []
	}{
		{
			name:       "success with events",
			store:      &mockSyslogStore{events: []model.SyslogEvent{{ID: 1, Hostname: "web01"}}},
			wantStatus: http.StatusOK,
		},
		{
			name:       "success empty returns empty array not null",
			store:      &mockSyslogStore{events: nil},
			wantStatus: http.StatusOK,
			wantEmpty:  true,
		},
		{
			name:       "store error returns 500",
			store:      &mockSyslogStore{listErr: errors.New("db down")},
			wantStatus: http.StatusInternalServerError,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			h := NewSyslogHandler(tt.store)
			req := httptest.NewRequestWithContext(context.Background(), http.MethodGet, "/api/v1/syslog", nil)
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

func TestSyslogGet(t *testing.T) {
	tests := []struct {
		name       string
		id         string
		store      *mockSyslogStore
		wantStatus int
	}{
		{
			name: "success",
			id:   "42",
			store: &mockSyslogStore{event: model.SyslogEvent{
				ID: 42, Hostname: "web01", ReceivedAt: time.Now(),
			}},
			wantStatus: http.StatusOK,
		},
		{
			name:       "invalid id",
			id:         "abc",
			store:      &mockSyslogStore{},
			wantStatus: http.StatusBadRequest,
		},
		{
			name:       "not found",
			id:         "999",
			store:      &mockSyslogStore{getErr: pgx.ErrNoRows},
			wantStatus: http.StatusNotFound,
		},
		{
			name:       "store error",
			id:         "42",
			store:      &mockSyslogStore{getErr: errors.New("db error")},
			wantStatus: http.StatusInternalServerError,
		},
		{
			name: "msgid triggers juniper lookup",
			id:   "42",
			store: &mockSyslogStore{
				event: model.SyslogEvent{
					ID: 42, Hostname: "router01", MsgID: "OSPF_NBR_UP",
					ReceivedAt: time.Now(),
				},
				juniperRef: []model.JuniperSyslogRef{
					{ID: 1, Name: "OSPF_NBR_UP", Description: "OSPF neighbor up"},
				},
			},
			wantStatus: http.StatusOK,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			h := NewSyslogHandler(tt.store)

			r := chi.NewRouter()
			r.Get("/syslog/{id}", h.Get)

			req := httptest.NewRequestWithContext(context.Background(), http.MethodGet, "/syslog/"+tt.id, nil)
			rec := httptest.NewRecorder()

			r.ServeHTTP(rec, req)

			if rec.Code != tt.wantStatus {
				t.Errorf("got status %d, want %d; body: %s", rec.Code, tt.wantStatus, rec.Body.String())
			}

			if tt.name == "msgid triggers juniper lookup" {
				var resp struct {
					Data struct {
						JuniperRef []model.JuniperSyslogRef `json:"juniper_ref"`
					} `json:"data"`
				}
				if err := json.NewDecoder(rec.Body).Decode(&resp); err != nil {
					t.Fatalf("decode: %v", err)
				}
				if len(resp.Data.JuniperRef) != 1 {
					t.Errorf("expected 1 juniper ref, got %d", len(resp.Data.JuniperRef))
				}
			}
		})
	}
}
