package handler

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"net"
	"net/http"
	"net/netip"
	"strconv"
	"strings"
	"time"

	"github.com/go-chi/chi/v5/middleware"

	"github.com/lasseh/taillight/internal/auth"
	"github.com/lasseh/taillight/internal/broker"
	"github.com/lasseh/taillight/internal/metrics"
	"github.com/lasseh/taillight/internal/model"
	"github.com/lasseh/taillight/internal/notification"
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
	// applogMaxAttrsExponent bounds the exponent of each number in attrs.
	// Every float64 value can be written within it.
	applogMaxAttrsExponent = 400
)

var (
	errAttrsTooLarge = errors.New("attrs exceeds 64KB limit")
	errAttrsExponent = errors.New("attrs holds a number with an exponent outside -400 to 400")
)

// invalidLevelMessage lists the canonical levels and every accepted alias,
// derived from the model so the two never drift apart.
var invalidLevelMessage = "level must be DEBUG, INFO, WARN, ERROR, or FATAL (also accepts " +
	strings.Join(model.AppLogLevelAliasNames(), ", ") + ")"

// AppLogIngestRequest is the POST body for log ingestion.
type AppLogIngestRequest struct {
	Logs []AppLogIngestEntry `json:"logs"`
}

// AppLogIngestEntry is a single log entry in an ingest batch.
type AppLogIngestEntry struct {
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
	store       AppLogStore
	broker      *broker.AppLogBroker
	logger      *slog.Logger
	notifEngine *notification.Engine
}

// NewAppLogIngestHandler creates a new AppLogIngestHandler.
func NewAppLogIngestHandler(store AppLogStore, b *broker.AppLogBroker, l *slog.Logger, engine *notification.Engine) *AppLogIngestHandler {
	return &AppLogIngestHandler{store: store, broker: b, logger: l, notifEngine: engine}
}

