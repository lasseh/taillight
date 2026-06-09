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

	"github.com/lasseh/taillight/internal/model"
	"github.com/lasseh/taillight/internal/notification"
	"github.com/lasseh/taillight/internal/postgres"
	"github.com/lasseh/taillight/internal/scheduler"
	"github.com/lasseh/taillight/internal/worker"
)

// AnalysisScheduleStore is the persistence interface for the schedule handler.
type AnalysisScheduleStore interface {
	ListAnalysisSchedules(ctx context.Context) ([]model.AnalysisSchedule, error)
	GetAnalysisSchedule(ctx context.Context, id int64) (model.AnalysisSchedule, error)
	CreateAnalysisSchedule(ctx context.Context, s model.AnalysisSchedule) (model.AnalysisSchedule, error)
	UpdateAnalysisSchedule(ctx context.Context, id int64, s model.AnalysisSchedule) (model.AnalysisSchedule, error)
	DeleteAnalysisSchedule(ctx context.Context, id int64) error
	// ListNotificationChannels backs validation of notify_channel_ids: a
	// schedule may only target existing, email-type channels.
	ListNotificationChannels(ctx context.Context) ([]notification.Channel, error)
}

// AnalysisScheduleHandler exposes CRUD + run-now for recurring analysis schedules.
type AnalysisScheduleHandler struct {
	store         AnalysisScheduleStore
	scheduler     *scheduler.AnalysisScheduler
	netlogEnabled bool
}

// NewAnalysisScheduleHandler creates a new AnalysisScheduleHandler.
func NewAnalysisScheduleHandler(store AnalysisScheduleStore, sched *scheduler.AnalysisScheduler, netlogEnabled bool) *AnalysisScheduleHandler {
	return &AnalysisScheduleHandler{store: store, scheduler: sched, netlogEnabled: netlogEnabled}
}

// List handles GET /api/v1/analysis/schedules.
func (h *AnalysisScheduleHandler) List(w http.ResponseWriter, r *http.Request) {
	schedules, err := h.store.ListAnalysisSchedules(r.Context())
	if err != nil {
		LoggerFromContext(r.Context()).Error("list analysis schedules", "err", err)
		writeError(w, http.StatusInternalServerError, "internal_error", "failed to list analysis schedules")
		return
	}
	writeJSON(w, itemResponse{Data: emptySlice(schedules)})
}

// Get handles GET /api/v1/analysis/schedules/{id}.
func (h *AnalysisScheduleHandler) Get(w http.ResponseWriter, r *http.Request) {
	id, err := strconv.ParseInt(chi.URLParam(r, "id"), 10, 64)
	if err != nil {
		writeError(w, http.StatusBadRequest, "invalid_id", "id must be an integer")
		return
	}

	sched, err := h.store.GetAnalysisSchedule(r.Context(), id)
	if errors.Is(err, pgx.ErrNoRows) {
		writeError(w, http.StatusNotFound, "not_found", "analysis schedule not found")
		return
	}
	if err != nil {
		LoggerFromContext(r.Context()).Error("get analysis schedule", "id", id, "err", err)
		writeError(w, http.StatusInternalServerError, "internal_error", "failed to get analysis schedule")
		return
	}
	writeJSON(w, itemResponse{Data: sched})
}

// Create handles POST /api/v1/analysis/schedules.
func (h *AnalysisScheduleHandler) Create(w http.ResponseWriter, r *http.Request) {
	sched, ok := h.decodeAndValidateSchedule(w, r)
	if !ok {
		return
	}

	created, err := h.store.CreateAnalysisSchedule(r.Context(), sched)
	if err != nil {
		LoggerFromContext(r.Context()).Error("create analysis schedule", "err", err)
		writeError(w, http.StatusInternalServerError, "internal_error", "failed to create analysis schedule")
		return
	}
	writeJSONStatus(w, http.StatusCreated, itemResponse{Data: created})
}

// Update handles PUT /api/v1/analysis/schedules/{id}.
func (h *AnalysisScheduleHandler) Update(w http.ResponseWriter, r *http.Request) {
	id, err := strconv.ParseInt(chi.URLParam(r, "id"), 10, 64)
	if err != nil {
		writeError(w, http.StatusBadRequest, "invalid_id", "id must be an integer")
		return
	}

	sched, ok := h.decodeAndValidateSchedule(w, r)
	if !ok {
		return
	}

	updated, err := h.store.UpdateAnalysisSchedule(r.Context(), id, sched)
	if errors.Is(err, pgx.ErrNoRows) {
		writeError(w, http.StatusNotFound, "not_found", "analysis schedule not found")
		return
	}
	if err != nil {
		LoggerFromContext(r.Context()).Error("update analysis schedule", "id", id, "err", err)
		writeError(w, http.StatusInternalServerError, "internal_error", "failed to update analysis schedule")
		return
	}
	writeJSON(w, itemResponse{Data: updated})
}

