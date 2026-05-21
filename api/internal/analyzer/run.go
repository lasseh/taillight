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

	// Structure check: if the model drifted off the required section template
	// (e.g. emitted "Key Findings / Recommendations / Appendices" instead of
	// the mandated headers), send one corrective follow-up and prefer
	// whichever reply validates. We never make the report worse — if the
	// retry also fails or the call errors, we keep the original.
	if required := requiredHeaders[mode]; len(required) > 0 {
		if vErr := validateStructure(resp.Message.Content, required); vErr != nil {
			a.logger.Warn("report failed structure check, retrying once",
				"feed", params.Feed,
				"prompt_mode", mode,
				"issue", vErr.Error(),
			)
			retry, rErr := a.client.Chat(ctx, ollama.ChatRequest{
				Model: a.cfg.Model,
				Messages: append(messages,
					ollama.ChatMessage{Role: "assistant", Content: resp.Message.Content},
					ollama.ChatMessage{Role: "user", Content: structureCorrection(vErr, required)},
				),
				Options: options,
			})
			switch {
			case rErr != nil:
				metrics.AnalysisStructureRetriesTotal.WithLabelValues("retry_error").Inc()
				a.logger.Warn("structure retry chat failed, keeping first reply",
					"feed", params.Feed,
					"prompt_mode", mode,
					"err", rErr.Error(),
				)
			case validateStructure(retry.Message.Content, required) == nil:
				metrics.AnalysisStructureRetriesTotal.WithLabelValues("fixed").Inc()
				a.logger.Info("structure retry fixed the report",
					"feed", params.Feed,
					"prompt_mode", mode,
				)
				// Add the retry's eval counts to the first call's so the
				// token tallies reflect what the run actually cost.
				retry.PromptEvalCount += resp.PromptEvalCount
				retry.EvalCount += resp.EvalCount
				resp = retry
			default:
				metrics.AnalysisStructureRetriesTotal.WithLabelValues("still_invalid").Inc()
				a.logger.Warn("structure retry still invalid, keeping first reply",
					"feed", params.Feed,
					"prompt_mode", mode,
				)
			}
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
	)

	return Result{
		PeriodStart:      data.PeriodStart,
		PeriodEnd:        data.PeriodEnd,
		Report:           resp.Message.Content,
		PromptTokens:     resp.PromptEvalCount,
		CompletionTokens: resp.EvalCount,
	}, nil
}
