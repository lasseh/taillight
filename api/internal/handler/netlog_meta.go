package handler

import (
	"net/http"
)

// NetlogMetaHandler handles REST metadata endpoints for netlog filter UI dropdowns.
type NetlogMetaHandler struct {
	store NetlogStore
}

// NewNetlogMetaHandler creates a new NetlogMetaHandler.
func NewNetlogMetaHandler(store NetlogStore) *NetlogMetaHandler {
	return &NetlogMetaHandler{store: store}
}

// Hosts handles GET /api/v1/netlog/meta/hosts.
func (h *NetlogMetaHandler) Hosts(w http.ResponseWriter, r *http.Request) {
	hosts, err := h.store.ListNetlogHosts(r.Context())
	if err != nil {
		if isClientGone(r) {
			return
		}
		LoggerFromContext(r.Context()).Error("list netlog hosts failed", "err", err)
		writeError(w, http.StatusInternalServerError, "query_failed", "failed to list hosts")
		return
	}
	writeJSON(w, itemResponse{Data: emptySlice(hosts)})
}

// Programs handles GET /api/v1/netlog/meta/programs.
func (h *NetlogMetaHandler) Programs(w http.ResponseWriter, r *http.Request) {
	programs, err := h.store.ListNetlogPrograms(r.Context())
	if err != nil {
		if isClientGone(r) {
			return
		}
		LoggerFromContext(r.Context()).Error("list netlog programs failed", "err", err)
		writeError(w, http.StatusInternalServerError, "query_failed", "failed to list programs")
		return
	}
	writeJSON(w, itemResponse{Data: emptySlice(programs)})
}

// Facilities handles GET /api/v1/netlog/meta/facilities.
func (h *NetlogMetaHandler) Facilities(w http.ResponseWriter, r *http.Request) {
	facilities, err := h.store.ListNetlogFacilities(r.Context())
	if err != nil {
		if isClientGone(r) {
			return
		}
		LoggerFromContext(r.Context()).Error("list netlog facilities failed", "err", err)
		writeError(w, http.StatusInternalServerError, "query_failed", "failed to list facilities")
		return
	}
	writeJSON(w, itemResponse{Data: emptySlice(facilities)})
}

// Tags handles GET /api/v1/netlog/meta/tags.
func (h *NetlogMetaHandler) Tags(w http.ResponseWriter, r *http.Request) {
	tags, err := h.store.ListNetlogTags(r.Context())
	if err != nil {
		if isClientGone(r) {
			return
		}
		LoggerFromContext(r.Context()).Error("list netlog tags failed", "err", err)
		writeError(w, http.StatusInternalServerError, "query_failed", "failed to list tags")
		return
	}
	writeJSON(w, itemResponse{Data: emptySlice(tags)})
}
