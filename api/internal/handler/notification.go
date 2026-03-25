package handler

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"strconv"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/jackc/pgx/v5"

	"github.com/lasseh/taillight/internal/notification"
)

// NotificationStore defines the data access interface for notification CRUD.
type NotificationStore interface {
	ListNotificationChannels(ctx context.Context) ([]notification.Channel, error)
	GetNotificationChannel(ctx context.Context, id int64) (notification.Channel, error)
	CreateNotificationChannel(ctx context.Context, ch notification.Channel) (notification.Channel, error)
	UpdateNotificationChannel(ctx context.Context, id int64, ch notification.Channel) (notification.Channel, error)
	DeleteNotificationChannel(ctx context.Context, id int64) error
	ListNotificationRules(ctx context.Context) ([]notification.Rule, error)
	GetNotificationRule(ctx context.Context, id int64) (notification.Rule, error)
	CreateNotificationRule(ctx context.Context, r notification.Rule) (notification.Rule, error)
	UpdateNotificationRule(ctx context.Context, id int64, r notification.Rule) (notification.Rule, error)
	DeleteNotificationRule(ctx context.Context, id int64) error
	ListNotificationLog(ctx context.Context, f notification.LogFilter) ([]notification.LogEntry, error)
}

// NotificationHandler handles REST endpoints for notification management.
type NotificationHandler struct {
	store  NotificationStore
	engine *notification.Engine
}

// NewNotificationHandler creates a new NotificationHandler.
func NewNotificationHandler(store NotificationStore, engine *notification.Engine) *NotificationHandler {
	return &NotificationHandler{store: store, engine: engine}
}

// --- Channels ---

// ListChannels handles GET /api/v1/notifications/channels.
func (h *NotificationHandler) ListChannels(w http.ResponseWriter, r *http.Request) {
	channels, err := h.store.ListNotificationChannels(r.Context())
	if err != nil {
		LoggerFromContext(r.Context()).Error("list notification channels", "err", err)
		writeError(w, http.StatusInternalServerError, "internal_error", "failed to list channels")
		return
	}
	writeJSON(w, itemResponse{Data: emptySlice(channels)})
}

// GetChannel handles GET /api/v1/notifications/channels/{id}.
func (h *NotificationHandler) GetChannel(w http.ResponseWriter, r *http.Request) {
	id, err := strconv.ParseInt(chi.URLParam(r, "id"), 10, 64)
	if err != nil {
		writeError(w, http.StatusBadRequest, "invalid_id", "id must be an integer")
		return
	}

	ch, err := h.store.GetNotificationChannel(r.Context(), id)
	if errors.Is(err, pgx.ErrNoRows) {
		writeError(w, http.StatusNotFound, "not_found", "channel not found")
		return
	}
	if err != nil {
		LoggerFromContext(r.Context()).Error("get notification channel", "id", id, "err", err)
		writeError(w, http.StatusInternalServerError, "internal_error", "failed to get channel")
		return
	}
	writeJSON(w, itemResponse{Data: ch})
}

// CreateChannel handles POST /api/v1/notifications/channels.
func (h *NotificationHandler) CreateChannel(w http.ResponseWriter, r *http.Request) {
	body, err := io.ReadAll(io.LimitReader(r.Body, 64*1024))
	if err != nil {
		writeError(w, http.StatusBadRequest, "read_error", "failed to read request body")
		return
	}

	var ch notification.Channel
	if err := json.Unmarshal(body, &ch); err != nil {
		writeError(w, http.StatusBadRequest, "invalid_json", "malformed JSON body")
		return
	}

	if ch.Name == "" {
		writeError(w, http.StatusBadRequest, "validation_failed", "name is required")
		return
	}
	if ch.Type == "" {
		writeError(w, http.StatusBadRequest, "validation_failed", "type is required")
		return
	}

	// Validate config against backend.
	if h.engine != nil {
		if err := h.engine.ValidateChannel(ch); err != nil {
			writeError(w, http.StatusBadRequest, "validation_failed", err.Error())
			return
		}
	}

	created, err := h.store.CreateNotificationChannel(r.Context(), ch)
	if err != nil {
		LoggerFromContext(r.Context()).Error("create notification channel", "err", err)
		writeError(w, http.StatusInternalServerError, "internal_error", "failed to create channel")
		return
	}

	writeJSONStatus(w, http.StatusCreated, itemResponse{Data: created})
}

