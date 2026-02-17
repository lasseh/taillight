// Package logshipper provides an slog.Handler that batches and ships log entries
// to a taillight JSON log ingest endpoint.
package logshipper

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"log/slog"
	"net/http"
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

	// httpErrorStatusCode is the minimum status code considered an error.
	httpErrorStatusCode = 400
)

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
	Endpoint    string        // POST URL, e.g. http://localhost:8080/api/v1/applog/ingest
	APIKey      string        // Bearer token.
	Service     string        // Populates the service field for all entries.
	Component   string        // Optional component field.
	Host        string        // Optional host/instance identifier.
	AddSource   bool          // Include source file:line from the calling function.
	MinLevel    slog.Level    // Minimum level to ship (default: DEBUG, i.e. ship everything).
	BatchSize   int           // Flush when batch reaches this size.
	FlushPeriod time.Duration // Flush at least this often.
	BufferSize  int           // Buffered channel capacity.
	Client      *http.Client  // Optional HTTP client (defaults to http.DefaultClient).
}

func (c *Config) setDefaults() {
	if c.BatchSize <= 0 {
		c.BatchSize = defaultBatchSize
	}
	if c.FlushPeriod <= 0 {
		c.FlushPeriod = defaultFlushPeriod
	}
	if c.BufferSize <= 0 {
		c.BufferSize = defaultBufferSize
	}
	if c.Client == nil {
		c.Client = http.DefaultClient
	}
}

// Handler implements slog.Handler. It buffers log entries and ships them in
// batches via HTTP POST to the configured ingest endpoint.
type Handler struct {
	cfg        Config
	ch         chan logEntry
	done       chan struct{}
	wg         sync.WaitGroup
	closeOnce  sync.Once
	dropped    atomic.Int64
	sendFailed atomic.Int64
	preAttrs   []slog.Attr
	groups     []string
	ctx        context.Context
	cancel     context.CancelFunc
	logger     *slog.Logger
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
func New(cfg Config) *Handler {
	cfg.setDefaults()

	ctx, cancel := context.WithCancel(context.Background())
	h := &Handler{
		cfg:    cfg,
		ch:     make(chan logEntry, cfg.BufferSize),
		done:   make(chan struct{}),
		ctx:    ctx,
		cancel: cancel,
		logger: slog.Default(),
	}
	h.wg.Add(1)
	go h.loop()
	return h
}

// Enabled returns true if the level is at or above the configured minimum.
func (h *Handler) Enabled(_ context.Context, level slog.Level) bool {
	return level >= h.cfg.MinLevel
}

// Handle converts the slog.Record to a logEntry and pushes it to the channel.
func (h *Handler) Handle(_ context.Context, r slog.Record) error {
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
		setAttr(attrs, h.groups, a)
	}

	// Record attrs.
	r.Attrs(func(a slog.Attr) bool {
		setAttr(attrs, h.groups, a)
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
		h.dropped.Add(1)
	}

	return nil
}

// WithAttrs returns a new Handler with the given pre-resolved attributes.
func (h *Handler) WithAttrs(attrs []slog.Attr) slog.Handler {
	return &Handler{
		cfg:      h.cfg,
		ch:       h.ch,
		done:     h.done,
		preAttrs: append(cloneAttrs(h.preAttrs), attrs...),
		groups:   cloneStrings(h.groups),
		ctx:      h.ctx,
		cancel:   h.cancel,
		logger:   h.logger,
	}
}

// WithGroup returns a new Handler with the given group prefix.
func (h *Handler) WithGroup(name string) slog.Handler {
	if name == "" {
		return h
	}
	return &Handler{
		cfg:      h.cfg,
		ch:       h.ch,
		done:     h.done,
		preAttrs: cloneAttrs(h.preAttrs),
		groups:   append(cloneStrings(h.groups), name),
		ctx:      h.ctx,
		cancel:   h.cancel,
		logger:   h.logger,
	}
}

// Dropped returns the number of log entries dropped due to a full buffer.
func (h *Handler) Dropped() int64 {
	return h.dropped.Load()
}

// SendFailed returns the number of log entries that failed to send due to HTTP errors.
func (h *Handler) SendFailed() int64 {
	return h.sendFailed.Load()
}

// Shutdown flushes remaining buffered logs and stops the background goroutine.
// It closes the done channel first to trigger drain, then cancels the context
// after the loop exits so in-flight sends are not aborted.
func (h *Handler) Shutdown(ctx context.Context) error {
	h.closeOnce.Do(func() { close(h.done) })

	finished := make(chan struct{})
	go func() {
		h.wg.Wait()
		close(finished)
	}()

	select {
	case <-finished:
		h.cancel()
		return nil
	case <-ctx.Done():
		h.cancel()
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
			h.sendFailed.Add(int64(len(batch)))
			// Cap retained batch to prevent OOM on persistent failures.
			if len(batch) >= h.cfg.BatchSize*10 {
				batch = batch[:0]
			}
			return
		}
		batch = batch[:0]
	}

	for {
		select {
		case entry := <-h.ch:
			batch = append(batch, entry)
			if len(batch) >= h.cfg.BatchSize {
				flush(h.ctx)
			}
		case <-ticker.C:
			flush(h.ctx)
		case <-h.done:
			// Drain remaining entries using a fresh context for final flush.
			for {
				select {
				case entry := <-h.ch:
					batch = append(batch, entry)
				default:
					flush(context.Background())
					return
				}
			}
		}
	}
}

func (h *Handler) send(ctx context.Context, batch []logEntry) error {
	body, err := json.Marshal(ingestRequest{Logs: batch})
	if err != nil {
		return fmt.Errorf("marshal batch: %w", err)
	}

	req, err := http.NewRequestWithContext(ctx, http.MethodPost, h.cfg.Endpoint, bytes.NewReader(body))
	if err != nil {
		return fmt.Errorf("create request: %w", err)
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Authorization", "Bearer "+h.cfg.APIKey)

	resp, err := h.cfg.Client.Do(req)
	if err != nil {
		return fmt.Errorf("send batch: %w", err)
	}
	defer resp.Body.Close()               //nolint:errcheck // Response body close error is not actionable.
	_, _ = io.Copy(io.Discard, resp.Body) // Drain body to allow connection reuse.

	if resp.StatusCode >= httpErrorStatusCode {
		return fmt.Errorf("ingest API returned %d", resp.StatusCode)
	}
	return nil
}

func setAttr(m map[string]any, groups []string, a slog.Attr) {
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
			setAttr(target, nil, ga)
		}
		return
	}

	if a.Value.Kind() == slog.KindDuration {
		target[a.Key] = a.Value.Duration().String()
		return
	}

	v := a.Value.Any()
	if e, ok := v.(error); ok {
		target[a.Key] = e.Error()
		return
	}
	// Use String() for fmt.Stringer types that don't implement json.Marshaler,
	// so types like *url.URL and *regexp.Regexp serialize readably.
	if _, ok := v.(json.Marshaler); !ok {
		if s, ok := v.(fmt.Stringer); ok {
			target[a.Key] = s.String()
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
