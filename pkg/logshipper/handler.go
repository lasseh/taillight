// Package logshipper provides an slog.Handler that batches and ships log entries
// to a taillight JSON log ingest endpoint.
package logshipper

import (
	"bytes"
	"context"
	"crypto/tls"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"net/url"
	"os"
	"runtime"
	"strings"
	"sync"
	"sync/atomic"
	"time"
	"unicode/utf8"
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

	// maxMsgBytes is the taillight ingest API's per-entry cap on the msg
	// field (64 KB). Messages longer than this are truncated client-side so
	// one oversized entry cannot make the server reject the whole batch.
	maxMsgBytes = 64 * 1024

	// maxAttrsBytes is the ingest API's per-entry cap on the encoded attrs
	// (64 KB). Attrs over it are replaced by a stub for the same reason.
	maxAttrsBytes = 64 * 1024

	// maxServiceBytes, maxComponentBytes and maxHostBytes are the ingest
	// API's caps on those fields. The values are fixed per Handler, so New
	// rejects a config over a cap instead of having every batch rejected.
	maxServiceBytes   = 128
	maxComponentBytes = 128
	maxHostBytes      = 256

	// maxSourceBytes is the ingest API's cap on the source field. A longer
	// file:line source is shortened from the front.
	maxSourceBytes = 256

	// sourceCutPrefix marks a source shortened from the front.
	sourceCutPrefix = "…"

	// emptyMsgPlaceholder is shipped in place of an empty message, which the
	// ingest API rejects.
	emptyMsgPlaceholder = "(empty)"

	// truncationSuffix marks string values truncated to fit a byte cap.
	truncationSuffix = "…[truncated]"
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
	Endpoint  string // POST URL, http:// or https:// only.
	APIKey    Secret // Bearer token. Redacted in all string/JSON formatting.
	Service   string // Required. Populates the service field for all entries; the ingest API rejects entries without one.
	Component string // Optional component field.
	Host      string // Host/instance identifier. Defaults to os.Hostname(); the ingest API rejects entries without one.
	AddSource bool   // Include source file:line from the calling function.

	// MinLevel is the minimum level to ship. The zero value is slog.LevelInfo
	// (0), so Debug-level records are filtered out by default. Set this
	// explicitly to slog.LevelDebug to ship everything, or slog.LevelWarn /
	// slog.LevelError to reduce volume.
	MinLevel    slog.Level
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

	// MaxAttrBytes caps the length of any string attr value (in bytes).
	// Values longer than this are truncated with a "…[truncated]" suffix to
	// protect buffer memory and keep HTTP payloads bounded. The zero value
	// disables truncation (compatible with earlier releases). A recommended
	// production setting is 8192–16384.
	//
	// Only string values are truncated; other types (numbers, bools, nested
	// groups, json.Marshaler) are unaffected. The truncation is measured in
	// bytes on the UTF-8 representation and cuts at a byte boundary; it may
	// therefore split a multi-byte character — acceptable for a safety
	// backstop, not intended as a general-purpose string limiter.
	MaxAttrBytes int
}

// osHostname is os.Hostname, replaced in tests to exercise lookup failures.
var osHostname = os.Hostname

