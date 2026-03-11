package handler

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/go-chi/chi/v5"
	"github.com/jackc/pgx/v5"

	"github.com/lasseh/taillight/internal/notification"
)

// mockNotificationStore implements NotificationStore for testing.
type mockNotificationStore struct {
	channels    []notification.Channel
	channel     notification.Channel
	listChErr   error
	getChErr    error
	createChErr error
	updateChErr error
	deleteChErr error

	rules       []notification.Rule
	rule        notification.Rule
	listRuErr   error
	getRuErr    error
	createRuErr error
	updateRuErr error
	deleteRuErr error

	logEntries []notification.LogEntry
	listLogErr error
}

func (m *mockNotificationStore) ListNotificationChannels(_ context.Context) ([]notification.Channel, error) {
	return m.channels, m.listChErr
}

func (m *mockNotificationStore) GetNotificationChannel(_ context.Context, _ int64) (notification.Channel, error) {
	return m.channel, m.getChErr
}

func (m *mockNotificationStore) CreateNotificationChannel(_ context.Context, ch notification.Channel) (notification.Channel, error) {
	ch.ID = 1
	return ch, m.createChErr
}

func (m *mockNotificationStore) UpdateNotificationChannel(_ context.Context, _ int64, ch notification.Channel) (notification.Channel, error) {
	return ch, m.updateChErr
}

func (m *mockNotificationStore) DeleteNotificationChannel(_ context.Context, _ int64) error {
	return m.deleteChErr
}

func (m *mockNotificationStore) ListNotificationRules(_ context.Context) ([]notification.Rule, error) {
	return m.rules, m.listRuErr
}

func (m *mockNotificationStore) GetNotificationRule(_ context.Context, _ int64) (notification.Rule, error) {
	return m.rule, m.getRuErr
}

func (m *mockNotificationStore) CreateNotificationRule(_ context.Context, r notification.Rule) (notification.Rule, error) {
	r.ID = 1
	return r, m.createRuErr
}

func (m *mockNotificationStore) UpdateNotificationRule(_ context.Context, _ int64, r notification.Rule) (notification.Rule, error) {
	return r, m.updateRuErr
}

func (m *mockNotificationStore) DeleteNotificationRule(_ context.Context, _ int64) error {
	return m.deleteRuErr
}

func (m *mockNotificationStore) ListNotificationLog(_ context.Context, _ notification.LogFilter) ([]notification.LogEntry, error) {
	return m.logEntries, m.listLogErr
}

// --- Channel Tests ---

func TestListChannels(t *testing.T) {
	tests := []struct {
		name       string
		store      *mockNotificationStore
		wantStatus int
	}{
		{
			name:       "success",
			store:      &mockNotificationStore{channels: []notification.Channel{{ID: 1, Name: "slack"}}},
			wantStatus: http.StatusOK,
		},
		{
			name:       "empty returns array",
			store:      &mockNotificationStore{},
			wantStatus: http.StatusOK,
		},
		{
			name:       "store error",
			store:      &mockNotificationStore{listChErr: errors.New("db error")},
			wantStatus: http.StatusInternalServerError,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			h := NewNotificationHandler(tt.store, nil)
			req := httptest.NewRequestWithContext(context.Background(), http.MethodGet, "/api/v1/notifications/channels", nil)
			rec := httptest.NewRecorder()

			h.ListChannels(rec, req)

			if rec.Code != tt.wantStatus {
				t.Errorf("got status %d, want %d; body: %s", rec.Code, tt.wantStatus, rec.Body.String())
			}
		})
	}
}

