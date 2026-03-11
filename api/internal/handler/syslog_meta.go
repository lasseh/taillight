package handler

import (
	"net/http"
)

// SyslogMetaHandler handles REST metadata endpoints for filter UI dropdowns.
type SyslogMetaHandler struct {
	store SyslogStore
}

// NewSyslogMetaHandler creates a new SyslogMetaHandler.
func NewSyslogMetaHandler(store SyslogStore) *SyslogMetaHandler {
	return &SyslogMetaHandler{store: store}
}

// Hosts handles GET /api/v1/meta/hosts.
func (h *SyslogMetaHandler) Hosts(w http.ResponseWriter, r *http.Request) {
	hosts, err := h.store.ListHosts(r.Context())
	if err != nil {
		LoggerFromContext(r.Context()).Error("list hosts failed", "err", err)
		writeError(w, http.StatusInternalServerError, "query_failed", "failed to list hosts")
		return
	}
	writeJSON(w, itemResponse{Data: emptySlice(hosts)})
}

// Programs handles GET /api/v1/meta/programs.
func (h *SyslogMetaHandler) Programs(w http.ResponseWriter, r *http.Request) {
	programs, err := h.store.ListPrograms(r.Context())
	if err != nil {
		LoggerFromContext(r.Context()).Error("list programs failed", "err", err)
		writeError(w, http.StatusInternalServerError, "query_failed", "failed to list programs")
		return
	}
	writeJSON(w, itemResponse{Data: emptySlice(programs)})
}

// Facilities handles GET /api/v1/meta/facilities.
func (h *SyslogMetaHandler) Facilities(w http.ResponseWriter, r *http.Request) {
	facilities, err := h.store.ListFacilities(r.Context())
	if err != nil {
		LoggerFromContext(r.Context()).Error("list facilities failed", "err", err)
		writeError(w, http.StatusInternalServerError, "query_failed", "failed to list facilities")
		return
	}
	writeJSON(w, itemResponse{Data: emptySlice(facilities)})
}

// Tags handles GET /api/v1/meta/tags.
func (h *SyslogMetaHandler) Tags(w http.ResponseWriter, r *http.Request) {
	tags, err := h.store.ListTags(r.Context())
	if err != nil {
		LoggerFromContext(r.Context()).Error("list tags failed", "err", err)
		writeError(w, http.StatusInternalServerError, "query_failed", "failed to list tags")
		return
	}
	writeJSON(w, itemResponse{Data: emptySlice(tags)})
}
