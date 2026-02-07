// Package analyzer provides AI-powered syslog analysis using Ollama.
package analyzer

import (
	"context"
	"log/slog"
	"time"

	"github.com/lasseh/taillight/internal/model"
	"github.com/lasseh/taillight/internal/ollama"
)

// Store defines the data access methods needed by the analyzer.
type Store interface {
	GetTopMsgIDs(ctx context.Context, since time.Time, limit int) ([]model.MsgIDCount, error)
	GetSeverityComparison(ctx context.Context, currentSince, baselineSince time.Time) (model.SeverityComparison, error)
	GetTopErrorHosts(ctx context.Context, since time.Time, limit int) ([]model.HostErrorCount, error)
	GetNewMsgIDs(ctx context.Context, since, baselineSince time.Time) ([]string, error)
	GetEventClusters(ctx context.Context, since time.Time, windowMinutes int) ([]model.EventCluster, error)
	LookupJuniperRefs(ctx context.Context, names []string) (map[string]model.JuniperSyslogRef, error)
	InsertReport(ctx context.Context, r model.AnalysisReport) (int64, error)
}

// Config holds analyzer configuration.
type Config struct {
	Model       string
	Temperature float64
	NumCtx      int
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
