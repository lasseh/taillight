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
	"github.com/lasseh/taillight/internal/scheduler"
)

// SummaryStore defines the data access interface for summary schedule CRUD.
type SummaryStore interface {
	ListSummarySchedules(ctx context.Context) ([]notification.SummarySchedule, error)
	GetSummarySchedule(ctx context.Context, id int64) (notification.SummarySchedule, error)
	CreateSummarySchedule(ctx context.Context, s notification.SummarySchedule) (notification.SummarySchedule, error)
	UpdateSummarySchedule(ctx context.Context, id int64, s notification.SummarySchedule) (notification.SummarySchedule, error)
	DeleteSummarySchedule(ctx context.Context, id int64) error
}

// SummaryHandler handles REST endpoints for summary schedule management.
type SummaryHandler struct {
	store     SummaryStore
	scheduler *scheduler.SummaryScheduler
}

// NewSummaryHandler creates a new SummaryHandler.
func NewSummaryHandler(store SummaryStore, sched *scheduler.SummaryScheduler) *SummaryHandler {
	return &SummaryHandler{store: store, scheduler: sched}
}

// ListSchedules handles GET /api/v1/notifications/summaries.
func (h *SummaryHandler) ListSchedules(w http.ResponseWriter, r *http.Request) {
	schedules, err := h.store.ListSummarySchedules(r.Context())
	if err != nil {
		LoggerFromContext(r.Context()).Error("list summary schedules", "err", err)
		writeError(w, http.StatusInternalServerError, "internal_error", "failed to list summary schedules")
		return
	}
	writeJSON(w, itemResponse{Data: emptySlice(schedules)})
}

// GetSchedule handles GET /api/v1/notifications/summaries/{id}.
func (h *SummaryHandler) GetSchedule(w http.ResponseWriter, r *http.Request) {
	id, err := strconv.ParseInt(chi.URLParam(r, "id"), 10, 64)
	if err != nil {
		writeError(w, http.StatusBadRequest, "invalid_id", "id must be an integer")
		return
	}

	sched, err := h.store.GetSummarySchedule(r.Context(), id)
	if errors.Is(err, pgx.ErrNoRows) {
		writeError(w, http.StatusNotFound, "not_found", "summary schedule not found")
		return
	}
	if err != nil {
		LoggerFromContext(r.Context()).Error("get summary schedule", "id", id, "err", err)
		writeError(w, http.StatusInternalServerError, "internal_error", "failed to get summary schedule")
		return
	}
	writeJSON(w, itemResponse{Data: sched})
}

// CreateSchedule handles POST /api/v1/notifications/summaries.
func (h *SummaryHandler) CreateSchedule(w http.ResponseWriter, r *http.Request) {
	body, err := io.ReadAll(io.LimitReader(r.Body, 64*1024))
	if err != nil {
		writeError(w, http.StatusBadRequest, "read_error", "failed to read request body")
		return
	}

	var ss notification.SummarySchedule
	if err := json.Unmarshal(body, &ss); err != nil {
		writeError(w, http.StatusBadRequest, "invalid_json", "malformed JSON body")
		return
	}

	if ss.Name == "" {
		writeError(w, http.StatusBadRequest, "validation_failed", "name is required")
		return
	}
	if ss.Frequency != "daily" && ss.Frequency != "weekly" && ss.Frequency != "monthly" {
		writeError(w, http.StatusBadRequest, "validation_failed", "frequency must be daily, weekly, or monthly")
		return
	}
	if len(ss.EventKinds) == 0 {
		writeError(w, http.StatusBadRequest, "validation_failed", "at least one event_kind is required")
		return
	}
	if len(ss.ChannelIDs) == 0 {
		writeError(w, http.StatusBadRequest, "validation_failed", "at least one channel is required")
		return
	}
	if ss.Timezone != "" {
		if _, err := time.LoadLocation(ss.Timezone); err != nil {
			writeError(w, http.StatusBadRequest, "validation_failed", "invalid timezone")
			return
		}
	} else {
		ss.Timezone = "UTC"
	}
	if ss.TopN <= 0 {
		ss.TopN = 25
	}

	created, err := h.store.CreateSummarySchedule(r.Context(), ss)
	if err != nil {
		LoggerFromContext(r.Context()).Error("create summary schedule", "err", err)
		writeError(w, http.StatusInternalServerError, "internal_error", "failed to create summary schedule")
		return
	}

	writeJSONStatus(w, http.StatusCreated, itemResponse{Data: created})
}

// UpdateSchedule handles PUT /api/v1/notifications/summaries/{id}.
func (h *SummaryHandler) UpdateSchedule(w http.ResponseWriter, r *http.Request) {
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

	var ss notification.SummarySchedule
	if err := json.Unmarshal(body, &ss); err != nil {
		writeError(w, http.StatusBadRequest, "invalid_json", "malformed JSON body")
		return
	}

	if ss.Timezone != "" {
		if _, err := time.LoadLocation(ss.Timezone); err != nil {
			writeError(w, http.StatusBadRequest, "validation_failed", "invalid timezone")
			return
		}
	}

	updated, err := h.store.UpdateSummarySchedule(r.Context(), id, ss)
	if errors.Is(err, pgx.ErrNoRows) {
		writeError(w, http.StatusNotFound, "not_found", "summary schedule not found")
		return
	}
	if err != nil {
		LoggerFromContext(r.Context()).Error("update summary schedule", "id", id, "err", err)
		writeError(w, http.StatusInternalServerError, "internal_error", "failed to update summary schedule")
		return
	}
	writeJSON(w, itemResponse{Data: updated})
}

// DeleteSchedule handles DELETE /api/v1/notifications/summaries/{id}.
func (h *SummaryHandler) DeleteSchedule(w http.ResponseWriter, r *http.Request) {
	id, err := strconv.ParseInt(chi.URLParam(r, "id"), 10, 64)
	if err != nil {
		writeError(w, http.StatusBadRequest, "invalid_id", "id must be an integer")
		return
	}

	err = h.store.DeleteSummarySchedule(r.Context(), id)
	if errors.Is(err, pgx.ErrNoRows) {
		writeError(w, http.StatusNotFound, "not_found", "summary schedule not found")
		return
	}
	if err != nil {
		LoggerFromContext(r.Context()).Error("delete summary schedule", "id", id, "err", err)
		writeError(w, http.StatusInternalServerError, "internal_error", "failed to delete summary schedule")
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

// TriggerSchedule handles POST /api/v1/notifications/summaries/{id}/trigger.
func (h *SummaryHandler) TriggerSchedule(w http.ResponseWriter, r *http.Request) {
	id, err := strconv.ParseInt(chi.URLParam(r, "id"), 10, 64)
	if err != nil {
		writeError(w, http.StatusBadRequest, "invalid_id", "id must be an integer")
		return
	}

	if h.scheduler == nil {
		writeError(w, http.StatusServiceUnavailable, "scheduler_disabled", "summary scheduler is not enabled")
		return
	}

	if err := h.scheduler.TriggerSchedule(r.Context(), id); err != nil {
		LoggerFromContext(r.Context()).Error("trigger summary schedule", "id", id, "err", err)
		writeError(w, http.StatusInternalServerError, "internal_error", "failed to trigger summary")
		return
	}

	writeJSON(w, map[string]any{"success": true})
}
