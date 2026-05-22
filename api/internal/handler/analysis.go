package handler

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/jackc/pgx/v5"

	"github.com/lasseh/taillight/internal/model"
	"github.com/lasseh/taillight/internal/postgres"
	"github.com/lasseh/taillight/internal/worker"
)

const analysisDefaultLimit = 30

// AnalysisReportStore is the persistence interface for the analysis handler.
type AnalysisReportStore interface {
	ListReports(ctx context.Context, limit int) ([]model.AnalysisReportSummary, error)
	GetReportBySlug(ctx context.Context, slug string) (model.AnalysisReport, error)
	DeleteReport(ctx context.Context, id int64) error
	ListAnalysisHosts(ctx context.Context, feed string) ([]string, error)
	ListAnalysisHostEntries(ctx context.Context, feed string) ([]model.AnalysisHostEntry, error)
}

// AnalysisEnqueuer accepts new report runs.
type AnalysisEnqueuer interface {
	Enqueue(ctx context.Context, req model.AnalysisReport) (model.AnalysisReport, error)
}

// AnalysisHandler serves the report list, detail, create, and delete endpoints.
// The enqueuer and netlogEnabled flag are optional — pass nil/false to disable
// the corresponding capabilities (used in deployments without analysis or netlog).
type AnalysisHandler struct {
	store         AnalysisReportStore
	enqueuer      AnalysisEnqueuer
	netlogEnabled bool
}

// NewAnalysisHandler creates a new AnalysisHandler.
func NewAnalysisHandler(store AnalysisReportStore, enqueuer AnalysisEnqueuer, netlogEnabled bool) *AnalysisHandler {
	return &AnalysisHandler{store: store, enqueuer: enqueuer, netlogEnabled: netlogEnabled}
}

// List handles GET /api/v1/analysis/reports.
func (h *AnalysisHandler) List(w http.ResponseWriter, r *http.Request) {
	limit := model.ParseLimit(r, analysisDefaultLimit, 100)

	reports, err := h.store.ListReports(r.Context(), limit)
	if err != nil {
		if isClientGone(r) {
			return
		}
		LoggerFromContext(r.Context()).Error("list analysis reports failed", "err", err)
		writeError(w, http.StatusInternalServerError, "query_failed", "failed to list reports")
		return
	}

	writeJSON(w, itemResponse{Data: emptySlice(reports)})
}

// Get handles GET /api/v1/analysis/reports/{slug}.
func (h *AnalysisHandler) Get(w http.ResponseWriter, r *http.Request) {
	slug := chi.URLParam(r, "slug")
	if slug == "" {
		writeError(w, http.StatusBadRequest, "invalid_slug", "slug is required")
		return
	}

	report, err := h.store.GetReportBySlug(r.Context(), slug)
	if errors.Is(err, pgx.ErrNoRows) {
		writeError(w, http.StatusNotFound, "not_found", "report not found")
		return
	}
	if err != nil {
		if isClientGone(r) {
			return
		}
		LoggerFromContext(r.Context()).Error("get analysis report failed", "slug", slug, "err", err)
		writeError(w, http.StatusInternalServerError, "query_failed", "failed to get report")
		return
	}

	writeJSON(w, itemResponse{Data: report})
}

// createReportRequest is the JSON body for POST /api/v1/analysis/reports.
//
// PromptMode selects the prompt set framing the report ("daily", "weekly", or
// "incident"). Empty defaults to "daily". PeriodMinutes overrides the analysis
// window; empty/zero picks a mode-aware default (24h for daily/weekly, 60min
// for incident). Bounds: 5 ≤ period_minutes ≤ 43200 (5min..30d).
//
// Hosts optionally restricts the report to an explicit set of hostnames.
// Empty/missing means "all hosts on the feed." Names that don't exist for
// the selected feed are rejected up-front rather than producing a thin
// report at worker time.
type createReportRequest struct {
	Feed          string   `json:"feed"`
	PromptMode    string   `json:"prompt_mode,omitempty"`
	PeriodMinutes int      `json:"period_minutes,omitempty"`
	Hosts         []string `json:"hosts,omitempty"`
}

// requestBodyLimit caps the JSON request body. Picked to comfortably hold a
// large host list (a few hundred FQDNs) without becoming an attack surface
// for oversized payloads.
const requestBodyLimit = 64 * 1024

