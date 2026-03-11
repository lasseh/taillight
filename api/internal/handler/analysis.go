package handler

import (
	"context"
	"errors"
	"net/http"
	"strconv"

	"github.com/go-chi/chi/v5"
	"github.com/jackc/pgx/v5"

	"github.com/lasseh/taillight/internal/model"
)

const analysisDefaultLimit = 30

// AnalysisStore defines the analysis report data access interface.
type AnalysisStore interface {
	GetReport(ctx context.Context, id int64) (model.AnalysisReport, error)
	ListReports(ctx context.Context, limit int) ([]model.AnalysisReportSummary, error)
	GetLatestReport(ctx context.Context) (model.AnalysisReport, error)
}

// AnalysisRunner can trigger an analysis run.
type AnalysisRunner interface {
	Run(ctx context.Context) (int64, error)
}

// AnalysisHandler handles REST endpoints for analysis reports.
type AnalysisHandler struct {
	store  AnalysisStore
	runner AnalysisRunner
}

// NewAnalysisHandler creates a new AnalysisHandler.
func NewAnalysisHandler(store AnalysisStore, runner AnalysisRunner) *AnalysisHandler {
	return &AnalysisHandler{store: store, runner: runner}
}

// List handles GET /api/v1/analysis/reports.
func (h *AnalysisHandler) List(w http.ResponseWriter, r *http.Request) {
	limit := model.ParseLimit(r, analysisDefaultLimit, 100)

	reports, err := h.store.ListReports(r.Context(), limit)
	if err != nil {
		LoggerFromContext(r.Context()).Error("list analysis reports failed", "err", err)
		writeError(w, http.StatusInternalServerError, "query_failed", "failed to list reports")
		return
	}

	writeJSON(w, itemResponse{Data: emptySlice(reports)})
}

// Latest handles GET /api/v1/analysis/reports/latest.
func (h *AnalysisHandler) Latest(w http.ResponseWriter, r *http.Request) {
	report, err := h.store.GetLatestReport(r.Context())
	if errors.Is(err, pgx.ErrNoRows) {
		writeError(w, http.StatusNotFound, "not_found", "no analysis reports found")
		return
	}
	if err != nil {
		LoggerFromContext(r.Context()).Error("get latest analysis report failed", "err", err)
		writeError(w, http.StatusInternalServerError, "query_failed", "failed to get latest report")
		return
	}

	writeJSON(w, itemResponse{Data: report})
}

// Get handles GET /api/v1/analysis/reports/{id}.
func (h *AnalysisHandler) Get(w http.ResponseWriter, r *http.Request) {
	idStr := chi.URLParam(r, "id")
	id, err := strconv.ParseInt(idStr, 10, 64)
	if err != nil {
		writeError(w, http.StatusBadRequest, "invalid_id", "id must be an integer")
		return
	}

	report, err := h.store.GetReport(r.Context(), id)
	if errors.Is(err, pgx.ErrNoRows) {
		writeError(w, http.StatusNotFound, "not_found", "report not found")
		return
	}
	if err != nil {
		LoggerFromContext(r.Context()).Error("get analysis report failed", "id", id, "err", err)
		writeError(w, http.StatusInternalServerError, "query_failed", "failed to get report")
		return
	}

	writeJSON(w, itemResponse{Data: report})
}

// Trigger handles POST /api/v1/analysis/reports/trigger.
func (h *AnalysisHandler) Trigger(w http.ResponseWriter, r *http.Request) {
	if h.runner == nil {
		writeError(w, http.StatusServiceUnavailable, "not_configured", "analysis is not enabled")
		return
	}

	id, err := h.runner.Run(r.Context())
	if err != nil {
		LoggerFromContext(r.Context()).Error("manual analysis trigger failed", "err", err)
		writeError(w, http.StatusInternalServerError, "analysis_failed", "analysis run failed")
		return
	}

	writeJSON(w, itemResponse{Data: map[string]int64{"report_id": id}})
}
