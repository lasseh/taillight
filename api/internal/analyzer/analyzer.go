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
// Methods that query log events accept an AnalysisScope, which pairs the
// feed ("srvlog", "netlog", or "all") with an optional explicit host filter.
type Store interface {
	GetTopMsgIDs(ctx context.Context, scope model.AnalysisScope, since time.Time, limit int) ([]model.MsgIDCount, error)
	GetSeverityComparison(ctx context.Context, scope model.AnalysisScope, currentSince, baselineSince time.Time) (model.SeverityComparison, error)
	GetTopErrorHosts(ctx context.Context, scope model.AnalysisScope, since time.Time, limit int) ([]model.HostErrorCount, error)
	GetNewMsgIDs(ctx context.Context, scope model.AnalysisScope, since, baselineSince time.Time) ([]string, error)
	GetEventClusters(ctx context.Context, scope model.AnalysisScope, since time.Time, windowMinutes int) ([]model.EventCluster, error)
	GetMsgIDSamples(ctx context.Context, scope model.AnalysisScope, since time.Time, keys []string, perKeyLimit int) (map[string][]model.SampleMessage, error)
	GetTopPrograms(ctx context.Context, scope model.AnalysisScope, since time.Time, limit int) ([]model.ProgramCount, error)
	GetTopFacilities(ctx context.Context, scope model.AnalysisScope, since time.Time, limit int) ([]model.FacilityCount, error)
	GetVolumeTimeline(ctx context.Context, scope model.AnalysisScope, since, until time.Time, bucketMinutes int) ([]model.AnalysisVolumeBucket, error)
	LookupJuniperRefs(ctx context.Context, names []string) (map[string]model.JuniperNetlogRef, error)
}

// Config holds analyzer configuration. Feed selection is per-run (passed to Run),
// not configured globally.
type Config struct {
	Model       string
	Temperature float64
	NumCtx      int
	// PromptsDir, when non-empty, is the directory containing system.md and
	// user.md. Files are reloaded on every Run so edits take effect without a
	// rebuild or restart. Empty means use the embedded default prompts.
	PromptsDir string
}

// RunParams carries the per-run inputs for Analyzer.Run. Grouping them keeps
// the signature stable as new dimensions (mode, scope, host filter) get added.
//
// Hosts is the optional host scope for the run: empty means "all hosts on
// the feed," and non-empty restricts every aggregation (and the baseline)
// to that exact set. Hosts is expected to be already-normalized by the
// caller (the handler/worker pass through the persisted report.Hosts which
// is normalized at insert time); the analyzer itself does not re-normalize.
type RunParams struct {
	Feed   string
	Hosts  []string
	Period time.Duration
	Mode   string // "" defaults to AnalysisModeDaily.
}

// Result is the output of a single analysis run. Persistence is the caller's
// responsibility (the worker writes it to the report row). PromptMode is not
// returned here: the worker already knows the mode from the report row it's
// processing, and the analyzer never substitutes a different mode for the one
// it was asked to render.
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