// Period bounds for manual triggers. The general upper bound matches monthly
// schedules so manual runs can never exceed what a recurring schedule could
// produce. Incident mode has a tighter ceiling because the prompt is written
// for "live triage" — handing it a 30-day window produces incoherent output.
const (
	minPeriodMinutes         = 5
	maxPeriodMinutes         = 30 * 24 * 60 // 30 days
	maxIncidentPeriodMinutes = 6 * 60       // 6 hours
)

// defaultPeriodMinutes returns the per-mode default analysis window when the
// caller doesn't override it. Daily mirrors the historical 24h window; weekly
// gives the trend prompt 7 days of context; incident keeps a tight 1h window
// so live triage focuses on what's happening right now.
func defaultPeriodMinutes(mode string) int {
	switch mode {
	case model.AnalysisModeWeekly:
		return 7 * 24 * 60
	case model.AnalysisModeIncident:
		return 60
	default:
		return 24 * 60
	}
}

// Create handles POST /api/v1/analysis/reports. The new row is returned with
// status="pending" and the worker picks it up asynchronously.
func (h *AnalysisHandler) Create(w http.ResponseWriter, r *http.Request) {
	if h.enqueuer == nil {
		writeError(w, http.StatusServiceUnavailable, "not_configured", "analysis is not enabled")
		return
	}

	body, err := io.ReadAll(io.LimitReader(r.Body, requestBodyLimit))
	if err != nil {
		writeError(w, http.StatusBadRequest, "read_error", "failed to read request body")
		return
	}

	var req createReportRequest
	if err := json.Unmarshal(body, &req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid_json", "malformed JSON body")
		return
	}

	if !model.IsValidAnalysisFeed(req.Feed) {
		writeError(w, http.StatusBadRequest, "invalid_feed", "feed must be netlog, srvlog, or all")
		return
	}
	if (req.Feed == model.AnalysisFeedNetlog || req.Feed == model.AnalysisFeedAll) && !h.netlogEnabled {
		writeError(w, http.StatusBadRequest, "feed_unavailable", "netlog feature is disabled")
		return
	}

	mode := req.PromptMode
	if mode == "" {
		mode = model.AnalysisModeDaily
	}
	if !model.IsValidAnalysisMode(mode) {
		writeError(w, http.StatusBadRequest, "invalid_prompt_mode", "prompt_mode must be daily, weekly, or incident")
		return
	}

	periodMinutes := req.PeriodMinutes
	if periodMinutes == 0 {
		periodMinutes = defaultPeriodMinutes(mode)
	}
	if periodMinutes < minPeriodMinutes || periodMinutes > maxPeriodMinutes {
		writeError(w, http.StatusBadRequest, "invalid_period",
			"period_minutes must be between 5 and 43200")
		return
	}
	if mode == model.AnalysisModeIncident && periodMinutes > maxIncidentPeriodMinutes {
		writeError(w, http.StatusBadRequest, "invalid_period",
			"incident mode period_minutes must be 360 or less")
		return
	}

	// Normalize hosts (sort + dedup + trim) before validation so the response
	// error message — and the persisted row — both reflect the canonical set
	// the caller actually meant.
	hosts := model.NormalizeHosts(req.Hosts)
	if len(hosts) > 0 {
		unknown, validateErr := h.validateHostsForFeed(r.Context(), req.Feed, hosts)
		if validateErr != nil {
			LoggerFromContext(r.Context()).Error("validate hosts failed", "feed", req.Feed, "err", validateErr)
			writeError(w, http.StatusInternalServerError, "validate_failed", "failed to validate host scope")
			return
		}
		if len(unknown) > 0 {
			writeError(w, http.StatusBadRequest, "unknown_hosts",
				fmt.Sprintf("hosts not found for feed %s: %v", req.Feed, unknown))
			return
		}
	}

	// period_end is minute-truncated so rapid clicks resolve to the same window
	// and hit the duplicate-active guard (which now includes prompt_mode, so
	// different modes for the same window don't collide).
	periodEnd := time.Now().UTC().Truncate(time.Minute)
	periodStart := periodEnd.Add(-time.Duration(periodMinutes) * time.Minute)

	report, err := h.enqueuer.Enqueue(r.Context(), model.AnalysisReport{
		Feed:        req.Feed,
		PromptMode:  mode,
		Hosts:       hosts,
		PeriodStart: periodStart,
		PeriodEnd:   periodEnd,
	})
	if errors.Is(err, postgres.ErrDuplicateActiveReport) {
		writeError(w, http.StatusConflict, "duplicate_report", "a report for this feed and period is already pending or running")
		return
	}
	if errors.Is(err, worker.ErrQueueFull) {
		writeError(w, http.StatusTooManyRequests, "queue_full", "analysis worker queue is full, try again shortly")
		return
	}
	if err != nil {
		LoggerFromContext(r.Context()).Error("enqueue analysis report failed", "err", err)
		writeError(w, http.StatusInternalServerError, "enqueue_failed", "failed to queue analysis run")
		return
	}

	writeJSONStatus(w, http.StatusCreated, itemResponse{Data: report})
}

