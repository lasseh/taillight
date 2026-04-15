// Package logshipper provides an slog.Handler that batches and ships log entries
// to a taillight JSON log ingest endpoint.
package logshipper

import (
	"bytes"
	"context"
	"crypto/tls"
	"encoding/json"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"net/url"
	"os"
	"runtime"
	"sync"
	"sync/atomic"
	"time"
)

// LevelFatal is a custom slog level for fatal/critical log entries.
// Use this when logging events that should be stored as FATAL severity.
const LevelFatal = slog.Level(12) // slog.LevelError + 4

const (
	// defaultBatchSize is the default number of log entries per HTTP request.
	defaultBatchSize = 100

	// defaultFlushPeriod is the default interval at which logs are flushed.
	defaultFlushPeriod = time.Second

	// defaultBufferSize is the default capacity of the buffered channel.
	defaultBufferSize = 1024

	// defaultSendTimeout is the default per-request HTTP timeout.
	defaultSendTimeout = 30 * time.Second

	// httpErrorStatusCode is the minimum status code considered an error.
	httpErrorStatusCode = 400
)

// Secret is a redacting string type for sensitive values like API keys.
// Its String, GoString, and MarshalJSON methods all return "[REDACTED]",
// so accidental logging via %v/%+v/%#v or JSON encoding cannot leak the value.
// Cast to string explicitly at the point of use (e.g. the Authorization header).
type Secret string

// String returns a redacted placeholder so %v/%s never leak the value.
func (Secret) String() string { return "[REDACTED]" }

// GoString returns a redacted placeholder so %#v never leaks the value.
func (Secret) GoString() string { return "[REDACTED]" }

// MarshalJSON returns a redacted placeholder so JSON encoding never leaks the value.
func (Secret) MarshalJSON() ([]byte, error) { return []byte(`"[REDACTED]"`), nil }

// levelString maps any slog.Level to one of the five canonical taillight
// severity strings: DEBUG, INFO, WARN, ERROR, FATAL.
func levelString(l slog.Level) string {
	switch {
	case l >= LevelFatal:
		return "FATAL"
	case l >= slog.LevelError:
		return "ERROR"
	case l >= slog.LevelWarn:
		return "WARN"
	case l >= slog.LevelInfo:
		return "INFO"
	default:
		return "DEBUG"
	}
}

// Config configures the logshipper Handler.
type Config struct {
	Endpoint    string        // POST URL, http:// or https:// only.
	APIKey      Secret        // Bearer token. Redacted in all string/JSON formatting.
	Service     string        // Populates the service field for all entries.
	Component   string        // Optional component field.
	Host        string        // Optional host/instance identifier.
	AddSource   bool          // Include source file:line from the calling function.
	MinLevel    slog.Level    // Minimum level to ship (default: DEBUG, i.e. ship everything).
	BatchSize   int           // Flush when batch reaches this size.
	FlushPeriod time.Duration // Flush at least this often.
	BufferSize  int           // Buffered channel capacity.

	// SendTimeout bounds each HTTP POST (including TLS handshake). Default 30s.
	// Applied as Client.Timeout when Client is nil, and as a per-send context
	// deadline in all cases so a hung endpoint cannot stall the drain loop.
	SendTimeout time.Duration

	// Client is an optional HTTP client. If set, InsecureSkipVerify is ignored
	// and the caller is responsible for TLS config, redirect policy, and the
	// client-level timeout. SendTimeout is still enforced via context.
	Client *http.Client

	// InsecureSkipVerify disables TLS certificate verification for the ingest
	// endpoint. Only honored when Client is nil. Do not enable in production
	// unless you understand the risk (MITM exposure).
	InsecureSkipVerify bool

	// Redact, if non-nil, is called for every attr value before it is written
	// into the outgoing JSON payload. It receives the attr key and the resolved
	// value, and must return the value to ship. Return nil to drop the attr
	// entirely. Use this to scrub PII, tokens, session IDs, etc.
	Redact func(key string, value any) any
}

