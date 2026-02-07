package analyzer

import (
	"fmt"
	"time"

	"github.com/lasseh/taillight/internal/metrics"
	"github.com/lasseh/taillight/internal/model"
	"github.com/lasseh/taillight/internal/ollama"

	"context"
)

// Run executes a full analysis cycle: gather → prompt → LLM → store.
// Returns the stored report ID.
func (a *Analyzer) Run(ctx context.Context) (int64, error) {
	start := time.Now()

	a.logger.Info("starting analysis run", "model", a.cfg.Model)

	// Check Ollama availability.
	if err := a.client.Ping(ctx); err != nil {
		metrics.AnalysisRunsTotal.WithLabelValues("failed").Inc()
		return 0, fmt.Errorf("ollama not available: %w", err)
	}

	// Gather data.
	data, err := a.gather(ctx)
	if err != nil {
		metrics.AnalysisRunsTotal.WithLabelValues("failed").Inc()
		return 0, fmt.Errorf("gather data: %w", err)
	}

	// Build prompt.
	sysProm, userProm, err := buildPrompt(data)
	if err != nil {
		metrics.AnalysisRunsTotal.WithLabelValues("failed").Inc()
		return 0, fmt.Errorf("build prompt: %w", err)
	}

	a.logger.Info("sending prompt to ollama", "model", a.cfg.Model)

	// Call Ollama.
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
		return 0, fmt.Errorf("ollama chat: %w", err)
	}

	durationMS := time.Since(start).Milliseconds()

	// Store report.
	report := model.AnalysisReport{
		Model:            a.cfg.Model,
		PeriodStart:      data.PeriodStart,
		PeriodEnd:        data.PeriodEnd,
		Report:           resp.Message.Content,
		PromptTokens:     resp.PromptEvalCount,
		CompletionTokens: resp.EvalCount,
		DurationMS:       durationMS,
		Status:           "completed",
	}

	id, err := a.store.InsertReport(ctx, report)
	if err != nil {
		metrics.AnalysisRunsTotal.WithLabelValues("failed").Inc()
		return 0, fmt.Errorf("store report: %w", err)
	}

	metrics.AnalysisRunsTotal.WithLabelValues("completed").Inc()
	metrics.AnalysisDurationSeconds.Observe(time.Since(start).Seconds())

	a.logger.Info("analysis complete",
		"report_id", id,
		"duration_ms", durationMS,
		"prompt_tokens", resp.PromptEvalCount,
		"completion_tokens", resp.EvalCount,
	)

	return id, nil
}
