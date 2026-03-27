package handler

import (
	"net/http"

	"github.com/go-chi/chi/v5"
)

// SrvlogDeviceHandler handles srvlog device detail requests.
type SrvlogDeviceHandler struct {
	store SrvlogStore
}

// NewSrvlogDeviceHandler creates a new SrvlogDeviceHandler.
func NewSrvlogDeviceHandler(store SrvlogStore) *SrvlogDeviceHandler {
	return &SrvlogDeviceHandler{store: store}
}

// Get handles GET /api/v1/srvlog/device/{hostname}.
func (h *SrvlogDeviceHandler) Get(w http.ResponseWriter, r *http.Request) {
	hostname := chi.URLParam(r, "hostname")
	if hostname == "" {
		writeError(w, http.StatusBadRequest, "invalid_hostname", "hostname is required")
		return
	}

	summary, err := h.store.GetSrvlogDeviceSummary(r.Context(), hostname)
	if err != nil {
		if isClientGone(r) {
			return
		}
		LoggerFromContext(r.Context()).Error("get srvlog device summary failed", "hostname", hostname, "err", err)
		writeError(w, http.StatusInternalServerError, "query_failed", "failed to get device summary")
		return
	}

	writeJSON(w, itemResponse{Data: summary})
}
