package analyzer

import (
	"context"
	"fmt"
	"time"

	"github.com/lasseh/taillight/internal/metrics"
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

	a.logger.Info("starting analysis run",
		"model", a.cfg.Model,
		"feed", params.Feed,
		"period", params.Period,
		"prompt_mode", mode,
	)

	if err := a.client.Ping(ctx); err != nil {
		metrics.AnalysisRunsTotal.WithLabelValues("failed").Inc()
		return Result{}, fmt.Errorf("ollama not available: %w", err)
	}

	data, err := a.gather(ctx, params.Feed, params.Period, periodEnd)
	if err != nil {
		metrics.AnalysisRunsTotal.WithLabelValues("failed").Inc()
		return Result{}, fmt.Errorf("gather data: %w", err)
	}

	sysProm, userProm, err := buildPrompt(data, a.cfg.PromptsDir, mode)
	if err != nil {
		metrics.AnalysisRunsTotal.WithLabelValues("failed").Inc()
		return Result{}, fmt.Errorf("build prompt: %w", err)
	}

	a.logger.Info("sending prompt to ollama", "model", a.cfg.Model, "prompt_mode", mode)

	resp, err := a.client.Chat(ctx, ollama.ChatRequest{
		Model: a.cfg.Model,
		Messages: []ollama.ChatMessage{
			{Role: "system", Content: sysProm},
			{Role: "user", Content: userProm},
		},
		Options: ollama.Options{
			Temperature: a.cfg.Temperature,
			NumCtx:      a.cfg.NumCtx,
		},
	})
	if err != nil {
		metrics.AnalysisRunsTotal.WithLabelValues("failed").Inc()
		return Result{}, fmt.Errorf("ollama chat: %w", err)
	}

	metrics.AnalysisRunsTotal.WithLabelValues("completed").Inc()
	metrics.AnalysisDurationSeconds.Observe(time.Since(start).Seconds())

	a.logger.Info("analysis complete",
		"feed", params.Feed,
		"prompt_mode", mode,
		"duration_ms", time.Since(start).Milliseconds(),
		"prompt_tokens", resp.PromptEvalCount,
		"completion_tokens", resp.EvalCount,
	)

	return Result{
		PeriodStart:      data.PeriodStart,
		PeriodEnd:        data.PeriodEnd,
		PromptMode:       mode,
		Report:           resp.Message.Content,
		PromptTokens:     resp.PromptEvalCount,
		CompletionTokens: resp.EvalCount,
	}, nil
}
