package analyzer

import (
	"context"
	"fmt"
	"time"

	"github.com/lasseh/taillight/internal/metrics"
	"github.com/lasseh/taillight/internal/model"
	"github.com/lasseh/taillight/internal/ollama"
)

// Run executes a single analysis cycle for the given parameters. Persistence
// is the caller's responsibility — Run returns the assembled Result.
func (a *Analyzer) Run(ctx context.Context, params RunParams) (Result, error) {
	mode := params.Mode
	if mode == "" {
		mode = modeDaily
	}

	start := time.Now()
	periodEnd := start.UTC().Truncate(time.Minute)

	scope := model.AnalysisScope{Feed: params.Feed, Hosts: params.Hosts}

	a.logger.Info("starting analysis run",
		"model", a.cfg.Model,
		"feed", params.Feed,
		"hosts", len(scope.Hosts),
		"period", params.Period,
		"prompt_mode", mode,
	)

	if err := a.client.Ping(ctx); err != nil {
		metrics.AnalysisRunsTotal.WithLabelValues("failed").Inc()
		return Result{}, fmt.Errorf("ollama not available: %w", err)
	}

	data, err := a.gather(ctx, scope, params.Period, periodEnd)
	if err != nil {
		metrics.AnalysisRunsTotal.WithLabelValues("failed").Inc()
		return Result{}, fmt.Errorf("gather data: %w", err)
	}

	sysProm, userProm, err := buildPrompt(data, a.cfg.PromptsDir, mode)
	if err != nil {
		metrics.AnalysisRunsTotal.WithLabelValues("failed").Inc()
		return Result{}, fmt.Errorf("build prompt: %w", err)
	}

	// Log what's actually reaching the model. The data-signal counts let an
	// operator tell "the prompt arrived empty" (gather returned nothing)
	// from "the prompt had data and the model was lazy" without DB access.
	a.logger.Info("sending prompt to ollama",
		"model", a.cfg.Model,
		"prompt_mode", mode,
		"system_bytes", len(sysProm),
		"user_bytes", len(userProm),
		"top_msgids", len(data.TopMsgIDs),
		"new_msgids", len(data.NewMsgIDs),
		"event_clusters", len(data.EventClusters),
		"top_error_hosts", len(data.TopErrorHosts),
	)

	messages := []ollama.ChatMessage{
		{Role: "system", Content: sysProm},
		{Role: "user", Content: userProm},
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
	if vErr := validateReport(resp.Message.Content, mode); vErr != nil {
		a.logger.Warn("report failed validation, retrying once",
			"feed", params.Feed,
			"prompt_mode", mode,
			"issue", vErr.Error(),
		)
		retry, rErr := a.client.Chat(ctx, ollama.ChatRequest{
			Model: a.cfg.Model,
			Messages: append(messages,
				ollama.ChatMessage{Role: "assistant", Content: resp.Message.Content},
				ollama.ChatMessage{Role: "user", Content: structureCorrection(vErr, requiredHeaders[mode])},
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
		case validateReport(retry.Message.Content, mode) == nil:
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

	a.logger.Info("analysis complete",
		"feed", params.Feed,
		"prompt_mode", mode,
		"duration_ms", time.Since(start).Milliseconds(),
		"prompt_tokens", resp.PromptEvalCount,
		"completion_tokens", resp.EvalCount,
		"completion_bytes", len(resp.Message.Content),
	)

	// Prepend the deterministic briefing header so the markdown body starts
	// with the title block instead of `## TL;DR`. The header lives in code
	// rather than the prompt — dates don't need a model, and a fixed format
	// keeps the H1 stable across reports.
	report := prependReportHeader(resp.Message.Content, mode, data.PeriodStart, data.PeriodEnd)

	return Result{
		PeriodStart:      data.PeriodStart,
		PeriodEnd:        data.PeriodEnd,
		Report:           report,
		PromptTokens:     resp.PromptEvalCount,
		CompletionTokens: resp.EvalCount,
	}, nil
}
