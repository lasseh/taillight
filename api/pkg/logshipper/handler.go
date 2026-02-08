// Package logshipper provides an slog.Handler that batches and ships log entries
// to a taillight JSON log ingest endpoint.
package logshipper

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"net/http"
	"sync"
	"sync/atomic"
	"time"
)

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

// Config configures the logshipper Handler.
type Config struct {
	Endpoint    string        // POST URL, e.g. http://localhost:8080/api/v1/applog/ingest
	APIKey      string        // Bearer token.
	Service     string        // Populates the service field for all entries.
	Component   string        // Optional component field.
	Host        string        // Optional host/instance identifier.
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
	cfg      Config
	ch       chan logEntry
	done     chan struct{}
	wg       sync.WaitGroup
	dropped  atomic.Int64
	preAttrs []slog.Attr
	groups   []string
	ctx      context.Context
	cancel   context.CancelFunc
	logger   *slog.Logger
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
		Level:     r.Level.String(),
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

	// Extract source if present.
	if src, ok := attrs[slog.SourceKey]; ok {
		if s, ok := src.(*slog.Source); ok {
			entry.Source = fmt.Sprintf("%s:%d", s.File, s.Line)
			delete(attrs, slog.SourceKey)
		}
	}

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

// Shutdown flushes remaining buffered logs and stops the background goroutine.
func (h *Handler) Shutdown(ctx context.Context) error {
	h.cancel()
	close(h.done)

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
	defer func() { _ = resp.Body.Close() }()

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

	target[a.Key] = a.Value.Any()
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
