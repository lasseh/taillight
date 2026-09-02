package analyzer

import (
	"context"
	"fmt"
	"time"

	"github.com/lasseh/taillight/internal/metrics"
	"github.com/lasseh/taillight/internal/model"
	"github.com/lasseh/taillight/internal/ollama"
)

// isEmptyData reports whether the gathered period has no current-window
// activity worth narrating. Three signals must all be absent:
//
//   - no top event signatures (the count-grouped query yields no rows when
//     received_at had zero events);
//   - no volume timeline buckets (the time-bucket query has the same shape);
//   - no severity level with a non-zero current rate (the comparison can
//     still carry baseline-only entries, but Current==0 across the board
//     means nothing happened in the current window).
//
// All three together rule out the edge cases where one query happens to
// return a stray row — e.g. an event with an empty key gets filtered out
// of TopMsgIDs but still bumps the volume timeline.
func isEmptyData(data analysisData) bool {
	if len(data.TopMsgIDs) > 0 {
		return false
	}
	if len(data.VolumeTimeline) > 0 {
		return false
	}
	for _, lvl := range data.SeverityComparison.Levels {
		if lvl.Current > 0 {
			return false
		}
	}
	return true
}

// emptyDataBody returns the deterministic markdown body for an empty-window
// short-circuit. The phrasing tracks the scope so a reader skimming the
// report knows immediately whether the silence is "the whole feed is quiet"
// or "the hosts I picked are quiet".
func emptyDataBody(scope model.AnalysisScope) string {
	if scope.IsAllHosts() {
		return "_No events recorded on this feed during this window._\n"
	}
	return "_No events recorded for the scoped host(s) during this window._\n"
}

// prepared is what a feed's gather-and-prompt stage hands to the shared
// model call: the period it covered, whether the window was empty (and the
// body to emit if so), the rendered prompts, and the data-signal counts for
// the "sending prompt" log line.
type prepared struct {
	periodStart time.Time
	periodEnd   time.Time
	empty       bool
	emptyBody   string
	system      string
	user        string
	signals     []any
}

// prepareSyslog gathers and renders a netlog or srvlog run.
func (a *Analyzer) prepareSyslog(ctx context.Context, scope model.AnalysisScope, period time.Duration, periodEnd time.Time, mode string) (prepared, error) {
	data, err := a.gather(ctx, scope, period, periodEnd)
	if err != nil {
		return prepared{}, fmt.Errorf("gather data: %w", err)
	}
	p := prepared{periodStart: data.PeriodStart, periodEnd: data.PeriodEnd}
	if isEmptyData(data) {
		p.empty = true
		p.emptyBody = emptyDataBody(scope)
		return p, nil
	}
	p.system, p.user, err = buildPrompt(data, a.cfg.PromptsDir, mode)
	if err != nil {
		return prepared{}, fmt.Errorf("build prompt: %w", err)
	}
	p.signals = []any{
		"top_msgids", len(data.TopMsgIDs),
		"new_msgids", len(data.NewMsgIDs),
		"event_clusters", len(data.EventClusters),
		"top_error_hosts", len(data.TopErrorHosts),
	}
	return p, nil
}

// prepareAppLog gathers and renders an applog run.
func (a *Analyzer) prepareAppLog(ctx context.Context, scope model.AnalysisScope, period time.Duration, periodEnd time.Time, mode string) (prepared, error) {
	data, err := a.gatherAppLog(ctx, scope, period, periodEnd)
	if err != nil {
		return prepared{}, fmt.Errorf("gather data: %w", err)
	}
	p := prepared{periodStart: data.PeriodStart, periodEnd: data.PeriodEnd}
	if isEmptyAppLogData(data) {
		p.empty = true
		p.emptyBody = emptyAppLogBody(scope)
		return p, nil
	}
	p.system, p.user, err = buildAppLogPrompt(data, a.cfg.PromptsDir, mode)
	if err != nil {
		return prepared{}, fmt.Errorf("build prompt: %w", err)
	}
	p.signals = []any{
		"active_services", data.ActiveServices,
		"ranked_services", len(data.Ranked),
		"new_templates", len(data.NewTemplates),
		"silent_services", len(data.Silent),
		"new_services", len(data.NewServices),
	}
	return p, nil
}