func TestGetChannel(t *testing.T) {
	tests := []struct {
		name       string
		id         string
		store      *mockNotificationStore
		wantStatus int
	}{
		{
			name:       "success",
			id:         "1",
			store:      &mockNotificationStore{channel: notification.Channel{ID: 1, Name: "slack"}},
			wantStatus: http.StatusOK,
		},
		{
			name:       "invalid id",
			id:         "abc",
			store:      &mockNotificationStore{},
			wantStatus: http.StatusBadRequest,
		},
		{
			name:       "not found",
			id:         "999",
			store:      &mockNotificationStore{getChErr: pgx.ErrNoRows},
			wantStatus: http.StatusNotFound,
		},
		{
			name:       "store error",
			id:         "1",
			store:      &mockNotificationStore{getChErr: errors.New("db error")},
			wantStatus: http.StatusInternalServerError,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			h := NewNotificationHandler(tt.store, nil)
			r := chi.NewRouter()
			r.Get("/channels/{id}", h.GetChannel)

			req := httptest.NewRequestWithContext(context.Background(), http.MethodGet, "/channels/"+tt.id, nil)
			rec := httptest.NewRecorder()
			r.ServeHTTP(rec, req)

			if rec.Code != tt.wantStatus {
				t.Errorf("got status %d, want %d; body: %s", rec.Code, tt.wantStatus, rec.Body.String())
			}
		})
	}
}

func TestCreateChannel(t *testing.T) {
	tests := []struct {
		name       string
		body       string
		store      *mockNotificationStore
		wantStatus int
	}{
		{
			name:       "success",
			body:       `{"name":"slack-alerts","type":"slack","config":{"webhook_url":"https://hooks.slack.com/test"},"enabled":true}`,
			store:      &mockNotificationStore{},
			wantStatus: http.StatusCreated,
		},
		{
			name:       "invalid json",
			body:       `{bad}`,
			store:      &mockNotificationStore{},
			wantStatus: http.StatusBadRequest,
		},
		{
			name:       "missing name",
			body:       `{"type":"slack","config":{},"enabled":true}`,
			store:      &mockNotificationStore{},
			wantStatus: http.StatusBadRequest,
		},
		{
			name:       "missing type",
			body:       `{"name":"slack-alerts","config":{},"enabled":true}`,
			store:      &mockNotificationStore{},
			wantStatus: http.StatusBadRequest,
		},
		{
			name:       "store error",
			body:       `{"name":"slack-alerts","type":"slack","config":{},"enabled":true}`,
			store:      &mockNotificationStore{createChErr: errors.New("db error")},
			wantStatus: http.StatusInternalServerError,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			h := NewNotificationHandler(tt.store, nil)
			req := httptest.NewRequestWithContext(context.Background(), http.MethodPost, "/api/v1/notifications/channels", bytes.NewBufferString(tt.body))
			req.Header.Set("Content-Type", "application/json")
			rec := httptest.NewRecorder()

			h.CreateChannel(rec, req)

			if rec.Code != tt.wantStatus {
				t.Errorf("got status %d, want %d; body: %s", rec.Code, tt.wantStatus, rec.Body.String())
			}
		})
	}
}

func TestUpdateChannel(t *testing.T) {
	tests := []struct {
		name       string
		id         string
		body       string
		store      *mockNotificationStore
		wantStatus int
	}{
		{
			name:       "success",
			id:         "1",
			body:       `{"name":"updated","type":"slack","config":{},"enabled":false}`,
			store:      &mockNotificationStore{},
			wantStatus: http.StatusOK,
		},
		{
			name:       "invalid id",
			id:         "abc",
			body:       `{"name":"test","type":"slack"}`,
			store:      &mockNotificationStore{},
			wantStatus: http.StatusBadRequest,
		},
		{
			name:       "invalid json",
			id:         "1",
			body:       `{bad}`,
			store:      &mockNotificationStore{},
			wantStatus: http.StatusBadRequest,
		},
		{
			name:       "not found",
			id:         "999",
			body:       `{"name":"test","type":"slack"}`,
			store:      &mockNotificationStore{updateChErr: pgx.ErrNoRows},
			wantStatus: http.StatusNotFound,
		},
		{
			name:       "store error",
			id:         "1",
			body:       `{"name":"test","type":"slack"}`,
			store:      &mockNotificationStore{updateChErr: errors.New("db error")},
			wantStatus: http.StatusInternalServerError,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			h := NewNotificationHandler(tt.store, nil)
			r := chi.NewRouter()
			r.Put("/channels/{id}", h.UpdateChannel)

			req := httptest.NewRequestWithContext(context.Background(), http.MethodPut, "/channels/"+tt.id, bytes.NewBufferString(tt.body))
			req.Header.Set("Content-Type", "application/json")
			rec := httptest.NewRecorder()
			r.ServeHTTP(rec, req)

			if rec.Code != tt.wantStatus {
				t.Errorf("got status %d, want %d; body: %s", rec.Code, tt.wantStatus, rec.Body.String())
			}
		})
	}
}

