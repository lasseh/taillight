package handler

import (
	"context"
	"net/http"

	"github.com/go-chi/chi/v5"

	"github.com/lasseh/taillight/internal/model"
)

// DeviceStore defines the data access interface for device queries.
type DeviceStore interface {
	GetDeviceSummary(ctx context.Context, hostname string) (model.DeviceSummary, error)
}

// DeviceHandler handles device detail requests.
type DeviceHandler struct {
	store DeviceStore
}

// NewDeviceHandler creates a new DeviceHandler.
func NewDeviceHandler(store DeviceStore) *DeviceHandler {
	return &DeviceHandler{store: store}
}

// Get handles GET /api/v1/device/{hostname}.
func (h *DeviceHandler) Get(w http.ResponseWriter, r *http.Request) {
	hostname := chi.URLParam(r, "hostname")
	if hostname == "" {
		writeError(w, http.StatusBadRequest, "invalid_hostname", "hostname is required")
		return
	}

	summary, err := h.store.GetDeviceSummary(r.Context(), hostname)
	if err != nil {
		LoggerFromContext(r.Context()).Error("get device summary failed", "hostname", hostname, "err", err)
		writeError(w, http.StatusInternalServerError, "query_failed", "failed to get device summary")
		return
	}

	writeJSON(w, itemResponse{Data: summary})
}
