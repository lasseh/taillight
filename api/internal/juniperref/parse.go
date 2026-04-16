// Package juniperref parses Juniper syslog reference XLSX files into
// model.JuniperNetlogRef records suitable for database upsert.
package juniperref

import (
	"fmt"
	"io"
	"log/slog"
	"strings"

	"github.com/xuri/excelize/v2"

	"github.com/lasseh/taillight/internal/model"
)

// ValidOS reports whether s is a supported Juniper OS identifier.
func ValidOS(s string) bool {
	return s == "junos" || s == "junos-evolved"
}

// ParseXLSX reads a Juniper syslog reference XLSX from reader and returns the
// parsed references tagged with osName. The Excel file is expected to have a
// header row containing a "name" column, optionally preceded by a title row;
// recognized columns are: name, message, description, type, severity, cause,
// action. Rows without a name are skipped.
func ParseXLSX(reader io.Reader, osName string) ([]model.JuniperNetlogRef, error) {
	f, err := excelize.OpenReader(reader)
	if err != nil {
		return nil, fmt.Errorf("open xlsx: %w", err)
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

	var refs []model.JuniperNetlogRef
	for _, row := range rows[headerIdx+1:] {
		name := cellVal(row, nameIdx)
		if name == "" {
			continue
		}
		refs = append(refs, model.JuniperNetlogRef{
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
