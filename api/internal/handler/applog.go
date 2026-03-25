package handler

import (
	"errors"
	"net/http"
	"strconv"

	"github.com/go-chi/chi/v5"
	"github.com/jackc/pgx/v5"

	"github.com/lasseh/taillight/internal/model"
)

const (
	applogDefaultLimit = 100
	applogMaxLimit     = 1000
)

// AppLogHandler handles REST event queries for log events.
type AppLogHandler struct {
	store AppLogStore
}

// NewAppLogHandler creates a new AppLogHandler.
func NewAppLogHandler(store AppLogStore) *AppLogHandler {
	return &AppLogHandler{store: store}
}

// List handles GET /api/v1/applog with filter and cursor pagination.
func (h *AppLogHandler) List(w http.ResponseWriter, r *http.Request) {
	filter, err := model.ParseAppLogFilter(r)
	if err != nil {
		writeError(w, http.StatusBadRequest, "invalid_filter", err.Error())
		return
	}
	cursor := model.ParseCursor(r)
	limit := model.ParseLimit(r, applogDefaultLimit, applogMaxLimit)

	events, nextCursor, err := h.store.ListAppLogs(r.Context(), filter, cursor, limit)
	if err != nil {
		if isClientGone(r) {
			return
		}
		LoggerFromContext(r.Context()).Error("list applogs failed",
			"err", err,
			"service", filter.Service,
			"host", filter.Host,
			"level", filter.Level,
			"search", filter.Search,
		)
		writeError(w, http.StatusInternalServerError, "query_failed", "failed to query log events")
		return
	}

	resp := listResponse{
		Data:    emptySlice(events),
		HasMore: nextCursor != nil,
	}
	if nextCursor != nil {
		encoded := nextCursor.Encode()
		resp.Cursor = &encoded
	}

	writeJSON(w, resp)
}

// Get handles GET /api/v1/applog/{id}.
func (h *AppLogHandler) Get(w http.ResponseWriter, r *http.Request) {
	idStr := chi.URLParam(r, "id")
	id, err := strconv.ParseInt(idStr, 10, 64)
	if err != nil {
		writeError(w, http.StatusBadRequest, "invalid_id", "invalid event id")
		return
	}

	event, err := h.store.GetAppLog(r.Context(), id)
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			writeError(w, http.StatusNotFound, "not_found", "event not found")
			return
		}
		if isClientGone(r) {
			return
		}
		LoggerFromContext(r.Context()).Error("get applog failed", "id", id, "err", err)
		writeError(w, http.StatusInternalServerError, "query_failed", "failed to get event")
		return
	}

	writeJSON(w, itemResponse{Data: event})
}