// Run executes a single analysis cycle for the given parameters. Persistence
// is the caller's responsibility — Run returns the assembled Result.
func (a *Analyzer) Run(ctx context.Context, params RunParams) (Result, error) {
	mode := params.Mode
	if mode == "" {
		mode = modeDaily
	}
	kind := reportKind(params.Feed, mode)

	start := time.Now()
	periodEnd := start.UTC().Truncate(time.Minute)

	scope := model.AnalysisScope{Feed: params.Feed, Hosts: params.Hosts, Services: params.Services}
	scoped := !scope.IsAllHosts() || !scope.IsAllServices()

	a.logger.Info("starting analysis run",
		"model", a.cfg.Model,
		"feed", params.Feed,
		"hosts", len(scope.Hosts),
		"services", len(scope.Services),
		"period", params.Period,
		"prompt_mode", mode,
	)

	if err := a.client.Ping(ctx); err != nil {
		metrics.AnalysisRunsTotal.WithLabelValues("failed").Inc()
		return Result{}, fmt.Errorf("ollama not available: %w", err)
	}

	var p prepared
	var err error
	if params.Feed == model.AnalysisFeedApplog {
		p, err = a.prepareAppLog(ctx, scope, params.Period, periodEnd, mode)
	} else {
		p, err = a.prepareSyslog(ctx, scope, params.Period, periodEnd, mode)
	}
	if err != nil {
		metrics.AnalysisRunsTotal.WithLabelValues("failed").Inc()
		return Result{}, err
	}

	// Empty-data short-circuit. Asking a model to narrate the absence of data
	// invites hallucinations ("a small uptick was observed…") because the
	// prompt structure demands a verdict; deterministic text is correct.
	// Skipping the LLM also frees the worker slot during incident-triage
	// bursts when an operator may fire several scoped reports quickly.
	//
	// The persisted row has tokens 0/0 with status=completed — see worker
	// docs for the contract.
	if p.empty {
		metrics.AnalysisRunsTotal.WithLabelValues("completed").Inc()
		metrics.AnalysisDurationSeconds.Observe(time.Since(start).Seconds())
		a.logger.Info("analysis short-circuited: no events in window",
			"feed", params.Feed,
			"prompt_mode", mode,
			"scoped", scoped,
			"hosts", len(scope.Hosts),
			"services", len(scope.Services),
		)
		return Result{
			PeriodStart: p.periodStart,
			PeriodEnd:   p.periodEnd,
			Report:      prependReportHeader(p.emptyBody, kind, p.periodStart, p.periodEnd),
		}, nil
	}

	// Log what's actually reaching the model. The data-signal counts let an
	// operator tell "the prompt arrived empty" (gather returned nothing)
	// from "the prompt had data and the model was lazy" without DB access.
	a.logger.Info("sending prompt to ollama", append([]any{
		"model", a.cfg.Model,
		"prompt_mode", mode,
		"system_bytes", len(p.system),
		"user_bytes", len(p.user),
	}, p.signals...)...)

	messages := []ollama.ChatMessage{
		{Role: "system", Content: p.system},
		{Role: "user", Content: p.user},
	}
	options := ollama.Options{
		Temperature: a.cfg.Temperature,
		NumCtx:      a.cfg.NumCtx,
	}

	resp, err := a.client.Chat(ctx, ollama.ChatRequest{
		Model:    a.cfg.Model,
		Messages: messages,
		Options:  options,
	})
	if err != nil {
		metrics.AnalysisRunsTotal.WithLabelValues("failed").Inc()
		return Result{}, fmt.Errorf("ollama chat: %w", err)
	}

	// Report check: structure (exact section set + order) and first-section
	// content (must contain a status/trend/verdict token, not just the
	// placeholder). On any violation, send one corrective follow-up and
	// prefer whichever reply validates. We never make the report worse — if
	// the retry also fails or the call errors, we keep the original.
	if vErr := validateReport(resp.Message.Content, kind); vErr != nil {
		a.logger.Warn("report failed validation, retrying once",
			"feed", params.Feed,
			"prompt_mode", mode,
			"issue", vErr.Error(),
		)
		retry, rErr := a.client.Chat(ctx, ollama.ChatRequest{
			Model: a.cfg.Model,
			Messages: append(messages,
				ollama.ChatMessage{Role: "assistant", Content: resp.Message.Content},
				ollama.ChatMessage{Role: "user", Content: structureCorrection(vErr, kind)},
			),
			Options: options,
		})
		switch {
		case rErr != nil:
			metrics.AnalysisStructureRetriesTotal.WithLabelValues("retry_error").Inc()
			a.logger.Warn("validation retry chat failed, keeping first reply",
				"feed", params.Feed,
				"prompt_mode", mode,
				"err", rErr.Error(),
			)
		case validateReport(retry.Message.Content, kind) == nil:
			metrics.AnalysisStructureRetriesTotal.WithLabelValues("fixed").Inc()
			a.logger.Info("validation retry fixed the report",
				"feed", params.Feed,
				"prompt_mode", mode,
			)
			// Add the retry's eval counts to the first call's so the token
			// tallies reflect what the run actually cost.
			retry.PromptEvalCount += resp.PromptEvalCount
			retry.EvalCount += resp.EvalCount
			resp = retry
		default:
			metrics.AnalysisStructureRetriesTotal.WithLabelValues("still_invalid").Inc()
			a.logger.Warn("validation retry still invalid, keeping first reply",
				"feed", params.Feed,
				"prompt_mode", mode,
			)
		}
	}

	metrics.AnalysisRunsTotal.WithLabelValues("completed").Inc()
	metrics.AnalysisDurationSeconds.Observe(time.Since(start).Seconds())
	metrics.AnalysisCompletionTokens.Observe(float64(resp.EvalCount))

	a.logger.Info("analysis complete",
		"feed", params.Feed,
		"prompt_mode", mode,
		"duration_ms", time.Since(start).Milliseconds(),
		"prompt_tokens", resp.PromptEvalCount,
		"completion_tokens", resp.EvalCount,
		"completion_bytes", len(resp.Message.Content),
	)

	// Normalize before persisting so every finding is its own markdown block —
	// the models emit one finding per line with no blank line between them,
	// which every renderer collapses into a single wall of text. Doing it here
	// rather than in a renderer means email, printed PDF, and the web report
	// view all read the same from one implementation.
	//
	// Prepend the deterministic briefing header so the markdown body starts
	// with the title block instead of `## TL;DR`. The header lives in code
	// rather than the prompt — dates don't need a model, and a fixed format
	// keeps the H1 stable across reports.
	report := prependReportHeader(
		normalizeReportMarkdown(resp.Message.Content),
		kind, p.periodStart, p.periodEnd,
	)

	return Result{
		PeriodStart:      p.periodStart,
		PeriodEnd:        p.periodEnd,
		Report:           report,
		PromptTokens:     resp.PromptEvalCount,
		CompletionTokens: resp.EvalCount,
	}, nil
}