func (c *Config) setDefaults() error {
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

	if c.Service == "" {
		return errors.New("service is required: the ingest API rejects entries with an empty service")
	}
	if len(c.Service) > maxServiceBytes {
		return fmt.Errorf("service is %d bytes: the ingest API rejects entries over %d", len(c.Service), maxServiceBytes)
	}
	if len(c.Component) > maxComponentBytes {
		return fmt.Errorf("component is %d bytes: the ingest API rejects entries over %d", len(c.Component), maxComponentBytes)
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

	// Resolved after the static checks so a lookup failure cannot mask them.
	if c.Host == "" {
		// os.Hostname can also return an empty name without an error.
		host, err := osHostname()
		if err != nil {
			return fmt.Errorf("host is required: resolve hostname: %w", err)
		}
		if host == "" {
			return errors.New("host is required: the ingest API rejects entries with an empty host, and the system hostname is empty")
		}
		c.Host = host
	}
	if len(c.Host) > maxHostBytes {
		return fmt.Errorf("host is %d bytes: the ingest API rejects entries over %d", len(c.Host), maxHostBytes)
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
	dropped      atomic.Int64
	sendFailed   atomic.Int64
	encodeFailed atomic.Int64
	closing      atomic.Bool

	// mu serializes Shutdown against in-flight Handle calls.
	// Handle holds a read lock (taken with TryRLock, so it never queues
	// behind Shutdown) for the duration of its closing-check + channel push.
	// Shutdown takes Lock as a barrier to wait for those calls to finish, so
	// no entry can be added to h.ch after close(h.done). This fixes a
	// lost-entry race where a concurrent Handle could push an entry after
	// the drain loop had already emptied the channel.
	mu sync.RWMutex

	// shutdownCtx is written by Shutdown before it starts the barrier
	// goroutine that closes done, and read by the drain branch in loop after
	// receiving from done. The go statement and the close/receive pair
	// provide the happens-before, so no atomic is required for the field.
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

	// goas holds the WithGroup and WithAttrs calls in order, so attrs added
	// by a With call land under the groups open at that call, not later ones.
	goas []groupOrAttrs

	// intLogger emits internal diagnostics (send failures) to stderr. It is
	// deliberately NOT slog.Default(), to avoid a feedback loop if callers
	// later replace the default with a logger routed through this shipper.
	intLogger *slog.Logger
}

// groupOrAttrs is one WithGroup call (group set) or one WithAttrs call.
type groupOrAttrs struct {
	group string
	attrs []slog.Attr
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
// It returns an error if Config.Service is empty, if Config.Host is empty and
// the system hostname cannot be resolved, if Service, Component or Host is
// over the ingest API's length limit, or if Config.Endpoint is missing or has
// an unsupported scheme.
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
		intLogger: slog.New(slog.NewTextHandler(os.Stderr, &slog.HandlerOptions{Level: slog.LevelWarn})),
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
	// Hold the read side of the shutdown barrier for the entire call. This
	// guarantees that either (a) we observe closing=true and drop, or
	// (b) we complete our channel push before Shutdown's Lock can return.
	// Either way, no entry is lost silently after shutdown.
	//
	// Only Shutdown takes the write side, after setting closing, so a failed
	// TryRLock means Shutdown is waiting on the barrier: drop instead of
	// queueing behind it. That keeps Handle from blocking during Shutdown,
	// including a call made from inside a Redact or LogValuer callback.
	if !h.state.mu.TryRLock() {
		h.state.dropped.Add(1)
		return nil
	}
	defer h.state.mu.RUnlock()

	if h.state.closing.Load() {
		h.state.dropped.Add(1)
		return nil
	}

	// slog allows a zero time and an empty message, but the ingest API
	// rejects the whole batch for either.
	ts := r.Time
	if ts.IsZero() {
		ts = time.Now()
	}
	msg := r.Message
	if msg == "" {
		msg = emptyMsgPlaceholder
	}

	entry := logEntry{
		Timestamp: ts,
		Level:     levelString(r.Level),
		Msg:       truncateMsg(msg),
		Service:   h.cfg.Service,
		Component: h.cfg.Component,
		Host:      h.cfg.Host,
	}

	attrs := make(map[string]any, r.NumAttrs())

	// Attrs from WithAttrs, each under the groups open when it was added.
	var groups []string
	for _, goa := range h.goas {
		if goa.group != "" {
			groups = append(groups, goa.group)
			continue
		}
		for _, a := range goa.attrs {
			h.setAttr(attrs, groups, a)
		}
	}

	// Record attrs go under every group.
	r.Attrs(func(a slog.Attr) bool {
		h.setAttr(attrs, groups, a)
		return true
	})

	// Resolve source from the record's program counter when AddSource is set.
	// (The historical fallback of scraping a *slog.Source from attrs never
	// fires in practice — slog resolves Source via LogValuer into a Group —
	// so it has been removed.)
	if h.cfg.AddSource && r.PC != 0 {
		fs := runtime.CallersFrames([]uintptr{r.PC})
		f, _ := fs.Next()
		entry.Source = shortenSource(fmt.Sprintf("%s:%d", f.File, f.Line))
	}

	if len(attrs) > 0 {
		raw, err := json.Marshal(attrs)
		switch {
		case err != nil:
			// Don't lose the record — ship a stub payload and count the
			// encode failure so it shows up in EncodeFailed(). slog
			// discards Handle errors, so returning one here would be a
			// silent drop.
			h.state.encodeFailed.Add(1)
			entry.Attrs = encodeErrorPayload(err)
		case len(raw) > maxAttrsBytes:
			// The server measures these same bytes and would reject the
			// whole batch; ship the record with a stub instead.
			h.state.encodeFailed.Add(1)
			entry.Attrs = attrsDroppedPayload(len(raw))
		default:
			entry.Attrs = raw
		}
	}

	select {
	case h.ch <- entry:
	default:
		h.state.dropped.Add(1)
	}

	return nil
}

// truncateMsg makes msg valid UTF-8 and caps it at maxMsgBytes (the ingest
// API's per-entry limit), so an oversized message is truncated instead of the
// server rejecting the whole batch. Invalid bytes are replaced before the
// length check: JSON encoding would otherwise turn each one into a 3-byte
// U+FFFD after it, and the server measures the decoded message. The cut backs
// up to a rune boundary so the result stays valid UTF-8.
func truncateMsg(msg string) string {
	msg = strings.ToValidUTF8(msg, string(utf8.RuneError))
	if len(msg) <= maxMsgBytes {
		return msg
	}
	cut := maxMsgBytes - len(truncationSuffix)
	for cut > 0 && !utf8.RuneStart(msg[cut]) {
		cut--
	}
	return msg[:cut] + truncationSuffix
}

// shortenSource makes a file:line source valid UTF-8 and caps it at
// maxSourceBytes, keeping the tail (file name and line), which is the useful
// part of a long build path. The cut moves forward to a rune boundary.
func shortenSource(source string) string {
	source = strings.ToValidUTF8(source, string(utf8.RuneError))
	if len(source) <= maxSourceBytes {
		return source
	}
	cut := len(source) - (maxSourceBytes - len(sourceCutPrefix))
	for cut < len(source) && !utf8.RuneStart(source[cut]) {
		cut++
	}
	return sourceCutPrefix + source[cut:]
}

// encodeErrorPayload builds a JSON object recording that the original attrs
// failed to marshal. Kept as a helper so the format is consistent and the
// error string is safely escaped.
func encodeErrorPayload(err error) json.RawMessage {
	b, mErr := json.Marshal(map[string]string{"_encode_error": err.Error()})
	if mErr != nil {
		// Last resort: a static payload that always encodes cleanly.
		return json.RawMessage(`{"_encode_error":"unrepresentable"}`)
	}
	return b
}

// attrsDroppedPayload builds the stub shipped in place of attrs over
// maxAttrsBytes. It records their encoded size.
func attrsDroppedPayload(size int) json.RawMessage {
	return json.RawMessage(fmt.Sprintf(`{"_attrs_dropped_bytes":%d}`, size))
}

// WithAttrs returns a new Handler with the given pre-resolved attributes.
// The returned handler shares the parent's state (counters, channel, goroutine)
// so Dropped/SendFailed/EncodeFailed aggregate across all derived loggers.
func (h *Handler) WithAttrs(attrs []slog.Attr) slog.Handler {
	if len(attrs) == 0 {
		return h
	}
	return h.withGroupOrAttrs(groupOrAttrs{attrs: attrs})
}

// WithGroup returns a new Handler with the given group prefix.
// The returned handler shares the parent's state (see WithAttrs).
func (h *Handler) WithGroup(name string) slog.Handler {
	if name == "" {
		return h
	}
	return h.withGroupOrAttrs(groupOrAttrs{group: name})
}

// withGroupOrAttrs returns a copy of h with goa appended. The goas slice is
// copied so sibling handlers derived from h never share a backing array.
func (h *Handler) withGroupOrAttrs(goa groupOrAttrs) *Handler {
	h2 := *h
	h2.goas = make([]groupOrAttrs, len(h.goas)+1)
	copy(h2.goas, h.goas)
	h2.goas[len(h.goas)] = goa
	return &h2
}

// Dropped returns the number of log entries dropped due to a full buffer or
// submission after shutdown began.
func (h *Handler) Dropped() int64 {
	return h.state.dropped.Load()
}

// SendFailed returns the number of log entries that failed to send due to
// HTTP errors or timeouts. After the library's bounded retry policy has been
// exhausted (one retry on the following flush tick), entries are counted here
// and dropped to keep memory bounded.
func (h *Handler) SendFailed() int64 {
	return h.state.sendFailed.Load()
}

// EncodeFailed returns the number of log entries whose attrs could not be
// shipped: they failed to JSON-marshal, or their encoded size was over the
// ingest API's 64 KB cap. Those entries still ship, with an "_encode_error"
// or "_attrs_dropped_bytes" stub attr, so the record itself is not lost —
// only the attrs payload is replaced.
func (h *Handler) EncodeFailed() int64 {
	return h.state.encodeFailed.Load()
}

// Shutdown flushes remaining buffered logs and stops the background goroutine.
// It marks the handler as closing (so concurrent Handle calls drop fast),
// records the caller's context for the final drain flush, waits for in-flight
// Handle calls to finish their push, and waits for the drain goroutine to
// finish or the context to expire.
//
// If ctx expires first, Shutdown returns ctx.Err(). That covers a send still
// in flight (bounded by Config.SendTimeout) and a Handle call stuck in user
// code such as Redact or a LogValuer. The drain still runs once that call
// returns, under the first caller's context, so entries it cannot send are
// counted in SendFailed.
//
// Shutdown is safe to call multiple times and from multiple goroutines — only
// the first call performs the close; every call waits for the drain or its
// own context, whichever ends first.
func (h *Handler) Shutdown(ctx context.Context) error {
	h.closeOnce.Do(func() {
		// Flip the fast-path flag first so new Handle callers drop.
		h.state.closing.Store(true)
		h.state.shutdownCtx = ctx

		// Write-lock the barrier: Lock blocks until every in-flight Handle
		// call has released its read lock, so close(h.done) cannot race with
		// an enqueue that the drain loop would never see. It runs in its own
		// goroutine because a Handle call can stay in user code indefinitely,
		// and every caller must be able to return when its context expires.
		go func() {
			h.state.mu.Lock()
			h.state.mu.Unlock() //nolint:staticcheck,gocritic // Intentional Lock+Unlock used as a barrier.
			close(h.done)
		}()
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
	// pendingRetry holds a single previous batch that failed to send. It is
	// retried (combined with the current batch) on the next flush tick. On a
	// second consecutive failure the combined set is counted as SendFailed
	// and dropped, keeping retained memory bounded to ~2 × BatchSize.
	var pendingRetry []logEntry
	ticker := time.NewTicker(h.cfg.FlushPeriod)
	defer ticker.Stop()

	flush := func(ctx context.Context) {
		if len(batch) == 0 && len(pendingRetry) == 0 {
			return
		}

		wasRetry := len(pendingRetry) > 0
		var toSend []logEntry
		if wasRetry {
			toSend = make([]logEntry, 0, len(pendingRetry)+len(batch))
			toSend = append(toSend, pendingRetry...)
			toSend = append(toSend, batch...)
		} else {
			toSend = batch
		}

		err := h.send(ctx, toSend)
		if err == nil {
			pendingRetry = nil
			batch = batch[:0]
			return
		}

		h.intLogger.Warn("logshipper send failed",
			"error", err,
			"batch_size", len(toSend),
			"retry", wasRetry,
		)
		if wasRetry {
			// Second consecutive failure — drop all, keep memory bounded.
			h.state.sendFailed.Add(int64(len(toSend)))
			pendingRetry = nil
		} else {
			// First failure — retain for one retry on the next flush tick.
			// Copy so the backing array can be reused.
			pendingRetry = make([]logEntry, len(toSend))
			copy(pendingRetry, toSend)
		}
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
					// Any entries still in pendingRetry after the final
					// drain flush are unreachable. Count them as failed so
					// the caller's SendFailed() reflects reality.
					if len(pendingRetry) > 0 {
						h.state.sendFailed.Add(int64(len(pendingRetry)))
						pendingRetry = nil
					}
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

	// Only 2xx is a delivery. The built-in client does not follow redirects,
	// so a 3xx means the entries never reached the ingest API.
	if resp.StatusCode < http.StatusOK || resp.StatusCode >= http.StatusMultipleChoices {
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

	// The group maps along the path are created only once there is a value
	// to store, so a group with no attrs is not emitted.
	if a.Value.Kind() == slog.KindGroup {
		sub := make(map[string]any)
		for _, ga := range a.Value.Group() {
			h.setAttr(sub, nil, ga)
		}
		if len(sub) == 0 {
			return
		}
		target := groupMap(m, groups)
		if a.Key == "" {
			// An inline group's attrs belong to the enclosing level.
			for k, v := range sub {
				target[k] = v
			}
			return
		}
		target[a.Key] = sub
		return
	}

	var v any
	if a.Value.Kind() == slog.KindDuration {
		v = a.Value.Duration().String()
	} else {
		v = a.Value.Any()
		// fmt.Sprint calls Error()/String() and recovers when the method
		// panics on a nil receiver, so a nil *url.URL cannot crash the caller.
		if _, ok := v.(error); ok {
			v = fmt.Sprint(v)
		} else if _, ok := v.(json.Marshaler); !ok {
			// Use String() for fmt.Stringer types that don't implement
			// json.Marshaler, so *url.URL, *regexp.Regexp etc. serialize readably.
			if _, ok := v.(fmt.Stringer); ok {
				v = fmt.Sprint(v)
			}
		}
	}

	if h.cfg.Redact != nil {
		v = h.cfg.Redact(a.Key, v)
		if v == nil {
			return
		}
	}

	if h.cfg.MaxAttrBytes > 0 {
		if s, ok := v.(string); ok && len(s) > h.cfg.MaxAttrBytes {
			v = s[:h.cfg.MaxAttrBytes] + truncationSuffix
		}
	}
	groupMap(m, groups)[a.Key] = v
}

// groupMap returns the map at the group path below m, creating the maps on
// the path as needed.
func groupMap(m map[string]any, groups []string) map[string]any {
	for _, g := range groups {
		sub, ok := m[g].(map[string]any)
		if !ok {
			sub = make(map[string]any)
			m[g] = sub
		}
		m = sub
	}
	return m
}