// Ingest handles POST /api/v1/applog/ingest.
func (h *AppLogIngestHandler) Ingest(w http.ResponseWriter, r *http.Request) {
	body := http.MaxBytesReader(w, r.Body, applogMaxBodySize)
	defer body.Close() //nolint:errcheck // MaxBytesReader close error is not actionable.

	data, err := io.ReadAll(body)
	if err != nil {
		metrics.AppLogIngestErrorsTotal.Inc()
		writeError(w, http.StatusRequestEntityTooLarge, "body_too_large", "request body exceeds 5MB limit")
		return
	}

	var req AppLogIngestRequest
	if err := json.Unmarshal(data, &req); err != nil {
		metrics.AppLogIngestErrorsTotal.Inc()
		writeError(w, http.StatusBadRequest, "invalid_json", "malformed JSON body")
		return
	}

	if len(req.Logs) == 0 {
		metrics.AppLogIngestErrorsTotal.Inc()
		writeError(w, http.StatusBadRequest, "empty_batch", "logs array is empty")
		return
	}
	if len(req.Logs) > applogMaxBatchSize {
		metrics.AppLogIngestErrorsTotal.Inc()
		writeError(w, http.StatusBadRequest, "batch_too_large", fmt.Sprintf("max batch size is %d entries", applogMaxBatchSize))
		return
	}

	// Validate all entries and collect errors so the client sees every problem
	// in one response, not just the first.
	var errs []string
	for i, entry := range req.Logs {
		idx := "logs[" + strconv.Itoa(i) + "]: "
		if entry.Timestamp.IsZero() {
			errs = append(errs, idx+"timestamp is required")
		} else if y := entry.Timestamp.UTC().Year(); y < 1 || y > 9998 {
			// Reads return the timestamp in the server's zone, and JSON
			// encoding fails outside years 0 to 9999. The margin keeps every
			// zone in range.
			errs = append(errs, idx+"timestamp must be within UTC years 1 to 9998")
		}
		if entry.Level == "" {
			errs = append(errs, idx+"level is required")
		} else if normalized, ok := model.NormalizeLevel(entry.Level); !ok {
			errs = append(errs, idx+invalidLevelMessage)
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
		if err := checkAttrs(entry.Attrs); err != nil {
			errs = append(errs, idx+err.Error())
		}
	}
	if len(errs) > 0 {
		metrics.AppLogIngestErrorsTotal.Inc()
		writeError(w, http.StatusBadRequest, "validation_failed", strings.Join(errs, "; "))
		return
	}

	// Server-captured ingest metadata, identical for every entry in the batch.
	// The client IP is resolved by the clientIPMiddleware (from the trusted
	// real_ip_header when behind a proxy, else the TCP peer), never from the
	// request body. The trust model is acceptable here because ingest requires
	// a valid API key.
	sourceIP := resolveSourceIP(middleware.GetClientIP(r.Context()))
	apiKeyID, _ := auth.APIKeyIDFromContext(r.Context()) // zero-value UUID (Valid=false) → SQL NULL for session auth.

	// Convert to model events.
	events := make([]model.AppLogEvent, len(req.Logs))
	for i, entry := range req.Logs {
		events[i] = model.AppLogEvent{
			Timestamp: entry.Timestamp,
			Level:     entry.Level, // Already normalized during validation.
			Service:   replaceNUL(entry.Service),
			Component: replaceNUL(entry.Component),
			Host:      replaceNUL(entry.Host),
			Msg:       replaceNUL(entry.Msg),
			Source:    replaceNUL(entry.Source),
			Attrs:     replaceNULEscapes(entry.Attrs),
			SourceIP:  sourceIP,
			APIKeyID:  apiKeyID,
		}
	}

	// Batch insert — populates ID and ReceivedAt.
	inserted, err := h.store.InsertLogBatch(r.Context(), events)
	if err != nil {
		if isClientGone(r) {
			return
		}
		metrics.AppLogIngestErrorsTotal.Inc()
		LoggerFromContext(r.Context()).Error("insert log batch failed", "err", err, "batch_size", len(events))
		writeError(w, http.StatusInternalServerError, "insert_failed", "failed to store log entries")
		return
	}

	metrics.AppLogIngestBatchesTotal.Inc()
	metrics.AppLogIngestTotal.Add(float64(len(inserted)))

	// Broadcast to SSE clients and notification engine. SSE clients get the
	// attrs-truncated preview to keep the in-browser buffer bounded; the
	// notification engine sees the full event so rules can match on attrs.
	for i := range inserted {
		h.broker.Broadcast(inserted[i].WithAttrsPreview(model.AttrsPreviewLimit))
		if h.notifEngine != nil {
			h.notifEngine.HandleAppLogEvent(inserted[i])
		}
	}

	writeJSONStatus(w, http.StatusAccepted, map[string]int{"accepted": len(inserted)})
}

// checkAttrs applies the attrs size limit to raw, which must be valid JSON.
// Postgres stores attrs as jsonb and prints each number in full positional
// notation, so a number written with an exponent prints longer than it was
// sent. The size counts each such number at its printed length, which is at
// most its raw length plus the magnitude of its exponent.
func checkAttrs(raw json.RawMessage) error {
	size := len(raw)
	if size > applogMaxAttrsLen {
		return errAttrsTooLarge
	}
	dec := json.NewDecoder(bytes.NewReader(raw))
	dec.UseNumber()
	for {
		tok, err := dec.Token()
		if errors.Is(err, io.EOF) {
			return nil
		}
		if err != nil {
			return fmt.Errorf("attrs is not valid JSON: %w", err)
		}
		n, ok := tok.(json.Number)
		if !ok {
			continue
		}
		i := strings.IndexAny(string(n), "eE")
		if i < 0 {
			continue
		}
		exp, err := strconv.ParseInt(string(n[i+1:]), 10, 64)
		if err != nil || exp > applogMaxAttrsExponent || exp < -applogMaxAttrsExponent {
			return errAttrsExponent
		}
		size += int(max(exp, -exp))
		if size > applogMaxAttrsLen {
			return errAttrsTooLarge
		}
	}
}

// replaceNUL swaps NUL bytes for U+FFFD. Postgres text cannot store a NUL, and
// one such entry would otherwise fail the whole batch insert.
func replaceNUL(s string) string {
	return strings.ReplaceAll(s, "\x00", "\ufffd")
}

// replaceNULEscapes swaps every \u0000 escape in raw JSON for \ufffd, in place,
// because Postgres jsonb rejects it for the same reason. The escapes have the
// same length. The scan skips each escaped byte, so the text \\u0000, an
// escaped backslash, is left alone.
func replaceNULEscapes(raw json.RawMessage) json.RawMessage {
	for i := 0; i < len(raw)-1; i++ {
		if raw[i] != '\\' {
			continue
		}
		if bytes.HasPrefix(raw[i:], []byte(`\u0000`)) {
			copy(raw[i:], `\ufffd`)
		}
		i++
	}
	return raw
}

// resolveSourceIP normalizes a resolved client IP for storage in the Postgres
// `inet` source_ip column, returning nil if empty/malformed. The input comes
// from middleware.GetClientIP (already a bare IP); the SplitHostPort fallback
// also tolerates a raw "ip:port" RemoteAddr.
func resolveSourceIP(clientIP string) *string {
	if clientIP == "" {
		return nil
	}
	host, _, err := net.SplitHostPort(clientIP)
	if err != nil {
		host = clientIP // already a bare IP (the common case)
	}
	addr, err := netip.ParseAddr(host)
	if err != nil {
		return nil
	}
	s := addr.String()
	return &s
}
