package handler

import (
	"encoding/json"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"strconv"
	"strings"
	"time"

	"github.com/lasseh/taillight/internal/broker"
	"github.com/lasseh/taillight/internal/metrics"
	"github.com/lasseh/taillight/internal/model"
)

const (
	applogMaxBatchSize    = 1000
	applogMaxBodySize     = 5 * 1024 * 1024 // 5 MB.
	applogMaxServiceLen   = 128
	applogMaxComponentLen = 128
	applogMaxHostLen      = 256
	applogMaxSourceLen    = 256
	applogMaxMsgLen       = 64 * 1024 // 64 KB.
	applogMaxAttrsLen     = 64 * 1024 // 64 KB.
)

// ApplogIngestRequest is the POST body for log ingestion.
type ApplogIngestRequest struct {
	Logs []ApplogIngestEntry `json:"logs"`
}

// ApplogIngestEntry is a single log entry in an ingest batch.
type ApplogIngestEntry struct {
	Timestamp time.Time       `json:"timestamp"`
	Level     string          `json:"level"`
	Msg       string          `json:"msg"`
	Service   string          `json:"service"`
	Component string          `json:"component,omitempty"`
	Host      string          `json:"host,omitempty"`
	Source    string          `json:"source,omitempty"`
	Attrs     json.RawMessage `json:"attrs,omitempty"`
}

// AppLogIngestHandler handles POST /api/v1/applog/ingest.
type AppLogIngestHandler struct {
	store  AppLogStore
	broker *broker.ApplogBroker
	logger *slog.Logger
}

// NewAppLogIngestHandler creates a new AppLogIngestHandler.
func NewAppLogIngestHandler(store AppLogStore, b *broker.ApplogBroker, l *slog.Logger) *AppLogIngestHandler {
	return &AppLogIngestHandler{store: store, broker: b, logger: l}
}

// Ingest handles POST /api/v1/applog/ingest.
func (h *AppLogIngestHandler) Ingest(w http.ResponseWriter, r *http.Request) {
	body := http.MaxBytesReader(w, r.Body, applogMaxBodySize)
	defer body.Close() //nolint:errcheck // MaxBytesReader close error is not actionable.

	data, err := io.ReadAll(body)
	if err != nil {
		metrics.ApplogIngestErrorsTotal.Inc()
		writeError(w, http.StatusRequestEntityTooLarge, "body_too_large", "request body exceeds 5MB limit")
		return
	}

	var req ApplogIngestRequest
	if err := json.Unmarshal(data, &req); err != nil {
		metrics.ApplogIngestErrorsTotal.Inc()
		writeError(w, http.StatusBadRequest, "invalid_json", "malformed JSON body")
		return
	}

	if len(req.Logs) == 0 {
		metrics.ApplogIngestErrorsTotal.Inc()
		writeError(w, http.StatusBadRequest, "empty_batch", "logs array is empty")
		return
	}
	if len(req.Logs) > applogMaxBatchSize {
		metrics.ApplogIngestErrorsTotal.Inc()
		writeError(w, http.StatusBadRequest, "batch_too_large", fmt.Sprintf("max batch size is %d entries", applogMaxBatchSize))
		return
	}

	// Validate entries.
	var errs []string
	for i, entry := range req.Logs {
		idx := "logs[" + strconv.Itoa(i) + "]: "
		if entry.Timestamp.IsZero() {
			errs = append(errs, idx+"timestamp is required")
		}
		if entry.Level == "" {
			errs = append(errs, idx+"level is required")
		} else if normalized, ok := model.NormalizeLevel(entry.Level); !ok {
			errs = append(errs, idx+"level must be DEBUG, INFO, WARN, ERROR, or FATAL (also accepts TRACE, WARNING, CRITICAL, PANIC)")
		} else {
			req.Logs[i].Level = normalized
		}
		if entry.Msg == "" {
			errs = append(errs, idx+"msg is required")
		} else if len(entry.Msg) > applogMaxMsgLen {
			errs = append(errs, idx+"msg exceeds 64KB limit")
		}
		if entry.Service == "" {
			errs = append(errs, idx+"service is required")
		} else if len(entry.Service) > applogMaxServiceLen {
			errs = append(errs, idx+"service exceeds 128 char limit")
		}
		if len(entry.Component) > applogMaxComponentLen {
			errs = append(errs, idx+"component exceeds 128 char limit")
		}
		if entry.Host == "" {
			errs = append(errs, idx+"host is required")
		} else if len(entry.Host) > applogMaxHostLen {
			errs = append(errs, idx+"host exceeds 256 char limit")
		}
		if len(entry.Source) > applogMaxSourceLen {
			errs = append(errs, idx+"source exceeds 256 char limit")
		}
		if len(entry.Attrs) > applogMaxAttrsLen {
			errs = append(errs, idx+"attrs exceeds 64KB limit")
		}
	}
	if len(errs) > 0 {
		metrics.ApplogIngestErrorsTotal.Inc()
		writeError(w, http.StatusBadRequest, "validation_failed", strings.Join(errs, "; "))
		return
	}

	// Convert to model events.
	events := make([]model.AppLogEvent, len(req.Logs))
	for i, entry := range req.Logs {
		events[i] = model.AppLogEvent{
			Timestamp: entry.Timestamp,
			Level:     entry.Level, // Already normalized during validation.
			Service:   entry.Service,
			Component: entry.Component,
			Host:      entry.Host,
			Msg:       entry.Msg,
			Source:    entry.Source,
			Attrs:     entry.Attrs,
		}
	}

	// Batch insert — populates ID and ReceivedAt.
	inserted, err := h.store.InsertLogBatch(r.Context(), events)
	if err != nil {
		metrics.ApplogIngestErrorsTotal.Inc()
		LoggerFromContext(r.Context()).Error("insert log batch failed", "err", err, "batch_size", len(events))
		writeError(w, http.StatusInternalServerError, "insert_failed", "failed to store log entries")
		return
	}

	metrics.ApplogIngestBatchesTotal.Inc()
	metrics.ApplogIngestTotal.Add(float64(len(inserted)))

	// Broadcast to SSE clients.
	for i := range inserted {
		h.broker.Broadcast(inserted[i])
	}

	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusAccepted)
	json.NewEncoder(w).Encode(map[string]int{"accepted": len(inserted)}) //nolint:errcheck
}
