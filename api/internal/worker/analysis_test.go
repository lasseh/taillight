package worker

import (
	"context"
	"errors"
	"io"
	"log/slog"
	"sync"
	"testing"

	"github.com/lasseh/taillight/internal/analyzer"
	"github.com/lasseh/taillight/internal/model"
)

// fakeReportStore implements just enough of ReportStore for fireCompletion
// to be exercised. The CAS counter tracks how many times the worker tried
// to mark a row notified; the first call wins, subsequent calls return
// won=false, matching the production atomic UPDATE semantics.
type fakeReportStore struct {
	mu           sync.Mutex
	notifiedOnce map[int64]bool
	casCalls     int
	casErr       error
}

func newFakeReportStore() *fakeReportStore {
	return &fakeReportStore{notifiedOnce: map[int64]bool{}}
}

func (f *fakeReportStore) MarkReportNotified(_ context.Context, id int64) (bool, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.casCalls++
	if f.casErr != nil {
		return false, f.casErr
	}
	if f.notifiedOnce[id] {
		return false, nil
	}
	f.notifiedOnce[id] = true
	return true, nil
}

// Stubs for the rest of the interface — never called by fireCompletion.
func (f *fakeReportStore) InsertPendingReport(_ context.Context, r model.AnalysisReport) (model.AnalysisReport, error) {
	return r, nil
}
func (f *fakeReportStore) DeleteReport(_ context.Context, _ int64) error { return nil }
func (f *fakeReportStore) GetReport(_ context.Context, _ int64) (model.AnalysisReport, error) {
	return model.AnalysisReport{}, nil
}
func (f *fakeReportStore) MarkReportRunning(_ context.Context, _ int64) error          { return nil }
func (f *fakeReportStore) MarkReportFailed(_ context.Context, _ int64, _ string) error { return nil }
func (f *fakeReportStore) MarkReportCompleted(_ context.Context, _ int64, _ string, _, _ int) error {
	return nil
}

func discardLogger() *slog.Logger {
	return slog.New(slog.NewTextHandler(io.Discard, nil))
}

func TestFireCompletionInvokesCallbackOnce(t *testing.T) {
	store := newFakeReportStore()
	var calls int
	cb := func(_ context.Context, _ model.AnalysisReport) { calls++ }
	w := &Analysis{store: store, logger: discardLogger(), onCompleted: cb}

	report := model.AnalysisReport{ID: 42, Feed: "netlog", PromptMode: "daily"}
	res := analyzer.Result{Report: "body", PromptTokens: 10, CompletionTokens: 20}

	w.fireCompletion(context.Background(), 42, report, res)
	w.fireCompletion(context.Background(), 42, report, res) // simulate retry

	if calls != 1 {
		t.Fatalf("expected callback to fire exactly once, got %d", calls)
	}
	if store.casCalls != 2 {
		t.Fatalf("expected CAS to be attempted twice (won + lost), got %d", store.casCalls)
	}
}

func TestFireCompletionSkipsWhenCallbackNil(t *testing.T) {
	store := newFakeReportStore()
	w := &Analysis{store: store, logger: discardLogger(), onCompleted: nil}

	w.fireCompletion(context.Background(), 1, model.AnalysisReport{ID: 1}, analyzer.Result{})

	if store.casCalls != 0 {
		t.Fatalf("expected no CAS call when callback is nil, got %d", store.casCalls)
	}
}

func TestFireCompletionSkipsOnCASError(t *testing.T) {
	store := newFakeReportStore()
	store.casErr = errors.New("db down")
	var calls int
	cb := func(_ context.Context, _ model.AnalysisReport) { calls++ }
	w := &Analysis{store: store, logger: discardLogger(), onCompleted: cb}

	w.fireCompletion(context.Background(), 1, model.AnalysisReport{ID: 1}, analyzer.Result{})

	if calls != 0 {
		t.Fatalf("expected no callback on CAS error, got %d", calls)
	}
}

func TestFireCompletionDecoratesReport(t *testing.T) {
	store := newFakeReportStore()
	var got model.AnalysisReport
	cb := func(_ context.Context, r model.AnalysisReport) { got = r }
	w := &Analysis{store: store, logger: discardLogger(), onCompleted: cb}

	report := model.AnalysisReport{ID: 7, Feed: "srvlog", PromptMode: "weekly", Status: "running"}
	res := analyzer.Result{Report: "## TL;DR\nfine", PromptTokens: 5, CompletionTokens: 6}

	w.fireCompletion(context.Background(), 7, report, res)

	if got.Status != "completed" {
		t.Errorf("expected status=completed, got %q", got.Status)
	}
	if got.Report != res.Report {
		t.Errorf("expected callback to see report body, got %q", got.Report)
	}
	if got.PromptTokens != 5 || got.CompletionTokens != 6 {
		t.Errorf("expected token counts wired into callback report, got %d/%d", got.PromptTokens, got.CompletionTokens)
	}
	if got.CompletedAt == nil {
		t.Error("expected CompletedAt to be set")
	}
}
