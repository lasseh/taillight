package handler

import (
	"net/http"

	"github.com/lasseh/taillight/internal/model"
)

// SrvlogMetaHandler handles REST metadata endpoints for filter UI dropdowns.
type SrvlogMetaHandler struct {
	store SrvlogStore
}

// NewSrvlogMetaHandler creates a new SrvlogMetaHandler.
func NewSrvlogMetaHandler(store SrvlogStore) *SrvlogMetaHandler {
	return &SrvlogMetaHandler{store: store}
}

// metaSearch reads the optional search parameter of a meta list endpoint. It
// writes a 400 and returns false when the parameter is invalid.
func metaSearch(w http.ResponseWriter, r *http.Request) (string, bool) {
	search, err := model.ParseMetaSearch(r)
	if err != nil {
		writeError(w, http.StatusBadRequest, "invalid_params", err.Error())
		return "", false
	}
	return search, true
}

// Hosts handles GET /api/v1/meta/hosts.
func (h *SrvlogMetaHandler) Hosts(w http.ResponseWriter, r *http.Request) {
	search, ok := metaSearch(w, r)
	if !ok {
		return
	}
	hosts, err := h.store.ListSrvlogHosts(r.Context(), search)
	if err != nil {
		if isClientGone(r) {
			return
		}
		LoggerFromContext(r.Context()).Error("list hosts failed", "err", err)
		writeError(w, http.StatusInternalServerError, "query_failed", "failed to list hosts")
		return
	}
	writeJSON(w, itemResponse{Data: emptySlice(hosts)})
}

// Programs handles GET /api/v1/meta/programs.
func (h *SrvlogMetaHandler) Programs(w http.ResponseWriter, r *http.Request) {
	search, ok := metaSearch(w, r)
	if !ok {
		return
	}
	programs, err := h.store.ListSrvlogPrograms(r.Context(), search)
	if err != nil {
		if isClientGone(r) {
			return
		}
		LoggerFromContext(r.Context()).Error("list programs failed", "err", err)
		writeError(w, http.StatusInternalServerError, "query_failed", "failed to list programs")
		return
	}
	writeJSON(w, itemResponse{Data: emptySlice(programs)})
}

// Facilities handles GET /api/v1/meta/facilities.
func (h *SrvlogMetaHandler) Facilities(w http.ResponseWriter, r *http.Request) {
	facilities, err := h.store.ListSrvlogFacilities(r.Context())
	if err != nil {
		if isClientGone(r) {
			return
		}
		LoggerFromContext(r.Context()).Error("list facilities failed", "err", err)
		writeError(w, http.StatusInternalServerError, "query_failed", "failed to list facilities")
		return
	}
	writeJSON(w, itemResponse{Data: emptySlice(facilities)})
}

// Tags handles GET /api/v1/meta/tags.
func (h *SrvlogMetaHandler) Tags(w http.ResponseWriter, r *http.Request) {
	search, ok := metaSearch(w, r)
	if !ok {
		return
	}
	tags, err := h.store.ListSrvlogTags(r.Context(), search)
	if err != nil {
		if isClientGone(r) {
			return
		}
		LoggerFromContext(r.Context()).Error("list tags failed", "err", err)
		writeError(w, http.StatusInternalServerError, "query_failed", "failed to list tags")
		return
	}
	writeJSON(w, itemResponse{Data: emptySlice(tags)})
}