// UpdateChannel handles PUT /api/v1/notifications/channels/{id}.
func (h *NotificationHandler) UpdateChannel(w http.ResponseWriter, r *http.Request) {
	id, err := strconv.ParseInt(chi.URLParam(r, "id"), 10, 64)
	if err != nil {
		writeError(w, http.StatusBadRequest, "invalid_id", "id must be an integer")
		return
	}

	body, err := io.ReadAll(io.LimitReader(r.Body, 64*1024))
	if err != nil {
		writeError(w, http.StatusBadRequest, "read_error", "failed to read request body")
		return
	}

	var ch notification.Channel
	if err := json.Unmarshal(body, &ch); err != nil {
		writeError(w, http.StatusBadRequest, "invalid_json", "malformed JSON body")
		return
	}

	if h.engine != nil {
		if err := h.engine.ValidateChannel(ch); err != nil {
			writeError(w, http.StatusBadRequest, "validation_failed", err.Error())
			return
		}
	}

	updated, err := h.store.UpdateNotificationChannel(r.Context(), id, ch)
	if errors.Is(err, pgx.ErrNoRows) {
		writeError(w, http.StatusNotFound, "not_found", "channel not found")
		return
	}
	if err != nil {
		LoggerFromContext(r.Context()).Error("update notification channel", "id", id, "err", err)
		writeError(w, http.StatusInternalServerError, "internal_error", "failed to update channel")
		return
	}
	writeJSON(w, itemResponse{Data: updated})
}

