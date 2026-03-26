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
	defaultLimit = 100
	maxLimit     = 1000
)

// SrvlogHandler handles REST event queries.
type SrvlogHandler struct {
	store SrvlogStore
}

// NewSrvlogHandler creates a new SrvlogHandler.
func NewSrvlogHandler(store SrvlogStore) *SrvlogHandler {
	return &SrvlogHandler{store: store}
}

// List handles GET /api/v1/srvlog with filter and cursor pagination.
func (h *SrvlogHandler) List(w http.ResponseWriter, r *http.Request) {
	filter, err := model.ParseSrvlogFilter(r)
	if err != nil {
		writeError(w, http.StatusBadRequest, "invalid_filter", err.Error())
		return
	}
	cursor := model.ParseCursor(r)
	limit := model.ParseLimit(r, defaultLimit, maxLimit)

	events, nextCursor, err := h.store.ListSrvlogs(r.Context(), filter, cursor, limit)
	if err != nil {
		if isClientGone(r) {
			return
		}
		LoggerFromContext(r.Context()).Error("list srvlogs failed",
			"err", err,
			"hostname", filter.Hostname,
			"programname", filter.Programname,
			"severity", filter.Severity,
			"facility", filter.Facility,
			"search", filter.Search,
		)
		writeError(w, http.StatusInternalServerError, "query_failed", "failed to query events")
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

// Get handles GET /api/v1/srvlog/{id}.
func (h *SrvlogHandler) Get(w http.ResponseWriter, r *http.Request) {
	idStr := chi.URLParam(r, "id")
	id, err := strconv.ParseInt(idStr, 10, 64)
	if err != nil {
		writeError(w, http.StatusBadRequest, "invalid_id", "invalid event id")
		return
	}

	event, err := h.store.GetSrvlog(r.Context(), id)
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			writeError(w, http.StatusNotFound, "not_found", "event not found")
			return
		}
		if isClientGone(r) {
			return
		}
		LoggerFromContext(r.Context()).Error("get srvlog failed", "id", id, "err", err)
		writeError(w, http.StatusInternalServerError, "query_failed", "failed to get event")
		return
	}

	writeJSON(w, itemResponse{Data: event})
}