func (c *Config) setDefaults() error {
	if c.Host == "" {
		c.Host, _ = os.Hostname()
	}
	if c.BatchSize <= 0 {
		c.BatchSize = defaultBatchSize
	}
	if c.FlushPeriod <= 0 {
		c.FlushPeriod = defaultFlushPeriod
	}
	if c.BufferSize <= 0 {
		c.BufferSize = defaultBufferSize
	}
	if c.SendTimeout <= 0 {
		c.SendTimeout = defaultSendTimeout
	}

	u, err := url.Parse(c.Endpoint)
	if err != nil {
		return fmt.Errorf("parse endpoint %q: %w", c.Endpoint, err)
	}
	if u.Scheme != "http" && u.Scheme != "https" {
		return fmt.Errorf("endpoint %q: scheme must be http or https", c.Endpoint)
	}
	if u.Host == "" {
		return fmt.Errorf("endpoint %q: missing host", c.Endpoint)
	}

	if c.Client == nil {
		c.Client = buildClient(c.InsecureSkipVerify, c.SendTimeout)
	}
	return nil
}

// buildClient returns an http.Client with sensible defaults for a log shipper:
// TLS 1.2+, optional skip-verify, bounded timeouts, and CheckRedirect set to
// ErrUseLastResponse so the Bearer token is never replayed to a redirect target.
func buildClient(insecureSkipVerify bool, timeout time.Duration) *http.Client {
	transport := &http.Transport{
		TLSClientConfig: &tls.Config{
			MinVersion:         tls.VersionTLS12,
			InsecureSkipVerify: insecureSkipVerify, //nolint:gosec // Opt-in per Config.InsecureSkipVerify.
		},
		TLSHandshakeTimeout:   10 * time.Second,
		ResponseHeaderTimeout: 15 * time.Second,
		IdleConnTimeout:       90 * time.Second,
		MaxIdleConns:          10,
	}
	return &http.Client{
		Transport: transport,
		Timeout:   timeout,
		CheckRedirect: func(_ *http.Request, _ []*http.Request) error {
			return http.ErrUseLastResponse
		},
	}
}

// state holds fields shared by all Handler instances derived from a single
// New call (via WithAttrs/WithGroup). Counters live here so that metrics
// aggregate across child loggers rather than each With() call starting fresh.
type state struct {
	dropped    atomic.Int64
	sendFailed atomic.Int64
	closing    atomic.Bool

	// shutdownCtx is written by Shutdown before close(done) and read by the
	// drain branch in loop after receiving from done. The close/receive pair
	// provides the happens-before, so no atomic is required for the field.
	shutdownCtx context.Context
}

// Handler implements slog.Handler. It buffers log entries and ships them in
// batches via HTTP POST to the configured ingest endpoint.
type Handler struct {
	cfg       Config
	state     *state
	ch        chan logEntry
	done      chan struct{}
	wg        *sync.WaitGroup
	closeOnce *sync.Once
	preAttrs  []slog.Attr
	groups    []string
	logger    *slog.Logger
}

type logEntry struct {
	Timestamp time.Time       `json:"timestamp"`
	Level     string          `json:"level"`
	Msg       string          `json:"msg"`
	Service   string          `json:"service"`
	Component string          `json:"component,omitempty"`
	Host      string          `json:"host,omitempty"`
	Source    string          `json:"source,omitempty"`
	Attrs     json.RawMessage `json:"attrs,omitempty"`
}

type ingestRequest struct {
	Logs []logEntry `json:"logs"`
}

// New creates and starts a Handler that batches and sends logs in the background.
// It returns an error if Config.Endpoint is missing or has an unsupported scheme.
func New(cfg Config) (*Handler, error) {
	if err := cfg.setDefaults(); err != nil {
		return nil, err
	}

	h := &Handler{
		cfg:       cfg,
		state:     &state{},
		ch:        make(chan logEntry, cfg.BufferSize),
		done:      make(chan struct{}),
		wg:        &sync.WaitGroup{},
		closeOnce: &sync.Once{},
		logger:    slog.Default(),
	}
	h.wg.Add(1)
	go h.loop()
	return h, nil
}

// Enabled returns true if the level is at or above the configured minimum.
func (h *Handler) Enabled(_ context.Context, level slog.Level) bool {
	return level >= h.cfg.MinLevel
}