// DeleteChannel handles DELETE /api/v1/notifications/channels/{id}.
func (h *NotificationHandler) DeleteChannel(w http.ResponseWriter, r *http.Request) {
	id, err := strconv.ParseInt(chi.URLParam(r, "id"), 10, 64)
	if err != nil {
		writeError(w, http.StatusBadRequest, "invalid_id", "id must be an integer")
		return
	}

	err = h.store.DeleteNotificationChannel(r.Context(), id)
	if errors.Is(err, pgx.ErrNoRows) {
		writeError(w, http.StatusNotFound, "not_found", "channel not found")
		return
	}
	if err != nil {
		LoggerFromContext(r.Context()).Error("delete notification channel", "id", id, "err", err)
		writeError(w, http.StatusInternalServerError, "internal_error", "failed to delete channel")
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

// TestChannel handles POST /api/v1/notifications/channels/{id}/test.
func (h *NotificationHandler) TestChannel(w http.ResponseWriter, r *http.Request) {
	id, err := strconv.ParseInt(chi.URLParam(r, "id"), 10, 64)
	if err != nil {
		writeError(w, http.StatusBadRequest, "invalid_id", "id must be an integer")
		return
	}

	ch, err := h.store.GetNotificationChannel(r.Context(), id)
	if errors.Is(err, pgx.ErrNoRows) {
		writeError(w, http.StatusNotFound, "not_found", "channel not found")
		return
	}
	if err != nil {
		LoggerFromContext(r.Context()).Error("get notification channel for test", "id", id, "err", err)
		writeError(w, http.StatusInternalServerError, "internal_error", "failed to get channel")
		return
	}

	if h.engine == nil {
		writeError(w, http.StatusServiceUnavailable, "engine_disabled", "notification engine is not enabled")
		return
	}

	result, err := h.engine.SendTestNotification(r.Context(), ch)
	if err != nil {
		if isClientGone(r) {
			return
		}
		LoggerFromContext(r.Context()).Error("test notification setup failed", "channel_id", id, "err", err)
		writeError(w, http.StatusBadRequest, "test_failed", "failed to send test notification")
		return
	}

	if !result.Success {
		// Log the raw backend error for debugging, but return a sanitized
		// message to the client to avoid leaking webhook URLs, SMTP hosts, etc.
		if result.Error != nil {
			LoggerFromContext(r.Context()).Warn("test notification delivery failed",
				"channel_id", id,
				"status_code", result.StatusCode,
				"err", result.Error,
			)
		}
		writeJSON(w, map[string]any{
			"success":     false,
			"status_code": result.StatusCode,
			"error":       "delivery failed",
			"duration_ms": result.Duration.Milliseconds(),
		})
		return
	}

	writeJSON(w, map[string]any{
		"success":     true,
		"status_code": result.StatusCode,
		"duration_ms": result.Duration.Milliseconds(),
	})
}

// --- Rules ---

// ListRules handles GET /api/v1/notifications/rules.
func (h *NotificationHandler) ListRules(w http.ResponseWriter, r *http.Request) {
	rules, err := h.store.ListNotificationRules(r.Context())
	if err != nil {
		LoggerFromContext(r.Context()).Error("list notification rules", "err", err)
		writeError(w, http.StatusInternalServerError, "internal_error", "failed to list rules")
		return
	}
	writeJSON(w, itemResponse{Data: emptySlice(rules)})
}

// GetRule handles GET /api/v1/notifications/rules/{id}.
func (h *NotificationHandler) GetRule(w http.ResponseWriter, r *http.Request) {
	id, err := strconv.ParseInt(chi.URLParam(r, "id"), 10, 64)
	if err != nil {
		writeError(w, http.StatusBadRequest, "invalid_id", "id must be an integer")
		return
	}

	rule, err := h.store.GetNotificationRule(r.Context(), id)
	if errors.Is(err, pgx.ErrNoRows) {
		writeError(w, http.StatusNotFound, "not_found", "rule not found")
		return
	}
	if err != nil {
		LoggerFromContext(r.Context()).Error("get notification rule", "id", id, "err", err)
		writeError(w, http.StatusInternalServerError, "internal_error", "failed to get rule")
		return
	}
	writeJSON(w, itemResponse{Data: rule})
}

// CreateRule handles POST /api/v1/notifications/rules.
func (h *NotificationHandler) CreateRule(w http.ResponseWriter, r *http.Request) {
	body, err := io.ReadAll(io.LimitReader(r.Body, 64*1024))
	if err != nil {
		writeError(w, http.StatusBadRequest, "read_error", "failed to read request body")
		return
	}

	var rule notification.Rule
	if err := json.Unmarshal(body, &rule); err != nil {
		writeError(w, http.StatusBadRequest, "invalid_json", "malformed JSON body")
		return
	}

	if rule.Name == "" {
		writeError(w, http.StatusBadRequest, "validation_failed", "name is required")
		return
	}
	if rule.EventKind == "" {
		writeError(w, http.StatusBadRequest, "validation_failed", "event_kind is required (syslog or applog)")
		return
	}
	if rule.EventKind != notification.EventKindSyslog && rule.EventKind != notification.EventKindAppLog {
		writeError(w, http.StatusBadRequest, "validation_failed", "event_kind must be syslog or applog")
		return
	}

	created, err := h.store.CreateNotificationRule(r.Context(), rule)
	if err != nil {
		LoggerFromContext(r.Context()).Error("create notification rule", "err", err)
		writeError(w, http.StatusInternalServerError, "internal_error", "failed to create rule")
		return
	}

	writeJSONStatus(w, http.StatusCreated, itemResponse{Data: created})
}

// UpdateRule handles PUT /api/v1/notifications/rules/{id}.
func (h *NotificationHandler) UpdateRule(w http.ResponseWriter, r *http.Request) {
	id, err := strconv.ParseInt(chi.URLParam(r, "id"), 10, 64)
	if err != nil {
		writeError(w, http.StatusBadRequest, "invalid_id", "id must be an integer")
		return
	}

	body, err := io.ReadAll(io.LimitReader(r.Body, 64*1024))
	if err != nil {
		writeError(w, http.StatusBadRequest, "read_error", "failed to read request body")
		return
	}

	var rule notification.Rule
	if err := json.Unmarshal(body, &rule); err != nil {
		writeError(w, http.StatusBadRequest, "invalid_json", "malformed JSON body")
		return
	}

	updated, err := h.store.UpdateNotificationRule(r.Context(), id, rule)
	if errors.Is(err, pgx.ErrNoRows) {
		writeError(w, http.StatusNotFound, "not_found", "rule not found")
		return
	}
	if err != nil {
		LoggerFromContext(r.Context()).Error("update notification rule", "id", id, "err", err)
		writeError(w, http.StatusInternalServerError, "internal_error", "failed to update rule")
		return
	}
	writeJSON(w, itemResponse{Data: updated})
}

// DeleteRule handles DELETE /api/v1/notifications/rules/{id}.
func (h *NotificationHandler) DeleteRule(w http.ResponseWriter, r *http.Request) {
	id, err := strconv.ParseInt(chi.URLParam(r, "id"), 10, 64)
	if err != nil {
		writeError(w, http.StatusBadRequest, "invalid_id", "id must be an integer")
		return
	}

	err = h.store.DeleteNotificationRule(r.Context(), id)
	if errors.Is(err, pgx.ErrNoRows) {
		writeError(w, http.StatusNotFound, "not_found", "rule not found")
		return
	}
	if err != nil {
		LoggerFromContext(r.Context()).Error("delete notification rule", "id", id, "err", err)
		writeError(w, http.StatusInternalServerError, "internal_error", "failed to delete rule")
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

// --- Log ---

// ListLog handles GET /api/v1/notifications/log.
func (h *NotificationHandler) ListLog(w http.ResponseWriter, r *http.Request) {
	q := r.URL.Query()
	var f notification.LogFilter

	if v := q.Get("rule_id"); v != "" {
		id, err := strconv.ParseInt(v, 10, 64)
		if err != nil {
			writeError(w, http.StatusBadRequest, "invalid_param", "rule_id must be an integer")
			return
		}
		f.RuleID = &id
	}
	if v := q.Get("channel_id"); v != "" {
		id, err := strconv.ParseInt(v, 10, 64)
		if err != nil {
			writeError(w, http.StatusBadRequest, "invalid_param", "channel_id must be an integer")
			return
		}
		f.ChannelID = &id
	}
	f.Status = q.Get("status")
	if v := q.Get("from"); v != "" {
		t, err := time.Parse(time.RFC3339, v)
		if err != nil {
			writeError(w, http.StatusBadRequest, "invalid_param", "from must be RFC3339 format")
			return
		}
		f.From = &t
	}
	if v := q.Get("to"); v != "" {
		t, err := time.Parse(time.RFC3339, v)
		if err != nil {
			writeError(w, http.StatusBadRequest, "invalid_param", "to must be RFC3339 format")
			return
		}
		f.To = &t
	}

	entries, err := h.store.ListNotificationLog(r.Context(), f)
	if err != nil {
		LoggerFromContext(r.Context()).Error("list notification log", "err", err)
		writeError(w, http.StatusInternalServerError, "internal_error", "failed to list notification log")
		return
	}
	writeJSON(w, itemResponse{Data: emptySlice(entries)})
}
