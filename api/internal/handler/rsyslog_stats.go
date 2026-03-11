package handler

import (
	"context"
	"net/http"
	"time"

	"github.com/lasseh/taillight/internal/model"
)

// defaultRsyslogStatsField is the default time-series field for rsyslog stats volume queries.
const defaultRsyslogStatsField = "submitted"

// RsyslogStatsStore defines the rsyslog stats data access interface.
type RsyslogStatsStore interface {
	GetRsyslogStatsSummary(ctx context.Context, rangeDur time.Duration) (model.RsyslogStatsSummary, error)
	GetRsyslogStatsTimeSeries(ctx context.Context, field string, interval model.VolumeInterval, rangeDur time.Duration) ([]model.RsyslogStatsTimeSeries, error)
}

// RsyslogStatsHandler handles REST endpoints for rsyslog internal statistics.
type RsyslogStatsHandler struct {
	store RsyslogStatsStore
}

// NewRsyslogStatsHandler creates a new RsyslogStatsHandler.
func NewRsyslogStatsHandler(store RsyslogStatsStore) *RsyslogStatsHandler {
	return &RsyslogStatsHandler{store: store}
}

// Summary handles GET /api/v1/rsyslog/stats/summary.
func (h *RsyslogStatsHandler) Summary(w http.ResponseWriter, r *http.Request) {
	rangeDur, err := model.ParseRange(r)
	if err != nil {
		writeError(w, http.StatusBadRequest, "invalid_params", err.Error())
		return
	}

	summary, err := h.store.GetRsyslogStatsSummary(r.Context(), rangeDur)
	if err != nil {
		LoggerFromContext(r.Context()).Error("get rsyslog stats summary failed", "err", err, "range", rangeDur)
		writeError(w, http.StatusInternalServerError, "query_failed", "failed to query rsyslog stats summary")
		return
	}

	writeJSON(w, itemResponse{Data: summary})
}

// Volume handles GET /api/v1/rsyslog/stats/volume.
func (h *RsyslogStatsHandler) Volume(w http.ResponseWriter, r *http.Request) {
	params, err := model.ParseVolumeParams(r)
	if err != nil {
		writeError(w, http.StatusBadRequest, "invalid_params", err.Error())
		return
	}

	field := r.URL.Query().Get("field")
	if field == "" {
		field = defaultRsyslogStatsField
	}

	series, err := h.store.GetRsyslogStatsTimeSeries(r.Context(), field, params.Interval, params.RangeDur)
	if err != nil {
		LoggerFromContext(r.Context()).Error("get rsyslog stats volume failed", "err", err, "field", field, "interval", params.Interval, "range", params.RangeDur)
		writeError(w, http.StatusInternalServerError, "query_failed", "failed to query rsyslog stats volume")
		return
	}

	writeJSON(w, itemResponse{Data: emptySlice(series)})
}
