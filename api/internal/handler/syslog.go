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

// SyslogHandler handles REST event queries.
type SyslogHandler struct {
	store SyslogStore
}

// NewSyslogHandler creates a new SyslogHandler.
func NewSyslogHandler(store SyslogStore) *SyslogHandler {
	return &SyslogHandler{store: store}
}

// List handles GET /api/v1/syslog with filter and cursor pagination.
func (h *SyslogHandler) List(w http.ResponseWriter, r *http.Request) {
	filter, err := model.ParseSyslogFilter(r)
	if err != nil {
		writeError(w, http.StatusBadRequest, "invalid_filter", err.Error())
		return
	}
	cursor := model.ParseCursor(r)
	limit := model.ParseLimit(r, defaultLimit, maxLimit)

	events, nextCursor, err := h.store.ListSyslogs(r.Context(), filter, cursor, limit)
	if err != nil {
		if isClientGone(r) {
			return
		}
		LoggerFromContext(r.Context()).Error("list syslogs failed",
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

// syslogDetailData embeds a SyslogEvent with optional Juniper reference data.
type syslogDetailData struct {
	model.SyslogEvent
	JuniperRef []model.JuniperSyslogRef `json:"juniper_ref,omitempty"`
}

// Get handles GET /api/v1/syslog/{id}.
func (h *SyslogHandler) Get(w http.ResponseWriter, r *http.Request) {
	idStr := chi.URLParam(r, "id")
	id, err := strconv.ParseInt(idStr, 10, 64)
	if err != nil {
		writeError(w, http.StatusBadRequest, "invalid_id", "invalid event id")
		return
	}

	event, err := h.store.GetSyslog(r.Context(), id)
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			writeError(w, http.StatusNotFound, "not_found", "event not found")
			return
		}
		if isClientGone(r) {
			return
		}
		LoggerFromContext(r.Context()).Error("get syslog failed", "id", id, "err", err)
		writeError(w, http.StatusInternalServerError, "query_failed", "failed to get event")
		return
	}

	// Embed the base event and optionally enrich with Juniper reference docs.
	detail := syslogDetailData{SyslogEvent: event}

	if event.MsgID != "" {
		refs, err := h.store.LookupJuniperRef(r.Context(), event.MsgID)
		if err != nil {
			LoggerFromContext(r.Context()).Warn("juniper ref lookup failed", "msgid", event.MsgID, "err", err)
		} else if len(refs) > 0 {
			detail.JuniperRef = refs
		}
	}

	writeJSON(w, itemResponse{Data: detail})
}
