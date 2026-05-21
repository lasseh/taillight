// Package analyzer provides AI-powered srvlog analysis using Ollama.
package analyzer

import (
	"context"
	"log/slog"
	"time"

	"github.com/lasseh/taillight/internal/model"
	"github.com/lasseh/taillight/internal/ollama"
)

// Store defines the data access methods needed by the analyzer.
// Methods that query log events accept a feed parameter ("srvlog", "netlog", or "all").
type Store interface {
	GetTopMsgIDs(ctx context.Context, feed string, since time.Time, limit int) ([]model.MsgIDCount, error)
	GetSeverityComparison(ctx context.Context, feed string, currentSince, baselineSince time.Time) (model.SeverityComparison, error)
	GetTopErrorHosts(ctx context.Context, feed string, since time.Time, limit int) ([]model.HostErrorCount, error)
	GetNewMsgIDs(ctx context.Context, feed string, since, baselineSince time.Time) ([]string, error)
	GetEventClusters(ctx context.Context, feed string, since time.Time, windowMinutes int) ([]model.EventCluster, error)
	LookupJuniperRefs(ctx context.Context, names []string) (map[string]model.JuniperNetlogRef, error)
}

// Config holds analyzer configuration. Feed selection is per-run (passed to Run),
// not configured globally.
type Config struct {
	Model       string
	Temperature float64
	NumCtx      int
}

// Result is the output of a single analysis run. Persistence is the caller's
// responsibility (the worker writes it to the report row).
type Result struct {
	PeriodStart      time.Time
	PeriodEnd        time.Time
	Report           string
	PromptTokens     int
	CompletionTokens int
}

// Analyzer orchestrates data gathering, prompt building, and LLM inference.
type Analyzer struct {
	store  Store
	client *ollama.Client
	cfg    Config
	logger *slog.Logger
}

// New creates a new Analyzer.
func New(store Store, client *ollama.Client, cfg Config, logger *slog.Logger) *Analyzer {
	return &Analyzer{
		store:  store,
		client: client,
		cfg:    cfg,
		logger: logger,
	}
}

// Model returns the configured model name. Used by the worker to stamp pending
// rows so the metadata bar can show which model produced (or was attempted on)
// a given report, even before the run completes.
func (a *Analyzer) Model() string {
	return a.cfg.Model
}
