package handler

import (
	"context"
	"net/http"
	"time"

	"github.com/go-chi/chi/v5"

	"github.com/lasseh/taillight/internal/model"
)

// AppLogDeviceStore defines the data access interface for applog device queries.
type AppLogDeviceStore interface {
	GetAppLogDeviceSummary(ctx context.Context, host string) (model.AppLogDeviceSummary, error)
}

// AppLogDeviceHandler handles applog device detail requests.
type AppLogDeviceHandler struct {
	store AppLogDeviceStore
}

// NewAppLogDeviceHandler creates a new AppLogDeviceHandler.
func NewAppLogDeviceHandler(store AppLogDeviceStore) *AppLogDeviceHandler {
	return &AppLogDeviceHandler{store: store}
}

// Get handles GET /api/v1/applog/device/{hostname}.
func (h *AppLogDeviceHandler) Get(w http.ResponseWriter, r *http.Request) {
	hostname := chi.URLParam(r, "hostname")
	if hostname == "" {
		writeError(w, http.StatusBadRequest, "invalid_hostname", "hostname is required")
		return
	}

	start := time.Now()
	summary, err := h.store.GetAppLogDeviceSummary(r.Context(), hostname)
	dur := time.Since(start)
	if err != nil {
		if isClientGone(r) {
			return
		}
		LoggerFromContext(r.Context()).Error("get applog device summary failed", "host", hostname, "err", err)
		writeError(w, http.StatusInternalServerError, "query_failed", "failed to get applog device summary")
		return
	}
	if dur > 500*time.Millisecond {
		LoggerFromContext(r.Context()).Warn("slow applog device summary", "host", hostname, "duration", dur, "total_count", summary.TotalCount)
	}

	writeJSON(w, itemResponse{Data: summary})
}
