package handler

import (
	"context"
	"encoding/csv"
	"fmt"
	"net/http"
	"strconv"
	"strings"
	"time"

	"github.com/lasseh/taillight/internal/model"
)

const (
	exportPageSize = 1000
	exportMaxRows  = 100_000
	exportMaxSpan  = 30 * 24 * time.Hour // 30 days.
	exportFlushDL  = 30 * time.Second    // Write deadline per page.
)

// ExportSrvlogStore is the subset of SrvlogStore needed for export.
type ExportSrvlogStore interface {
	ListSrvlogs(ctx context.Context, f model.SrvlogFilter, cursor *model.Cursor, limit int) ([]model.SrvlogEvent, *model.Cursor, error)
}

// ExportNetlogStore is the subset of NetlogStore needed for export.
type ExportNetlogStore interface {
	ListNetlogs(ctx context.Context, f model.NetlogFilter, cursor *model.Cursor, limit int) ([]model.NetlogEvent, *model.Cursor, error)
}

// ExportAppLogStore is the subset of AppLogStore needed for export.
type ExportAppLogStore interface {
	ListAppLogs(ctx context.Context, f model.AppLogFilter, cursor *model.Cursor, limit int) ([]model.AppLogEvent, *model.Cursor, error)
}

// ExportHandler serves CSV export endpoints for all log types.
type ExportHandler struct {
	srvlog ExportSrvlogStore
	netlog ExportNetlogStore
	applog ExportAppLogStore
}

// NewExportHandler creates a new ExportHandler.
func NewExportHandler(srvlog ExportSrvlogStore, netlog ExportNetlogStore, applog ExportAppLogStore) *ExportHandler {
	return &ExportHandler{srvlog: srvlog, netlog: netlog, applog: applog}
}

// ExportSrvlogs handles GET /api/v1/srvlog/export.
func (h *ExportHandler) ExportSrvlogs(w http.ResponseWriter, r *http.Request) {
	filter, err := model.ParseSrvlogFilter(r)
	if err != nil {
		writeError(w, http.StatusBadRequest, "invalid_filter", err.Error())
		return
	}
	if err := validateExportTimeRange(filter.From, filter.To); err != nil {
		writeError(w, http.StatusBadRequest, "invalid_time_range", err.Error())
		return
	}

	setExportHeaders(w, exportFilename("srvlog", filter.Hostname))
	cw := csv.NewWriter(w)
	cw.Write(srvlogCSVHeader) //nolint:errcheck // CSV write to response; errors caught by Flush.
	cw.Flush()

	rc := http.NewResponseController(w)
	var cursor *model.Cursor
	total := 0
	for {
		if r.Context().Err() != nil {
			return
		}
		_ = rc.SetWriteDeadline(time.Now().Add(exportFlushDL))

		events, next, err := h.srvlog.ListSrvlogs(r.Context(), filter, cursor, exportPageSize)
		if err != nil {
			if isClientGone(r) {
				return
			}
			LoggerFromContext(r.Context()).Error("export srvlogs failed", "err", err)
			return // Headers already sent; cannot write error response.
		}
		for i := range events {
			cw.Write(srvlogToRecord(&events[i])) //nolint:errcheck // CSV write to response; errors caught by Flush.
		}
		cw.Flush()

		total += len(events)
		if next == nil || total >= exportMaxRows {
			break
		}
		cursor = next
	}
}

// ExportNetlogs handles GET /api/v1/netlog/export.
func (h *ExportHandler) ExportNetlogs(w http.ResponseWriter, r *http.Request) {
	filter, err := model.ParseNetlogFilter(r)
	if err != nil {
		writeError(w, http.StatusBadRequest, "invalid_filter", err.Error())
		return
	}
	if err := validateExportTimeRange(filter.From, filter.To); err != nil {
		writeError(w, http.StatusBadRequest, "invalid_time_range", err.Error())
		return
	}

	setExportHeaders(w, exportFilename("netlog", filter.Hostname))
	cw := csv.NewWriter(w)
	cw.Write(srvlogCSVHeader) //nolint:errcheck // CSV write to response; errors caught by Flush.
	cw.Flush()

	rc := http.NewResponseController(w)
	var cursor *model.Cursor
	total := 0
	for {
		if r.Context().Err() != nil {
			return
		}
		_ = rc.SetWriteDeadline(time.Now().Add(exportFlushDL))

		events, next, err := h.netlog.ListNetlogs(r.Context(), filter, cursor, exportPageSize)
		if err != nil {
			if isClientGone(r) {
				return
			}
			LoggerFromContext(r.Context()).Error("export netlogs failed", "err", err)
			return
		}
		for i := range events {
			cw.Write(netlogToRecord(&events[i])) //nolint:errcheck // CSV write to response; errors caught by Flush.
		}
		cw.Flush()

		total += len(events)
		if next == nil || total >= exportMaxRows {
			break
		}
		cursor = next
	}
}

