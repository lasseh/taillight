package handler

import (
	"context"
	"net/http"
	"time"

	"github.com/lasseh/taillight/internal/model"
)

// defaultMetricsField is the default time-series field for metrics volume queries.
const defaultMetricsField = "events_broadcast"

// TaillightMetricsStore defines the taillight metrics data access interface.
type TaillightMetricsStore interface {
	GetMetricsSummary(ctx context.Context, rangeDur time.Duration) (model.MetricsSummary, error)
	GetMetricsTimeSeries(ctx context.Context, field string, interval model.VolumeInterval, rangeDur time.Duration) ([]model.MetricsTimeSeries, error)
}

// TaillightMetricsHandler handles REST endpoints for taillight application metrics.
type TaillightMetricsHandler struct {
	store TaillightMetricsStore
}

// NewTaillightMetricsHandler creates a new TaillightMetricsHandler.
func NewTaillightMetricsHandler(store TaillightMetricsStore) *TaillightMetricsHandler {
	return &TaillightMetricsHandler{store: store}
}

// Summary handles GET /api/v1/metrics/summary.
func (h *TaillightMetricsHandler) Summary(w http.ResponseWriter, r *http.Request) {
	rangeDur, err := model.ParseRange(r)
	if err != nil {
		writeError(w, http.StatusBadRequest, "invalid_params", err.Error())
		return
	}

	summary, err := h.store.GetMetricsSummary(r.Context(), rangeDur)
	if err != nil {
		LoggerFromContext(r.Context()).Error("get metrics summary failed", "err", err)
		writeError(w, http.StatusInternalServerError, "query_failed", "failed to query metrics summary")
		return
	}

	writeJSON(w, itemResponse{Data: summary})
}

// Volume handles GET /api/v1/metrics/volume.
func (h *TaillightMetricsHandler) Volume(w http.ResponseWriter, r *http.Request) {
	params, err := model.ParseVolumeParams(r)
	if err != nil {
		writeError(w, http.StatusBadRequest, "invalid_params", err.Error())
		return
	}

	field := r.URL.Query().Get("field")
	if field == "" {
		field = defaultMetricsField
	}

	series, err := h.store.GetMetricsTimeSeries(r.Context(), field, params.Interval, params.RangeDur)
	if err != nil {
		LoggerFromContext(r.Context()).Error("get metrics volume failed", "err", err)
		writeError(w, http.StatusInternalServerError, "query_failed", "failed to query metrics volume")
		return
	}

	writeJSON(w, itemResponse{Data: emptySlice(series)})
}
