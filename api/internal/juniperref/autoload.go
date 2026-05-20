package juniperref

import (
	"context"
	"errors"
	"fmt"
	"io/fs"
	"log/slog"
	"os"
	"path/filepath"
	"strings"

	"github.com/lasseh/taillight/internal/model"
)

// RefStore is the minimal store surface AutoImport needs. Both methods are
// satisfied by *postgres.Store.
type RefStore interface {
	CountJuniperRefsByOS(ctx context.Context, osName string) (int64, error)
	UpsertJuniperRefs(ctx context.Context, refs []model.JuniperNetlogRef) (int64, error)
}

// AutoImport scans dir for *.xlsx files and upserts their contents into the
// Juniper reference table. For each file the target OS is inferred from the
// filename (names containing "evolved" → "junos-evolved", else "junos"); a
// file is skipped when its OS already has rows in the table.
//
// A missing directory is logged at Info and returns nil — auto-import is
// best-effort. Per-file errors are logged at Warn and do not abort the
// remaining files.
func AutoImport(ctx context.Context, logger *slog.Logger, store RefStore, dir string) error {
	if logger == nil {
		logger = slog.Default()
	}
	if dir == "" {
		return nil
	}

	entries, err := os.ReadDir(dir)
	if err != nil {
		if errors.Is(err, fs.ErrNotExist) {
			logger.Info("juniper ref directory not found, skipping auto-import", "dir", dir)
			return nil
		}
		return fmt.Errorf("read juniper ref dir %q: %w", dir, err)
	}

	for _, entry := range entries {
		name := entry.Name()
		if entry.IsDir() {
			continue
		}
		if !strings.EqualFold(filepath.Ext(name), ".xlsx") {
			continue
		}
		// Skip Excel lockfiles and hidden files.
		if strings.HasPrefix(name, "~$") || strings.HasPrefix(name, ".") {
			continue
		}

		path := filepath.Join(dir, name)
		osName := inferOS(name)

		if err := importOne(ctx, logger, store, path, name, osName); err != nil {
			logger.Warn("juniper ref auto-import file failed", "file", path, "os", osName, "err", err)
		}
	}

	return nil
}

// inferOS returns the OS identifier implied by an XLSX filename.
func inferOS(filename string) string {
	if strings.Contains(strings.ToLower(filename), "evolved") {
		return "junos-evolved"
	}
	return "junos"
}

// importOne parses a single XLSX and upserts it when the target OS is empty.
func importOne(ctx context.Context, logger *slog.Logger, store RefStore, path, name, osName string) error {
	existing, err := store.CountJuniperRefsByOS(ctx, osName)
	if err != nil {
		return fmt.Errorf("count existing refs: %w", err)
	}
	if existing > 0 {
		logger.Info("juniper reference already loaded, skipping",
			"os", osName, "file", name, "rows", existing)
		return nil
	}

	file, err := os.Open(path)
	if err != nil {
		return fmt.Errorf("open xlsx: %w", err)
	}
	defer func() {
		if cerr := file.Close(); cerr != nil {
			logger.Warn("close xlsx file", "file", path, "err", cerr)
		}
	}()

	refs, err := ParseXLSX(file, osName)
	if err != nil {
		return fmt.Errorf("parse xlsx: %w", err)
	}
	if len(refs) == 0 {
		logger.Warn("juniper reference file produced no rows", "file", name, "os", osName)
		return nil
	}

	affected, err := store.UpsertJuniperRefs(ctx, refs)
	if err != nil {
		return fmt.Errorf("upsert refs: %w", err)
	}
	logger.Info("auto-imported juniper references",
		"os", osName, "file", name, "rows_affected", affected)
	return nil
}