func TestDeleteChannel(t *testing.T) {
	tests := []struct {
		name       string
		id         string
		store      *mockNotificationStore
		wantStatus int
	}{
		{
			name:       "success",
			id:         "1",
			store:      &mockNotificationStore{},
			wantStatus: http.StatusNoContent,
		},
		{
			name:       "invalid id",
			id:         "abc",
			store:      &mockNotificationStore{},
			wantStatus: http.StatusBadRequest,
		},
		{
			name:       "not found",
			id:         "999",
			store:      &mockNotificationStore{deleteChErr: pgx.ErrNoRows},
			wantStatus: http.StatusNotFound,
		},
		{
			name:       "store error",
			id:         "1",
			store:      &mockNotificationStore{deleteChErr: errors.New("db error")},
			wantStatus: http.StatusInternalServerError,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			h := NewNotificationHandler(tt.store, nil)
			r := chi.NewRouter()
			r.Delete("/channels/{id}", h.DeleteChannel)

			req := httptest.NewRequestWithContext(context.Background(), http.MethodDelete, "/channels/"+tt.id, nil)
			rec := httptest.NewRecorder()
			r.ServeHTTP(rec, req)

			if rec.Code != tt.wantStatus {
				t.Errorf("got status %d, want %d; body: %s", rec.Code, tt.wantStatus, rec.Body.String())
			}
		})
	}
}

// --- Rule Tests ---

func TestListRules(t *testing.T) {
	tests := []struct {
		name       string
		store      *mockNotificationStore
		wantStatus int
	}{
		{
			name:       "success",
			store:      &mockNotificationStore{rules: []notification.Rule{{ID: 1, Name: "err-rule"}}},
			wantStatus: http.StatusOK,
		},
		{
			name:       "empty returns array",
			store:      &mockNotificationStore{},
			wantStatus: http.StatusOK,
		},
		{
			name:       "store error",
			store:      &mockNotificationStore{listRuErr: errors.New("db error")},
			wantStatus: http.StatusInternalServerError,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			h := NewNotificationHandler(tt.store, nil)
			req := httptest.NewRequestWithContext(context.Background(), http.MethodGet, "/api/v1/notifications/rules", nil)
			rec := httptest.NewRecorder()

			h.ListRules(rec, req)

			if rec.Code != tt.wantStatus {
				t.Errorf("got status %d, want %d; body: %s", rec.Code, tt.wantStatus, rec.Body.String())
			}
		})
	}
}

func TestGetRule(t *testing.T) {
	tests := []struct {
		name       string
		id         string
		store      *mockNotificationStore
		wantStatus int
	}{
		{
			name:       "success",
			id:         "1",
			store:      &mockNotificationStore{rule: notification.Rule{ID: 1, Name: "err-rule"}},
			wantStatus: http.StatusOK,
		},
		{
			name:       "invalid id",
			id:         "abc",
			store:      &mockNotificationStore{},
			wantStatus: http.StatusBadRequest,
		},
		{
			name:       "not found",
			id:         "999",
			store:      &mockNotificationStore{getRuErr: pgx.ErrNoRows},
			wantStatus: http.StatusNotFound,
		},
		{
			name:       "store error",
			id:         "1",
			store:      &mockNotificationStore{getRuErr: errors.New("db error")},
			wantStatus: http.StatusInternalServerError,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			h := NewNotificationHandler(tt.store, nil)
			r := chi.NewRouter()
			r.Get("/rules/{id}", h.GetRule)

			req := httptest.NewRequestWithContext(context.Background(), http.MethodGet, "/rules/"+tt.id, nil)
			rec := httptest.NewRecorder()
			r.ServeHTTP(rec, req)

			if rec.Code != tt.wantStatus {
				t.Errorf("got status %d, want %d; body: %s", rec.Code, tt.wantStatus, rec.Body.String())
			}
		})
	}
}

