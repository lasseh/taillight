package handler

import (
	"net/http"

	"github.com/go-chi/chi/v5"
)

// NetlogDeviceHandler handles netlog device detail requests.
type NetlogDeviceHandler struct {
	store NetlogStore
}

// NewNetlogDeviceHandler creates a new NetlogDeviceHandler.
func NewNetlogDeviceHandler(store NetlogStore) *NetlogDeviceHandler {
	return &NetlogDeviceHandler{store: store}
}

// Get handles GET /api/v1/netlog/device/{hostname}.
func (h *NetlogDeviceHandler) Get(w http.ResponseWriter, r *http.Request) {
	hostname := chi.URLParam(r, "hostname")
	if hostname == "" {
		writeError(w, http.StatusBadRequest, "invalid_hostname", "hostname is required")
		return
	}

	summary, err := h.store.GetNetlogDeviceSummary(r.Context(), hostname)
	if err != nil {
		if isClientGone(r) {
			return
		}
		LoggerFromContext(r.Context()).Error("get netlog device summary failed", "hostname", hostname, "err", err)
		writeError(w, http.StatusInternalServerError, "query_failed", "failed to get netlog device summary")
		return
	}

	writeJSON(w, itemResponse{Data: summary})
}
