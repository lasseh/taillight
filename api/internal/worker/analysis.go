// Package worker contains background processors that pull work from in-memory
// queues. The analysis worker serializes Ollama-bound report runs.
package worker

import (
	"context"
	"errors"
	"log/slog"
	"time"

	"github.com/jackc/pgx/v5"

	"github.com/lasseh/taillight/internal/analyzer"
	"github.com/lasseh/taillight/internal/model"
)

// QueueDepth caps the number of pending analysis reports waiting to start.
// One global worker drains the queue — a sixth concurrent enqueue is rejected
// with ErrQueueFull so callers can return 429 to clients.
const QueueDepth = 5

// DefaultRunTimeout bounds a single analyzer run. Beyond this, the row is
// marked failed and the worker frees up for the next job.
const DefaultRunTimeout = 20 * time.Minute

// ErrQueueFull is returned by Enqueue when the worker queue is at capacity.
var ErrQueueFull = errors.New("analysis worker queue full")

// ReportStore is the persistence surface the worker writes to. It mirrors the
// store methods on *postgres.Store; defined here so wiring can pass any conforming
// implementation (and for testing).
type ReportStore interface {
	InsertPendingReport(ctx context.Context, r model.AnalysisReport) (model.AnalysisReport, error)
	DeleteReport(ctx context.Context, id int64) error
	GetReport(ctx context.Context, id int64) (model.AnalysisReport, error)
	MarkReportRunning(ctx context.Context, id int64) error
	MarkReportCompleted(ctx context.Context, id int64, body string, promptTokens, completionTokens int) error
	MarkReportFailed(ctx context.Context, id int64, msg string) error
}

// Runner is the analyzer surface the worker depends on. Model is the model
// name to stamp onto pending rows so the UI can display it before the run
// finishes (and on failed runs too).
type Runner interface {
	Run(ctx context.Context, params analyzer.RunParams) (analyzer.Result, error)
	Model() string
}

// Analysis is the queued analysis worker.
type Analysis struct {
	store      ReportStore
	runner     Runner
	logger     *slog.Logger
	work       chan int64
	runTimeout time.Duration
}

// NewAnalysis constructs an analysis worker. Start must be called once before
// Enqueue accepts work.
func NewAnalysis(store ReportStore, runner Runner, logger *slog.Logger) *Analysis {
	return &Analysis{
		store:      store,
		runner:     runner,
		logger:     logger.With("component", "analysis-worker"),
		work:       make(chan int64, QueueDepth),
		runTimeout: DefaultRunTimeout,
	}
}

// Start drains the work queue in a single goroutine until ctx is cancelled.
// The same ctx is the parent of every per-run context, so shutdown propagates.
func (a *Analysis) Start(ctx context.Context) {
	a.logger.Info("analysis worker started", "queue_depth", QueueDepth, "run_timeout", a.runTimeout)
	for {
		select {
		case <-ctx.Done():
			a.logger.Info("analysis worker stopped")
			return
		case id := <-a.work:
			a.process(ctx, id)
		}
	}
}

// Enqueue inserts a pending row and queues it for processing. Returns the
// stored row so the caller can show it immediately. The insert happens first;
// if the queue is full we delete the row and return ErrQueueFull so the user
// never sees an orphaned pending entry.
//
// ctx scopes the insert. The rollback delete uses context.WithoutCancel so a
// client disconnect between insert and the queue-full branch can't leave a
// pending row behind.
func (a *Analysis) Enqueue(ctx context.Context, req model.AnalysisReport) (model.AnalysisReport, error) {
	if req.Model == "" {
		req.Model = a.runner.Model()
	}

	report, err := a.store.InsertPendingReport(ctx, req)
	if err != nil {
		return model.AnalysisReport{}, err
	}

	select {
	case a.work <- report.ID:
		return report, nil
	default:
		// Rollback must survive client disconnect — otherwise a cancelled
		// request between insert success and this branch would leave the row
		// in 'pending' forever (until the next boot reconciler).
		if delErr := a.store.DeleteReport(context.WithoutCancel(ctx), report.ID); delErr != nil {
			a.logger.Warn("rollback delete failed after queue-full",
				"report_id", report.ID, "err", delErr)
		}
		return model.AnalysisReport{}, ErrQueueFull
	}
}

// process runs a single job end-to-end. Errors are logged and persisted to the
// report row; nothing bubbles out so a single bad run can't crash the worker.
func (a *Analysis) process(parent context.Context, id int64) {
	ctx, cancel := context.WithTimeout(parent, a.runTimeout)
	defer cancel()

	report, err := a.store.GetReport(ctx, id)
	if errors.Is(err, pgx.ErrNoRows) {
		// Row was deleted while queued — nothing to do.
		return
	}
	if err != nil {
		a.logger.Error("worker failed to load report", "report_id", id, "err", err)
		return
	}

	if err := a.store.MarkReportRunning(ctx, id); err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			// Row was deleted while queued.
			return
		}
		a.logger.Error("worker failed to mark running", "report_id", id, "err", err)
		return
	}

	period := report.PeriodEnd.Sub(report.PeriodStart)
	res, runErr := a.runner.Run(ctx, analyzer.RunParams{
		Feed:   report.Feed,
		Period: period,
		Mode:   report.PromptMode,
	})
	if runErr != nil {
		msg := truncateErr(runErr, 200)
		if errors.Is(runErr, context.DeadlineExceeded) {
			msg = "analysis timeout"
		}
		if markErr := a.store.MarkReportFailed(parent, id, msg); markErr != nil {
			a.logger.Error("worker failed to mark failed", "report_id", id, "err", markErr)
		}
		a.logger.Warn("analysis run failed", "report_id", id, "feed", report.Feed, "err", runErr)
		return
	}

	if err := a.store.MarkReportCompleted(parent, id, res.Report, res.PromptTokens, res.CompletionTokens); err != nil {
		a.logger.Error("worker failed to mark completed", "report_id", id, "err", err)
	}
}

func truncateErr(err error, n int) string {
	s := err.Error()
	if len(s) <= n {
		return s
	}
	// Trim back to a rune boundary so we never slice in the middle of a UTF-8
	// multi-byte sequence.
	cut := n
	for cut > 0 && (s[cut]&0xC0) == 0x80 {
		cut--
	}
	return s[:cut] + "…"
}