// validateHostsForFeed returns the subset of candidates that are not present
// in the feed's host metadata. An empty result means every candidate is
// known; a non-empty result is the list to surface back to the caller as
// the "unknown_hosts" error.
//
// For feed=all the union of srvlog and netlog hosts is used: a hostname only
// has to appear in at least one source to be considered known.
func (h *AnalysisHandler) validateHostsForFeed(ctx context.Context, feed string, candidates []string) ([]string, error) {
	known, err := h.store.ListAnalysisHosts(ctx, feed)
	if err != nil {
		return nil, err
	}
	knownSet := make(map[string]struct{}, len(known))
	for _, k := range known {
		knownSet[k] = struct{}{}
	}
	var unknown []string
	for _, c := range candidates {
		if _, ok := knownSet[c]; !ok {
			unknown = append(unknown, c)
		}
	}
	return unknown, nil
}

// Hosts handles GET /api/v1/analysis/hosts?feed={srvlog|netlog|all}. The
// frontend picker loads this once per feed-selection to populate its
// autocomplete suggestions; the response is intentionally minimal (no per-host
// stats — those live on /api/v1/hosts) so the call is cheap to fire on every
// open of the create-report panel.
func (h *AnalysisHandler) Hosts(w http.ResponseWriter, r *http.Request) {
	feed := r.URL.Query().Get("feed")
	if !model.IsValidAnalysisFeed(feed) {
		writeError(w, http.StatusBadRequest, "invalid_feed", "feed must be netlog, srvlog, or all")
		return
	}
	if (feed == model.AnalysisFeedNetlog || feed == model.AnalysisFeedAll) && !h.netlogEnabled {
		writeError(w, http.StatusBadRequest, "feed_unavailable", "netlog feature is disabled")
		return
	}

	entries, err := h.store.ListAnalysisHostEntries(r.Context(), feed)
	if err != nil {
		if isClientGone(r) {
			return
		}
		LoggerFromContext(r.Context()).Error("list analysis host entries failed", "feed", feed, "err", err)
		writeError(w, http.StatusInternalServerError, "query_failed", "failed to list hosts")
		return
	}

	writeJSON(w, itemResponse{Data: emptySlice(entries)})
}

// Delete handles DELETE /api/v1/analysis/reports/{slug}.
func (h *AnalysisHandler) Delete(w http.ResponseWriter, r *http.Request) {
	slug := chi.URLParam(r, "slug")
	if slug == "" {
		writeError(w, http.StatusBadRequest, "invalid_slug", "slug is required")
		return
	}

	report, err := h.store.GetReportBySlug(r.Context(), slug)
	if errors.Is(err, pgx.ErrNoRows) {
		writeError(w, http.StatusNotFound, "not_found", "report not found")
		return
	}
	if err != nil {
		LoggerFromContext(r.Context()).Error("get analysis report for delete failed", "slug", slug, "err", err)
		writeError(w, http.StatusInternalServerError, "query_failed", "failed to look up report")
		return
	}

	if err := h.store.DeleteReport(r.Context(), report.ID); err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			writeError(w, http.StatusNotFound, "not_found", "report not found")
			return
		}
		LoggerFromContext(r.Context()).Error("delete analysis report failed", "slug", slug, "err", err)
		writeError(w, http.StatusInternalServerError, "delete_failed", "failed to delete report")
		return
	}

	w.WriteHeader(http.StatusNoContent)
}