func TestCreateRule(t *testing.T) {
	tests := []struct {
		name       string
		body       string
		store      *mockNotificationStore
		wantStatus int
	}{
		{
			name:       "success syslog",
			body:       `{"name":"err-rule","event_kind":"syslog","enabled":true,"channel_ids":[1]}`,
			store:      &mockNotificationStore{},
			wantStatus: http.StatusCreated,
		},
		{
			name:       "success applog",
			body:       `{"name":"applog-rule","event_kind":"applog","enabled":true}`,
			store:      &mockNotificationStore{},
			wantStatus: http.StatusCreated,
		},
		{
			name:       "invalid json",
			body:       `{bad}`,
			store:      &mockNotificationStore{},
			wantStatus: http.StatusBadRequest,
		},
		{
			name:       "missing name",
			body:       `{"event_kind":"syslog"}`,
			store:      &mockNotificationStore{},
			wantStatus: http.StatusBadRequest,
		},
		{
			name:       "missing event_kind",
			body:       `{"name":"rule"}`,
			store:      &mockNotificationStore{},
			wantStatus: http.StatusBadRequest,
		},
		{
			name:       "invalid event_kind",
			body:       `{"name":"rule","event_kind":"invalid"}`,
			store:      &mockNotificationStore{},
			wantStatus: http.StatusBadRequest,
		},
		{
			name:       "store error",
			body:       `{"name":"err-rule","event_kind":"syslog"}`,
			store:      &mockNotificationStore{createRuErr: errors.New("db error")},
			wantStatus: http.StatusInternalServerError,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			h := NewNotificationHandler(tt.store, nil)
			req := httptest.NewRequestWithContext(context.Background(), http.MethodPost, "/api/v1/notifications/rules", bytes.NewBufferString(tt.body))
			req.Header.Set("Content-Type", "application/json")
			rec := httptest.NewRecorder()

			h.CreateRule(rec, req)

			if rec.Code != tt.wantStatus {
				t.Errorf("got status %d, want %d; body: %s", rec.Code, tt.wantStatus, rec.Body.String())
			}
		})
	}
}

func TestUpdateRule(t *testing.T) {
	tests := []struct {
		name       string
		id         string
		body       string
		store      *mockNotificationStore
		wantStatus int
	}{
		{
			name:       "success",
			id:         "1",
			body:       `{"name":"updated-rule","event_kind":"syslog","enabled":false}`,
			store:      &mockNotificationStore{},
			wantStatus: http.StatusOK,
		},
		{
			name:       "invalid id",
			id:         "abc",
			body:       `{"name":"test"}`,
			store:      &mockNotificationStore{},
			wantStatus: http.StatusBadRequest,
		},
		{
			name:       "invalid json",
			id:         "1",
			body:       `{bad}`,
			store:      &mockNotificationStore{},
			wantStatus: http.StatusBadRequest,
		},
		{
			name:       "not found",
			id:         "999",
			body:       `{"name":"test","event_kind":"syslog"}`,
			store:      &mockNotificationStore{updateRuErr: pgx.ErrNoRows},
			wantStatus: http.StatusNotFound,
		},
		{
			name:       "store error",
			id:         "1",
			body:       `{"name":"test","event_kind":"syslog"}`,
			store:      &mockNotificationStore{updateRuErr: errors.New("db error")},
			wantStatus: http.StatusInternalServerError,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			h := NewNotificationHandler(tt.store, nil)
			r := chi.NewRouter()
			r.Put("/rules/{id}", h.UpdateRule)

			req := httptest.NewRequestWithContext(context.Background(), http.MethodPut, "/rules/"+tt.id, bytes.NewBufferString(tt.body))
			req.Header.Set("Content-Type", "application/json")
			rec := httptest.NewRecorder()
			r.ServeHTTP(rec, req)

			if rec.Code != tt.wantStatus {
				t.Errorf("got status %d, want %d; body: %s", rec.Code, tt.wantStatus, rec.Body.String())
			}
		})
	}
}