// Handle converts the slog.Record to a logEntry and pushes it to the channel.
// Entries submitted after Shutdown begins are dropped and counted.
func (h *Handler) Handle(_ context.Context, r slog.Record) error {
	if h.state.closing.Load() {
		h.state.dropped.Add(1)
		return nil
	}

	entry := logEntry{
		Timestamp: r.Time,
		Level:     levelString(r.Level),
		Msg:       r.Message,
		Service:   h.cfg.Service,
		Component: h.cfg.Component,
		Host:      h.cfg.Host,
	}

	// Collect attributes.
	attrs := make(map[string]any)

	// Pre-resolved attrs from WithAttrs.
	for _, a := range h.preAttrs {
		h.setAttr(attrs, h.groups, a)
	}

	// Record attrs.
	r.Attrs(func(a slog.Attr) bool {
		h.setAttr(attrs, h.groups, a)
		return true
	})

	// Resolve source from the record's program counter.
	if h.cfg.AddSource && r.PC != 0 {
		fs := runtime.CallersFrames([]uintptr{r.PC})
		f, _ := fs.Next()
		entry.Source = fmt.Sprintf("%s:%d", f.File, f.Line)
	}

	// Extract source from attrs if not already set (e.g. from a wrapping handler).
	if entry.Source == "" {
		if src, ok := attrs[slog.SourceKey]; ok {
			if s, ok := src.(*slog.Source); ok {
				entry.Source = fmt.Sprintf("%s:%d", s.File, s.Line)
			}
		}
	}
	delete(attrs, slog.SourceKey)

	if len(attrs) > 0 {
		raw, err := json.Marshal(attrs)
		if err != nil {
			return fmt.Errorf("marshal attrs: %w", err)
		}
		entry.Attrs = raw
	}

	select {
	case h.ch <- entry:
	default:
		h.state.dropped.Add(1)
	}

	return nil
}

// WithAttrs returns a new Handler with the given pre-resolved attributes.
// The returned handler shares the parent's state (counters, channel, goroutine)
// so Dropped/SendFailed aggregate across all derived loggers.
func (h *Handler) WithAttrs(attrs []slog.Attr) slog.Handler {
	return &Handler{
		cfg:       h.cfg,
		state:     h.state,
		ch:        h.ch,
		done:      h.done,
		wg:        h.wg,
		closeOnce: h.closeOnce,
		preAttrs:  append(cloneAttrs(h.preAttrs), attrs...),
		groups:    cloneStrings(h.groups),
		logger:    h.logger,
	}
}

// WithGroup returns a new Handler with the given group prefix.
// The returned handler shares the parent's state (see WithAttrs).
func (h *Handler) WithGroup(name string) slog.Handler {
	if name == "" {
		return h
	}
	return &Handler{
		cfg:       h.cfg,
		state:     h.state,
		ch:        h.ch,
		done:      h.done,
		wg:        h.wg,
		closeOnce: h.closeOnce,
		preAttrs:  cloneAttrs(h.preAttrs),
		groups:    append(cloneStrings(h.groups), name),
		logger:    h.logger,
	}
}

// Dropped returns the number of log entries dropped due to a full buffer or
// submission after shutdown began.
func (h *Handler) Dropped() int64 {
	return h.state.dropped.Load()
}

// SendFailed returns the number of log entries that failed to send due to HTTP errors.
func (h *Handler) SendFailed() int64 {
	return h.state.sendFailed.Load()
}

// Shutdown flushes remaining buffered logs and stops the background goroutine.
// It marks the handler as closing (so concurrent Handle calls drop fast),
// records the caller's context for the final drain flush, and waits for the
// drain goroutine to finish or the context to expire.
//
// If ctx expires while a send is in flight, Shutdown returns ctx.Err() and the
// shipper goroutine will exit once the in-flight send finishes (bounded by
// Config.SendTimeout).
func (h *Handler) Shutdown(ctx context.Context) error {
	h.closeOnce.Do(func() {
		h.state.closing.Store(true)
		h.state.shutdownCtx = ctx
		close(h.done)
	})

	finished := make(chan struct{})
	go func() {
		h.wg.Wait()
		close(finished)
	}()

	select {
	case <-finished:
		return nil
	case <-ctx.Done():
		return ctx.Err()
	}
}

