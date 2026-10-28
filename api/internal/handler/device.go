package handler

import (
	"net/http"

	"github.com/go-chi/chi/v5"
)

// SrvlogDeviceHandler handles srvlog device detail requests.
type SrvlogDeviceHandler struct {
	store SrvlogStore
	mutes MuteRuleSet
}

// NewSrvlogDeviceHandler creates a new SrvlogDeviceHandler.
func NewSrvlogDeviceHandler(store SrvlogStore, mutes MuteRuleSet) *SrvlogDeviceHandler {
	return &SrvlogDeviceHandler{store: store, mutes: mutes}
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
	markSrvlogs(h.mutes.Active(), summary.CriticalLogs)

	writeJSON(w, itemResponse{Data: summary})
}