func TestDeleteRule(t *testing.T) {
	tests := []struct {
		name       string
		id         string
		store      *mockNotificationStore
		wantStatus int
	}{
		{
			name:       "success",
			id:         "1",
			store:      &mockNotificationStore{},
			wantStatus: http.StatusNoContent,
		},
		{
			name:       "invalid id",
			id:         "abc",
			store:      &mockNotificationStore{},
			wantStatus: http.StatusBadRequest,
		},
		{
			name:       "not found",
			id:         "999",
			store:      &mockNotificationStore{deleteRuErr: pgx.ErrNoRows},
			wantStatus: http.StatusNotFound,
		},
		{
			name:       "store error",
			id:         "1",
			store:      &mockNotificationStore{deleteRuErr: errors.New("db error")},
			wantStatus: http.StatusInternalServerError,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			h := NewNotificationHandler(tt.store, nil)
			r := chi.NewRouter()
			r.Delete("/rules/{id}", h.DeleteRule)

			req := httptest.NewRequestWithContext(context.Background(), http.MethodDelete, "/rules/"+tt.id, nil)
			rec := httptest.NewRecorder()
			r.ServeHTTP(rec, req)

			if rec.Code != tt.wantStatus {
				t.Errorf("got status %d, want %d; body: %s", rec.Code, tt.wantStatus, rec.Body.String())
			}
		})
	}
}

// --- Log Tests ---

func TestListLog(t *testing.T) {
	tests := []struct {
		name       string
		query      string
		store      *mockNotificationStore
		wantStatus int
	}{
		{
			name:       "success no filters",
			query:      "",
			store:      &mockNotificationStore{logEntries: []notification.LogEntry{{ID: 1}}},
			wantStatus: http.StatusOK,
		},
		{
			name:       "with rule_id filter",
			query:      "?rule_id=1",
			store:      &mockNotificationStore{},
			wantStatus: http.StatusOK,
		},
		{
			name:       "with channel_id filter",
			query:      "?channel_id=2",
			store:      &mockNotificationStore{},
			wantStatus: http.StatusOK,
		},
		{
			name:       "with status filter",
			query:      "?status=sent",
			store:      &mockNotificationStore{},
			wantStatus: http.StatusOK,
		},
		{
			name:       "with from filter",
			query:      "?from=2025-01-01T00:00:00Z",
			store:      &mockNotificationStore{},
			wantStatus: http.StatusOK,
		},
		{
			name:       "invalid rule_id",
			query:      "?rule_id=abc",
			store:      &mockNotificationStore{},
			wantStatus: http.StatusBadRequest,
		},
		{
			name:       "invalid channel_id",
			query:      "?channel_id=abc",
			store:      &mockNotificationStore{},
			wantStatus: http.StatusBadRequest,
		},
		{
			name:       "invalid from format",
			query:      "?from=not-a-date",
			store:      &mockNotificationStore{},
			wantStatus: http.StatusBadRequest,
		},
		{
			name:       "invalid to format",
			query:      "?to=not-a-date",
			store:      &mockNotificationStore{},
			wantStatus: http.StatusBadRequest,
		},
		{
			name:       "store error",
			query:      "",
			store:      &mockNotificationStore{listLogErr: errors.New("db error")},
			wantStatus: http.StatusInternalServerError,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			h := NewNotificationHandler(tt.store, nil)
			req := httptest.NewRequestWithContext(context.Background(), http.MethodGet, "/api/v1/notifications/log"+tt.query, nil)
			rec := httptest.NewRecorder()

			h.ListLog(rec, req)

			if rec.Code != tt.wantStatus {
				t.Errorf("got status %d, want %d; body: %s", rec.Code, tt.wantStatus, rec.Body.String())
			}

			if tt.wantStatus == http.StatusOK {
				var resp struct {
					Data json.RawMessage `json:"data"`
				}
				if err := json.NewDecoder(rec.Body).Decode(&resp); err != nil {
					t.Fatalf("decode: %v", err)
				}
				if string(resp.Data) == jsonNull {
					t.Error("data should be [] not null")
				}
			}
		})
	}
}