// ExportAppLogs handles GET /api/v1/applog/export.
func (h *ExportHandler) ExportAppLogs(w http.ResponseWriter, r *http.Request) {
	filter, err := model.ParseAppLogFilter(r)
	if err != nil {
		writeError(w, http.StatusBadRequest, "invalid_filter", err.Error())
		return
	}
	if err := validateExportTimeRange(filter.From, filter.To); err != nil {
		writeError(w, http.StatusBadRequest, "invalid_time_range", err.Error())
		return
	}

	setExportHeaders(w, exportFilename("applog", filter.Service))
	cw := csv.NewWriter(w)
	cw.Write(applogCSVHeader) //nolint:errcheck // CSV write to response; errors caught by Flush.
	cw.Flush()

	rc := http.NewResponseController(w)
	var cursor *model.Cursor
	total := 0
	for {
		if r.Context().Err() != nil {
			return
		}
		_ = rc.SetWriteDeadline(time.Now().Add(exportFlushDL))

		events, next, err := h.applog.ListAppLogs(r.Context(), filter, cursor, exportPageSize)
		if err != nil {
			if isClientGone(r) {
				return
			}
			LoggerFromContext(r.Context()).Error("export applogs failed", "err", err)
			return
		}
		for i := range events {
			cw.Write(applogToRecord(&events[i])) //nolint:errcheck // CSV write to response; errors caught by Flush.
		}
		cw.Flush()

		total += len(events)
		if next == nil || total >= exportMaxRows {
			break
		}
		cursor = next
	}
}

// validateExportTimeRange checks that from and to are both set and within the max span.
func validateExportTimeRange(from, to *time.Time) error {
	if from == nil || to == nil {
		return fmt.Errorf("both 'from' and 'to' query parameters are required for export")
	}
	if to.Before(*from) {
		return fmt.Errorf("'to' must be after 'from'")
	}
	if to.Sub(*from) > exportMaxSpan {
		return fmt.Errorf("time range must not exceed 30 days")
	}
	return nil
}

// setExportHeaders sets Content-Type and Content-Disposition for a CSV download.
func setExportHeaders(w http.ResponseWriter, filename string) {
	w.Header().Set("Content-Type", "text/csv; charset=utf-8")
	w.Header().Set("Content-Disposition", fmt.Sprintf("attachment; filename=%q", filename))
}

// exportFilename generates a download filename like "srvlog-web01-2026-04-10T14-30-00Z.csv".
func exportFilename(logType, filterHint string) string {
	ts := time.Now().UTC().Format("2006-01-02T15-04-05Z")
	if filterHint != "" {
		safe := strings.Map(func(r rune) rune {
			if r == '/' || r == '\\' || r == '"' || r == '*' {
				return '-'
			}
			return r
		}, filterHint)
		return fmt.Sprintf("%s-%s-%s.csv", logType, safe, ts)
	}
	return fmt.Sprintf("%s-%s.csv", logType, ts)
}

// CSV column definitions.

var srvlogCSVHeader = []string{
	"id", "received_at", "reported_at", "hostname", "fromhost_ip",
	"programname", "msgid", "severity", "severity_label",
	"facility", "facility_label", "syslogtag", "message",
}

func srvlogToRecord(e *model.SrvlogEvent) []string {
	return []string{
		strconv.FormatInt(e.ID, 10),
		e.ReceivedAt.Format(time.RFC3339Nano),
		e.ReportedAt.Format(time.RFC3339Nano),
		e.Hostname,
		e.FromhostIP,
		e.Programname,
		e.MsgID,
		strconv.Itoa(e.Severity),
		e.SeverityLabel,
		strconv.Itoa(e.Facility),
		e.FacilityLabel,
		e.SyslogTag,
		e.Message,
	}
}

func netlogToRecord(e *model.NetlogEvent) []string {
	return []string{
		strconv.FormatInt(e.ID, 10),
		e.ReceivedAt.Format(time.RFC3339Nano),
		e.ReportedAt.Format(time.RFC3339Nano),
		e.Hostname,
		e.FromhostIP,
		e.Programname,
		e.MsgID,
		strconv.Itoa(e.Severity),
		e.SeverityLabel,
		strconv.Itoa(e.Facility),
		e.FacilityLabel,
		e.SyslogTag,
		e.Message,
	}
}

var applogCSVHeader = []string{
	"id", "received_at", "timestamp", "level", "service",
	"component", "host", "msg", "source", "attrs",
}

func applogToRecord(e *model.AppLogEvent) []string {
	return []string{
		strconv.FormatInt(e.ID, 10),
		e.ReceivedAt.Format(time.RFC3339Nano),
		e.Timestamp.Format(time.RFC3339Nano),
		e.Level,
		e.Service,
		e.Component,
		e.Host,
		e.Msg,
		e.Source,
		string(e.Attrs),
	}
}
