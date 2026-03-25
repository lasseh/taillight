package handler

import (
	"context"
	"net/http"
	"time"

	"github.com/lasseh/taillight/internal/model"
)

// StatsStore defines the stats data access interface.
type StatsStore interface {
	GetVolume(ctx context.Context, interval model.VolumeInterval, rangeDur time.Duration) ([]model.VolumeBucket, error)
	GetAppLogVolume(ctx context.Context, interval model.VolumeInterval, rangeDur time.Duration) ([]model.VolumeBucket, error)
	GetSeverityVolume(ctx context.Context, interval model.VolumeInterval, rangeDur time.Duration) ([]model.SeverityVolumeBucket, error)
	GetAppLogSeverityVolume(ctx context.Context, interval model.VolumeInterval, rangeDur time.Duration) ([]model.SeverityVolumeBucket, error)
	GetSyslogSummary(ctx context.Context, rangeDur time.Duration) (model.SyslogSummary, error)
	GetAppLogSummary(ctx context.Context, rangeDur time.Duration) (model.AppLogSummary, error)
}

// StatsHandler handles REST endpoints for dashboard statistics.
type StatsHandler struct {
	store StatsStore
}

// NewStatsHandler creates a new StatsHandler.
func NewStatsHandler(store StatsStore) *StatsHandler {
	return &StatsHandler{store: store}
}

// Volume handles GET /api/v1/stats/volume.
func (h *StatsHandler) Volume(w http.ResponseWriter, r *http.Request) {
	params, err := model.ParseVolumeParams(r)
	if err != nil {
		writeError(w, http.StatusBadRequest, "invalid_params", err.Error())
		return
	}

	buckets, err := h.store.GetVolume(r.Context(), params.Interval, params.RangeDur)
	if err != nil {
		if isClientGone(r) {
			return
		}
		LoggerFromContext(r.Context()).Error("get volume failed", "err", err, "interval", params.Interval, "range", params.RangeDur)
		writeError(w, http.StatusInternalServerError, "query_failed", "failed to query volume")
		return
	}

	writeJSON(w, itemResponse{Data: emptySlice(buckets)})
}

// AppLogVolume handles GET /api/v1/applog/stats/volume.
func (h *StatsHandler) AppLogVolume(w http.ResponseWriter, r *http.Request) {
	params, err := model.ParseVolumeParams(r)
	if err != nil {
		writeError(w, http.StatusBadRequest, "invalid_params", err.Error())
		return
	}

	buckets, err := h.store.GetAppLogVolume(r.Context(), params.Interval, params.RangeDur)
	if err != nil {
		if isClientGone(r) {
			return
		}
		LoggerFromContext(r.Context()).Error("get applog volume failed", "err", err, "interval", params.Interval, "range", params.RangeDur)
		writeError(w, http.StatusInternalServerError, "query_failed", "failed to query applog volume")
		return
	}

	writeJSON(w, itemResponse{Data: emptySlice(buckets)})
}

// SeverityVolume handles GET /api/v1/stats/severity-volume.
func (h *StatsHandler) SeverityVolume(w http.ResponseWriter, r *http.Request) {
	params, err := model.ParseVolumeParams(r)
	if err != nil {
		writeError(w, http.StatusBadRequest, "invalid_params", err.Error())
		return
	}

	buckets, err := h.store.GetSeverityVolume(r.Context(), params.Interval, params.RangeDur)
	if err != nil {
		if isClientGone(r) {
			return
		}
		LoggerFromContext(r.Context()).Error("get severity volume failed", "err", err, "interval", params.Interval, "range", params.RangeDur)
		writeError(w, http.StatusInternalServerError, "query_failed", "failed to query severity volume")
		return
	}

	writeJSON(w, itemResponse{Data: emptySlice(buckets)})
}

// AppLogSeverityVolume handles GET /api/v1/applog/stats/severity-volume.
func (h *StatsHandler) AppLogSeverityVolume(w http.ResponseWriter, r *http.Request) {
	params, err := model.ParseVolumeParams(r)
	if err != nil {
		writeError(w, http.StatusBadRequest, "invalid_params", err.Error())
		return
	}

	buckets, err := h.store.GetAppLogSeverityVolume(r.Context(), params.Interval, params.RangeDur)
	if err != nil {
		if isClientGone(r) {
			return
		}
		LoggerFromContext(r.Context()).Error("get applog severity volume failed", "err", err, "interval", params.Interval, "range", params.RangeDur)
		writeError(w, http.StatusInternalServerError, "query_failed", "failed to query applog severity volume")
		return
	}

	writeJSON(w, itemResponse{Data: emptySlice(buckets)})
}

// SyslogSummary handles GET /api/v1/stats/summary.
func (h *StatsHandler) SyslogSummary(w http.ResponseWriter, r *http.Request) {
	rangeDur, err := model.ParseRange(r)
	if err != nil {
		writeError(w, http.StatusBadRequest, "invalid_params", err.Error())
		return
	}

	summary, err := h.store.GetSyslogSummary(r.Context(), rangeDur)
	if err != nil {
		if isClientGone(r) {
			return
		}
		LoggerFromContext(r.Context()).Error("get syslog summary failed", "err", err, "range", rangeDur)
		writeError(w, http.StatusInternalServerError, "query_failed", "failed to query syslog summary")
		return
	}

	writeJSON(w, itemResponse{Data: summary})
}

// AppLogSummary handles GET /api/v1/applog/stats/summary.
func (h *StatsHandler) AppLogSummary(w http.ResponseWriter, r *http.Request) {
	rangeDur, err := model.ParseRange(r)
	if err != nil {
		writeError(w, http.StatusBadRequest, "invalid_params", err.Error())
		return
	}

	summary, err := h.store.GetAppLogSummary(r.Context(), rangeDur)
	if err != nil {
		if isClientGone(r) {
			return
		}
		LoggerFromContext(r.Context()).Error("get applog summary failed", "err", err, "range", rangeDur)
		writeError(w, http.StatusInternalServerError, "query_failed", "failed to query applog summary")
		return
	}

	writeJSON(w, itemResponse{Data: summary})
}
