package main

import (
	"context"
	"fmt"
	"log/slog"
	"os"

	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/spf13/cobra"

	"github.com/lasseh/taillight/internal/config"
	"github.com/lasseh/taillight/internal/juniperref"
	"github.com/lasseh/taillight/internal/postgres"
)

var (
	importFile string
	importOS   string
)

var importCmd = &cobra.Command{
	Use:   "import",
	Short: "Import Juniper syslog reference data from XLSX",
	RunE:  runImport,
}

func init() {
	importCmd.Flags().StringVarP(&importFile, "file", "f", "", "path to Juniper syslog XLSX file")
	importCmd.Flags().StringVarP(&importOS, "os", "o", "", "target OS: junos or junos-evolved")
	cobra.CheckErr(importCmd.MarkFlagRequired("file"))
	cobra.CheckErr(importCmd.MarkFlagRequired("os"))
}

func runImport(_ *cobra.Command, _ []string) error {
	if !juniperref.ValidOS(importOS) {
		return fmt.Errorf("invalid --os value %q: must be junos or junos-evolved", importOS)
	}

	logger := slog.New(slog.NewTextHandler(os.Stdout, &slog.HandlerOptions{Level: slog.LevelInfo}))
	slog.SetDefault(logger)

	file, err := os.Open(importFile)
	if err != nil {
		return fmt.Errorf("open xlsx: %w", err)
	}
	defer func() {
		if cerr := file.Close(); cerr != nil {
			logger.Warn("close xlsx file", "err", cerr)
		}
	}()

	refs, err := juniperref.ParseXLSX(file, importOS)
	if err != nil {
		return fmt.Errorf("parse xlsx: %w", err)
	}
	logger.Info("parsed references from xlsx", "count", len(refs), "file", importFile, "os", importOS)

	if len(refs) == 0 {
		logger.Warn("no references found in file")
		return nil
	}

	cfg, err := config.Load(cfgFile)
	if err != nil {
		return fmt.Errorf("load config: %w", err)
	}

	ctx := context.Background()
	pool, err := pgxpool.New(ctx, cfg.DatabaseURL)
	if err != nil {
		return fmt.Errorf("connect to database: %w", err)
	}
	defer pool.Close()

	store := postgres.NewStore(pool)

	affected, err := store.UpsertJuniperRefs(ctx, refs)
	if err != nil {
		return fmt.Errorf("upsert refs: %w", err)
	}
	logger.Info("import complete", "rows_affected", affected)

	return nil
}
