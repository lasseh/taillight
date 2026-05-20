package juniperref

import (
	"context"
	"io"
	"log/slog"
	"os"
	"path/filepath"
	"testing"

	"github.com/lasseh/taillight/internal/model"
)

func TestInferOS(t *testing.T) {
	t.Parallel()

	cases := []struct {
		name string
		in   string
		want string
	}{
		{"plain junos", "System_Log_Messages_Junos_OS_25.4R1.xlsx", "junos"},
		{"evolved", "System_Log_Messages_Junos_OS_Evolved_25.4R1.xlsx", "junos-evolved"},
		{"evolved lowercase", "junos_evolved_messages.xlsx", "junos-evolved"},
		{"unrelated", "anything.xlsx", "junos"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			if got := inferOS(tc.in); got != tc.want {
				t.Errorf("inferOS(%q) = %q, want %q", tc.in, got, tc.want)
			}
		})
	}
}

// fakeStore records calls and returns pre-seeded counts.
type fakeStore struct {
	counts       map[string]int64
	upsertCalls  []string // OS values seen
	upsertedRows map[string]int
}

func (f *fakeStore) CountJuniperRefsByOS(_ context.Context, osName string) (int64, error) {
	return f.counts[osName], nil
}

func (f *fakeStore) UpsertJuniperRefs(_ context.Context, refs []model.JuniperNetlogRef) (int64, error) {
	if len(refs) == 0 {
		return 0, nil
	}
	f.upsertCalls = append(f.upsertCalls, refs[0].OS)
	if f.upsertedRows == nil {
		f.upsertedRows = make(map[string]int)
	}
	f.upsertedRows[refs[0].OS] += len(refs)
	return int64(len(refs)), nil
}

func TestAutoImportMissingDirIsNotFatal(t *testing.T) {
	t.Parallel()

	store := &fakeStore{counts: map[string]int64{}}
	logger := slog.New(slog.NewTextHandler(io.Discard, nil))

	err := AutoImport(t.Context(), logger, store, filepath.Join(t.TempDir(), "does-not-exist"))
	if err != nil {
		t.Fatalf("AutoImport with missing dir returned err: %v", err)
	}
	if len(store.upsertCalls) != 0 {
		t.Errorf("expected no upserts, got %d", len(store.upsertCalls))
	}
}

func TestAutoImportEmptyPathDisabled(t *testing.T) {
	t.Parallel()

	store := &fakeStore{counts: map[string]int64{}}
	logger := slog.New(slog.NewTextHandler(io.Discard, nil))

	if err := AutoImport(t.Context(), logger, store, ""); err != nil {
		t.Fatalf("AutoImport with empty dir returned err: %v", err)
	}
	if len(store.upsertCalls) != 0 {
		t.Errorf("expected no upserts, got %d", len(store.upsertCalls))
	}
}

func TestAutoImportSkipsWhenOSPopulated(t *testing.T) {
	t.Parallel()

	dir := t.TempDir()
	// Empty placeholder files — they would fail parse, but should never be opened.
	for _, n := range []string{
		"System_Log_Messages_Junos_OS_25.4R1.xlsx",
		"System_Log_Messages_Junos_OS_Evolved_25.4R1.xlsx",
	} {
		if err := os.WriteFile(filepath.Join(dir, n), nil, 0o600); err != nil {
			t.Fatalf("write placeholder: %v", err)
		}
	}

	store := &fakeStore{counts: map[string]int64{
		"junos":         500,
		"junos-evolved": 200,
	}}
	logger := slog.New(slog.NewTextHandler(io.Discard, nil))

	if err := AutoImport(t.Context(), logger, store, dir); err != nil {
		t.Fatalf("AutoImport returned err: %v", err)
	}
	if len(store.upsertCalls) != 0 {
		t.Errorf("expected zero upserts when both OSes populated, got %v", store.upsertCalls)
	}
}

func TestAutoImportIgnoresNonXLSXAndLockfiles(t *testing.T) {
	t.Parallel()

	dir := t.TempDir()
	for _, n := range []string{
		"README.md",
		"~$lockfile.xlsx",
		".hidden.xlsx",
		"notes.txt",
	} {
		if err := os.WriteFile(filepath.Join(dir, n), nil, 0o600); err != nil {
			t.Fatalf("write %s: %v", n, err)
		}
	}

	store := &fakeStore{counts: map[string]int64{}}
	logger := slog.New(slog.NewTextHandler(io.Discard, nil))

	if err := AutoImport(t.Context(), logger, store, dir); err != nil {
		t.Fatalf("AutoImport returned err: %v", err)
	}
	if len(store.upsertCalls) != 0 {
		t.Errorf("expected no upserts for non-xlsx/lockfile entries, got %v", store.upsertCalls)
	}
}
