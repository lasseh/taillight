package handler

import (
	"errors"
	"net/http"
	"strconv"

	"github.com/go-chi/chi/v5"
	"github.com/jackc/pgx/v5"

	"github.com/lasseh/taillight/internal/model"
)

// netlogDetailData wraps a netlog event with optional Juniper reference enrichment.
type netlogDetailData struct {
	Event      model.NetlogEvent        `json:"event"`
	JuniperRef []model.JuniperNetlogRef `json:"juniper_ref,omitempty"`
}

// NetlogHandler handles REST event queries for network device logs.
type NetlogHandler struct {
	store NetlogStore
}

// NewNetlogHandler creates a new NetlogHandler.
func NewNetlogHandler(store NetlogStore) *NetlogHandler {
	return &NetlogHandler{store: store}
}

// List handles GET /api/v1/netlog with filter and cursor pagination.
func (h *NetlogHandler) List(w http.ResponseWriter, r *http.Request) {
	filter, err := model.ParseNetlogFilter(r)
	if err != nil {
		writeError(w, http.StatusBadRequest, "invalid_filter", err.Error())
		return
	}
	cursor := model.ParseCursor(r)
	limit := model.ParseLimit(r, defaultLimit, maxLimit)

	events, nextCursor, err := h.store.ListNetlogs(r.Context(), filter, cursor, limit)
	if err != nil {
		if isClientGone(r) {
			return
		}
		LoggerFromContext(r.Context()).Error("list netlogs failed",
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

// Get handles GET /api/v1/netlog/{id} with Juniper reference enrichment.
func (h *NetlogHandler) Get(w http.ResponseWriter, r *http.Request) {
	idStr := chi.URLParam(r, "id")
	id, err := strconv.ParseInt(idStr, 10, 64)
	if err != nil {
		writeError(w, http.StatusBadRequest, "invalid_id", "invalid event id")
		return
	}

	event, err := h.store.GetNetlog(r.Context(), id)
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			writeError(w, http.StatusNotFound, "not_found", "event not found")
			return
		}
		if isClientGone(r) {
			return
		}
		LoggerFromContext(r.Context()).Error("get netlog failed", "id", id, "err", err)
		writeError(w, http.StatusInternalServerError, "query_failed", "failed to get event")
		return
	}

	detail := netlogDetailData{Event: event}

	// Enrich with Juniper reference if MsgID is set.
	if event.MsgID != "" {
		refs, err := h.store.LookupJuniperRef(r.Context(), event.MsgID)
		if err != nil {
			// Log but don't fail the request — the event itself is still valid.
			LoggerFromContext(r.Context()).Warn("juniper ref lookup failed", "msgid", event.MsgID, "err", err)
		} else {
			detail.JuniperRef = refs
		}
	}

	writeJSON(w, itemResponse{Data: detail})
}