func (h *Handler) loop() {
	defer h.wg.Done()

	batch := make([]logEntry, 0, h.cfg.BatchSize)
	ticker := time.NewTicker(h.cfg.FlushPeriod)
	defer ticker.Stop()

	flush := func(ctx context.Context) {
		if len(batch) == 0 {
			return
		}
		if err := h.send(ctx, batch); err != nil {
			h.logger.Warn("logshipper send failed", "error", err, "batch_size", len(batch))
			h.state.sendFailed.Add(int64(len(batch)))
		}
		// Always reset. Failed batches are dropped rather than retained, to
		// keep memory bounded and behavior predictable. Callers that need
		// durability should front the ingest endpoint with a sidecar agent.
		batch = batch[:0]
	}

	for {
		select {
		case entry := <-h.ch:
			batch = append(batch, entry)
			if len(batch) >= h.cfg.BatchSize {
				flush(context.Background())
			}
		case <-ticker.C:
			flush(context.Background())
		case <-h.done:
			drainCtx := h.state.shutdownCtx
			if drainCtx == nil {
				drainCtx = context.Background()
			}
			for {
				select {
				case entry := <-h.ch:
					batch = append(batch, entry)
				default:
					flush(drainCtx)
					return
				}
			}
		}
	}
}

func (h *Handler) send(ctx context.Context, batch []logEntry) error {
	ctx, cancel := context.WithTimeout(ctx, h.cfg.SendTimeout)
	defer cancel()

	body, err := json.Marshal(ingestRequest{Logs: batch})
	if err != nil {
		return fmt.Errorf("marshal batch: %w", err)
	}

	req, err := http.NewRequestWithContext(ctx, http.MethodPost, h.cfg.Endpoint, bytes.NewReader(body))
	if err != nil {
		return fmt.Errorf("create request: %w", err)
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Authorization", "Bearer "+string(h.cfg.APIKey))

	resp, err := h.cfg.Client.Do(req)
	if err != nil {
		return fmt.Errorf("send batch: %w", err)
	}
	defer resp.Body.Close() //nolint:errcheck // Response body close error is not actionable.

	if resp.StatusCode >= httpErrorStatusCode {
		body, _ := io.ReadAll(io.LimitReader(resp.Body, 512))
		return fmt.Errorf("ingest API returned %d: %s", resp.StatusCode, body)
	}
	_, _ = io.Copy(io.Discard, resp.Body) // Drain body to allow connection reuse.
	return nil
}

func (h *Handler) setAttr(m map[string]any, groups []string, a slog.Attr) {
	a.Value = a.Value.Resolve()
	if a.Equal(slog.Attr{}) {
		return
	}

	target := m
	for _, g := range groups {
		sub, ok := target[g].(map[string]any)
		if !ok {
			sub = make(map[string]any)
			target[g] = sub
		}
		target = sub
	}

	if a.Value.Kind() == slog.KindGroup {
		groupAttrs := a.Value.Group()
		if a.Key != "" {
			sub := make(map[string]any)
			target[a.Key] = sub
			target = sub
		}
		for _, ga := range groupAttrs {
			h.setAttr(target, nil, ga)
		}
		return
	}

	var v any
	if a.Value.Kind() == slog.KindDuration {
		v = a.Value.Duration().String()
	} else {
		v = a.Value.Any()
		if e, ok := v.(error); ok {
			v = e.Error()
		} else if _, ok := v.(json.Marshaler); !ok {
			// Use String() for fmt.Stringer types that don't implement
			// json.Marshaler, so *url.URL, *regexp.Regexp etc. serialize readably.
			if s, ok := v.(fmt.Stringer); ok {
				v = s.String()
			}
		}
	}

	if h.cfg.Redact != nil {
		v = h.cfg.Redact(a.Key, v)
		if v == nil {
			return
		}
	}
	target[a.Key] = v
}

func cloneAttrs(attrs []slog.Attr) []slog.Attr {
	if attrs == nil {
		return nil
	}
	c := make([]slog.Attr, len(attrs))
	copy(c, attrs)
	return c
}

func cloneStrings(s []string) []string {
	if s == nil {
		return nil
	}
	c := make([]string, len(s))
	copy(c, s)
	return c
}
