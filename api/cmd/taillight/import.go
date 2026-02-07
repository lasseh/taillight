package main

import (
	"context"
	"fmt"
	"log/slog"
	"os"
	"strings"

	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/spf13/cobra"
	"github.com/xuri/excelize/v2"

	"github.com/lasseh/taillight/internal/config"
	"github.com/lasseh/taillight/internal/model"
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
	switch importOS {
	case "junos", "junos-evolved":
	default:
		return fmt.Errorf("invalid --os value %q: must be junos or junos-evolved", importOS)
	}

	logger := slog.New(slog.NewTextHandler(os.Stdout, &slog.HandlerOptions{Level: slog.LevelInfo}))
	slog.SetDefault(logger)

	refs, err := parseXLSX(importFile, importOS)
	if err != nil {
		return fmt.Errorf("parse xlsx: %w", err)
	}
	logger.Info("parsed references from xlsx", "count", len(refs), "file", importFile, "os", importOS)

	if len(refs) == 0 {
		logger.Warn("no references found in file")
		return nil
	}

	cfg, err := config.Load()
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

// parseXLSX reads a Juniper syslog reference XLSX and returns parsed references.
func parseXLSX(path, osName string) ([]model.JuniperSyslogRef, error) {
	f, err := excelize.OpenFile(path)
	if err != nil {
		return nil, fmt.Errorf("open file: %w", err)
	}
	defer func() {
		if err := f.Close(); err != nil {
			slog.Warn("close xlsx file", "err", err)
		}
	}()

	sheet := f.GetSheetName(0)
	if sheet == "" {
		return nil, fmt.Errorf("no sheets found in workbook")
	}

	rows, err := f.GetRows(sheet)
	if err != nil {
		return nil, fmt.Errorf("read rows: %w", err)
	}
	if len(rows) < 2 {
		return nil, fmt.Errorf("file has no data rows (only %d rows)", len(rows))
	}

	// Scan rows for the header row containing a "name" column.
	// The XLSX may have a title row before the actual column headers.
	headerIdx := -1
	colMap := make(map[string]int)
	for ri, row := range rows {
		for _, cell := range row {
			if strings.EqualFold(strings.TrimSpace(cell), "name") {
				headerIdx = ri
				// Build colMap from this row.
				for j, c := range row {
					colMap[strings.ToLower(strings.TrimSpace(c))] = j
				}
				break
			}
		}
		if headerIdx >= 0 {
			break
		}
	}
	if headerIdx < 0 {
		return nil, fmt.Errorf("no header row with 'Name' column found in sheet")
	}

	// Required column: name.
	nameIdx := colMap["name"]

	getIdx := func(names ...string) int {
		for _, n := range names {
			if idx, found := colMap[n]; found {
				return idx
			}
		}
		return -1
	}

	msgIdx := getIdx("message")
	descIdx := getIdx("description")
	typeIdx := getIdx("type")
	sevIdx := getIdx("severity")
	causeIdx := getIdx("cause")
	actionIdx := getIdx("action")

	cellVal := func(row []string, idx int) string {
		if idx < 0 || idx >= len(row) {
			return ""
		}
		return strings.TrimSpace(row[idx])
	}

	var refs []model.JuniperSyslogRef
	for _, row := range rows[headerIdx+1:] {
		name := cellVal(row, nameIdx)
		if name == "" {
			continue
		}
		refs = append(refs, model.JuniperSyslogRef{
			Name:        name,
			Message:     cellVal(row, msgIdx),
			Description: cellVal(row, descIdx),
			Type:        cellVal(row, typeIdx),
			Severity:    cellVal(row, sevIdx),
			Cause:       cellVal(row, causeIdx),
			Action:      cellVal(row, actionIdx),
			OS:          osName,
		})
	}

	return refs, nil
}