// Delete handles DELETE /api/v1/analysis/schedules/{id}.
func (h *AnalysisScheduleHandler) Delete(w http.ResponseWriter, r *http.Request) {
	id, err := strconv.ParseInt(chi.URLParam(r, "id"), 10, 64)
	if err != nil {
		writeError(w, http.StatusBadRequest, "invalid_id", "id must be an integer")
		return
	}

	err = h.store.DeleteAnalysisSchedule(r.Context(), id)
	if errors.Is(err, pgx.ErrNoRows) {
		writeError(w, http.StatusNotFound, "not_found", "analysis schedule not found")
		return
	}
	if err != nil {
		LoggerFromContext(r.Context()).Error("delete analysis schedule", "id", id, "err", err)
		writeError(w, http.StatusInternalServerError, "internal_error", "failed to delete analysis schedule")
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

// Run handles POST /api/v1/analysis/schedules/{id}/run — the "run now" action.
func (h *AnalysisScheduleHandler) Run(w http.ResponseWriter, r *http.Request) {
	if h.scheduler == nil {
		writeError(w, http.StatusServiceUnavailable, "scheduler_disabled", "analysis scheduler is not enabled")
		return
	}

	id, err := strconv.ParseInt(chi.URLParam(r, "id"), 10, 64)
	if err != nil {
		writeError(w, http.StatusBadRequest, "invalid_id", "id must be an integer")
		return
	}

	err = h.scheduler.RunNow(r.Context(), id)
	switch {
	case errors.Is(err, postgres.ErrDuplicateActiveReport):
		writeError(w, http.StatusConflict, "duplicate_report", "a report for this feed and period is already pending or running")
		return
	case errors.Is(err, worker.ErrQueueFull):
		writeError(w, http.StatusTooManyRequests, "queue_full", "analysis worker queue is full, try again shortly")
		return
	case err != nil:
		LoggerFromContext(r.Context()).Error("run analysis schedule", "id", id, "err", err)
		writeError(w, http.StatusInternalServerError, "internal_error", "failed to run analysis schedule")
		return
	}

	w.WriteHeader(http.StatusNoContent)
}

// decodeAndValidateSchedule reads and validates a schedule from the request
// body. On validation failure it writes the error response and returns ok=false.
func (h *AnalysisScheduleHandler) decodeAndValidateSchedule(w http.ResponseWriter, r *http.Request) (model.AnalysisSchedule, bool) {
	netlogEnabled := h.netlogEnabled
	body, err := io.ReadAll(io.LimitReader(r.Body, 8*1024))
	if err != nil {
		writeError(w, http.StatusBadRequest, "read_error", "failed to read request body")
		return model.AnalysisSchedule{}, false
	}

	var sched model.AnalysisSchedule
	if err := json.Unmarshal(body, &sched); err != nil {
		writeError(w, http.StatusBadRequest, "invalid_json", "malformed JSON body")
		return model.AnalysisSchedule{}, false
	}

	if sched.Name == "" {
		writeError(w, http.StatusBadRequest, "validation_failed", "name is required")
		return model.AnalysisSchedule{}, false
	}
	if !model.IsValidAnalysisFeed(sched.Feed) {
		writeError(w, http.StatusBadRequest, "validation_failed", "feed must be netlog, srvlog, or all")
		return model.AnalysisSchedule{}, false
	}
	if (sched.Feed == model.AnalysisFeedNetlog || sched.Feed == model.AnalysisFeedAll) && !netlogEnabled {
		writeError(w, http.StatusBadRequest, "feed_unavailable", "netlog feature is disabled")
		return model.AnalysisSchedule{}, false
	}
	switch sched.Frequency {
	case "daily":
	case "weekly":
		if sched.DayOfWeek == nil || *sched.DayOfWeek < 0 || *sched.DayOfWeek > 6 {
			writeError(w, http.StatusBadRequest, "validation_failed", "weekly schedule requires day_of_week (0-6)")
			return model.AnalysisSchedule{}, false
		}
	case "monthly":
		if sched.DayOfMonth == nil || *sched.DayOfMonth < 1 || *sched.DayOfMonth > 28 {
			writeError(w, http.StatusBadRequest, "validation_failed", "monthly schedule requires day_of_month (1-28)")
			return model.AnalysisSchedule{}, false
		}
	default:
		writeError(w, http.StatusBadRequest, "validation_failed", "frequency must be daily, weekly, or monthly")
		return model.AnalysisSchedule{}, false
	}
	if _, _, err := splitTimeOfDay(sched.TimeOfDay); err != nil {
		writeError(w, http.StatusBadRequest, "validation_failed", "time_of_day must be HH:MM")
		return model.AnalysisSchedule{}, false
	}
	if sched.Timezone == "" {
		sched.Timezone = "UTC"
	}
	if _, err := time.LoadLocation(sched.Timezone); err != nil {
		writeError(w, http.StatusBadRequest, "validation_failed", "invalid timezone")
		return model.AnalysisSchedule{}, false
	}
	if !h.validateNotifyChannels(w, r, sched.NotifyChannelIDs) {
		return model.AnalysisSchedule{}, false
	}
	return sched, true
}

// validateNotifyChannels confirms every requested channel id exists and is an
// email-type channel — the only backend that renders an analysis report. On
// failure it writes the error response and returns false.
func (h *AnalysisScheduleHandler) validateNotifyChannels(w http.ResponseWriter, r *http.Request, ids []int64) bool {
	if len(ids) == 0 {
		return true
	}
	channels, err := h.store.ListNotificationChannels(r.Context())
	if err != nil {
		LoggerFromContext(r.Context()).Error("list notification channels for schedule validation", "err", err)
		writeError(w, http.StatusInternalServerError, "internal_error", "failed to validate notification channels")
		return false
	}
	emailChannels := make(map[int64]bool, len(channels))
	for _, ch := range channels {
		if ch.Type == notification.ChannelTypeEmail {
			emailChannels[ch.ID] = true
		}
	}
	for _, id := range ids {
		if !emailChannels[id] {
			writeError(w, http.StatusBadRequest, "validation_failed",
				"notify_channel_ids must reference existing email notification channels")
			return false
		}
	}
	return true
}

// splitTimeOfDay parses HH:MM into hour, minute components.
func splitTimeOfDay(s string) (int, int, error) {
	t, err := time.Parse("15:04", s)
	if err != nil {
		return 0, 0, err
	}
	return t.Hour(), t.Minute(), nil
}
