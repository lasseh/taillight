package notification

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"sync"
	"time"

	"github.com/lasseh/taillight/internal/metrics"
	"github.com/lasseh/taillight/internal/model"
	"github.com/sony/gobreaker/v2"
)

// retrySchedule is the bounded backoff applied to delivery failures.
// Four attempts total: 5s, 30s, 2m, 10m. After the last delay expires and
// the final attempt still fails, the notification is abandoned.
var retrySchedule = []time.Duration{
	5 * time.Second,
	30 * time.Second,
	2 * time.Minute,
	10 * time.Minute,
}

// dispatchJob is a unit of work for a dispatch worker.
type dispatchJob struct {
	rule     Rule
	channels []Channel
	payload  Payload
}

// breakerEntry wraps a circuit breaker with a last-used timestamp for TTL eviction.
type breakerEntry struct {
	cb       *gobreaker.CircuitBreaker[SendResult]
	lastUsed time.Time
}

// Engine is the central notification orchestrator.
type Engine struct {
	store       Store
	backends    map[ChannelType]Notifier
	channels    []Channel
	rules       []Rule
	cacheMu     sync.RWMutex
	suppressor  *Suppressor
	rateLimiter *PerKeyLimiter
	breakers    map[int64]*breakerEntry
	breakerMu   sync.Mutex
	dispatchCh  chan dispatchJob
	logger      *slog.Logger
	cfg         Config
	wg          sync.WaitGroup
	cancel      context.CancelFunc
}

// NewEngine creates a new notification engine.
func NewEngine(store Store, cfg Config, logger *slog.Logger) *Engine {
	e := &Engine{
		store:       store,
		backends:    make(map[ChannelType]Notifier),
		rateLimiter: NewPerKeyLimiter(),
		breakers:    make(map[int64]*breakerEntry),
		dispatchCh:  make(chan dispatchJob, cfg.DispatchBuffer),
		logger:      logger.With("component", "notification-engine"),
		cfg:         cfg,
	}

	e.suppressor = NewSuppressor(e.onFlush)
	return e
}

// RegisterBackend registers a notification backend for a channel type.
func (e *Engine) RegisterBackend(t ChannelType, n Notifier) {
	e.backends[t] = n
}

// Start launches the engine's background goroutines.
func (e *Engine) Start(ctx context.Context) {
	ctx, e.cancel = context.WithCancel(ctx)

	// Initial cache load.
	if err := e.refreshCache(ctx); err != nil {
		e.logger.Error("initial cache load failed", "err", err)
	}

	// Rule/channel refresh goroutine.
	e.wg.Go(func() {
		ticker := time.NewTicker(e.cfg.RuleRefreshInterval)
		defer ticker.Stop()
		for {
			select {
			case <-ctx.Done():
				return
			case <-ticker.C:
				if err := e.refreshCache(ctx); err != nil {
					e.logger.Warn("cache refresh failed", "err", err)
				}
			}
		}
	})

	// Breaker eviction goroutine.
	e.wg.Go(func() {
		ticker := time.NewTicker(limiterEvictInterval)
		defer ticker.Stop()
		for {
			select {
			case <-ctx.Done():
				return
			case <-ticker.C:
				e.evictStaleBreakers()
			}
		}
	})

	// Dispatch workers.
	for range e.cfg.DispatchWorkers {
		e.wg.Go(func() {
			e.dispatchWorker(ctx)
		})
	}

	e.logger.Info("notification engine started",
		"workers", e.cfg.DispatchWorkers,
		"default_silence", e.cfg.DefaultSilence,
		"default_silence_max", e.cfg.DefaultSilenceMax,
		"default_coalesce", e.cfg.DefaultCoalesce,
	)
}

// Shutdown stops the engine and waits for in-flight dispatches to complete.
// If the context deadline is reached, shutdown returns the context error.
func (e *Engine) Shutdown(ctx context.Context) error {
	e.suppressor.Stop()
	e.rateLimiter.Stop()
	if e.cancel != nil {
		e.cancel()
	}
	close(e.dispatchCh)

	done := make(chan struct{})
	go func() {
		e.wg.Wait()
		close(done)
	}()

	select {
	case <-done:
		e.logger.Info("notification engine stopped")
		return nil
	case <-ctx.Done():
		e.logger.Warn("notification engine shutdown timed out")
		return ctx.Err()
	}
}

// HandleSrvlogEvent evaluates all srvlog rules against the event.
func (e *Engine) HandleSrvlogEvent(event model.SrvlogEvent) {
	e.cacheMu.RLock()
	rules := e.rules
	e.cacheMu.RUnlock()

	for _, r := range rules {
		if !r.Enabled || r.EventKind != EventKindSrvlog {
			continue
		}
		metrics.NotifRulesEvaluatedTotal.Inc()

		if r.MatchesSrvlog(event) {
			metrics.NotifRulesMatchedTotal.Inc()

			payload := Payload{
				Kind:        EventKindSrvlog,
				RuleName:    r.Name,
				Timestamp:   event.ReceivedAt,
				EventCount:  1,
				SrvlogEvent: &event,
			}

			groupKey := r.GroupKeyFromSrvlog(event)
			silence, silenceMax, coalesce := e.windowsFor(r)
			e.suppressor.Record(r.ID, groupKey, silence, silenceMax, coalesce, payload)
		}
	}
}

// HandleNetlogEvent evaluates all netlog rules against the event.
func (e *Engine) HandleNetlogEvent(event model.NetlogEvent) {
	e.cacheMu.RLock()
	rules := e.rules
	e.cacheMu.RUnlock()

	for _, r := range rules {
		if !r.Enabled || r.EventKind != EventKindNetlog {
			continue
		}
		metrics.NotifRulesEvaluatedTotal.Inc()

		if r.MatchesNetlog(event) {
			metrics.NotifRulesMatchedTotal.Inc()

			payload := Payload{
				Kind:        EventKindNetlog,
				RuleName:    r.Name,
				Timestamp:   event.ReceivedAt,
				EventCount:  1,
				NetlogEvent: &event,
			}

			groupKey := r.GroupKeyFromNetlog(event)
			silence, silenceMax, coalesce := e.windowsFor(r)
			e.suppressor.Record(r.ID, groupKey, silence, silenceMax, coalesce, payload)
		}
	}
}

// HandleAppLogEvent evaluates all applog rules against the event.
func (e *Engine) HandleAppLogEvent(event model.AppLogEvent) {
	e.cacheMu.RLock()
	rules := e.rules
	e.cacheMu.RUnlock()

	for _, r := range rules {
		if !r.Enabled || r.EventKind != EventKindAppLog {
			continue
		}
		metrics.NotifRulesEvaluatedTotal.Inc()

		if r.MatchesAppLog(event) {
			metrics.NotifRulesMatchedTotal.Inc()

			payload := Payload{
				Kind:        EventKindAppLog,
				RuleName:    r.Name,
				Timestamp:   event.Timestamp,
				EventCount:  1,
				AppLogEvent: &event,
			}

			groupKey := r.GroupKeyFromAppLog(event)
			silence, silenceMax, coalesce := e.windowsFor(r)
			e.suppressor.Record(r.ID, groupKey, silence, silenceMax, coalesce, payload)
		}
	}
}

// windowsFor resolves the silence/silenceMax/coalesce durations for a rule,
// falling back to engine defaults when the rule leaves a field unset.
func (e *Engine) windowsFor(r Rule) (silence, silenceMax, coalesce time.Duration) {
	silence = r.Silence()
	if silence <= 0 {
		silence = e.cfg.DefaultSilence
	}
	silenceMax = r.SilenceMax()
	if silenceMax <= 0 {
		silenceMax = e.cfg.DefaultSilenceMax
	}
	coalesce = r.Coalesce()
	if coalesce <= 0 {
		coalesce = e.cfg.DefaultCoalesce
	}
	return silence, silenceMax, coalesce
}

// SendTestNotification sends a test notification to a channel, bypassing suppression.
func (e *Engine) SendTestNotification(ctx context.Context, ch Channel) (SendResult, error) {
	backend, ok := e.backends[ch.Type]
	if !ok {
		return SendResult{}, fmt.Errorf("no backend registered for channel type %q", ch.Type)
	}

	payload := Payload{
		Kind:       EventKindSrvlog,
		RuleName:   "test",
		Timestamp:  time.Now(),
		EventCount: 1,
		SrvlogEvent: &model.SrvlogEvent{
			ReceivedAt:    time.Now(),
			Hostname:      "test.example.com",
			Programname:   "taillight",
			Severity:      6,
			SeverityLabel: "info",
			Facility:      1,
			FacilityLabel: "user",
			Message:       "This is a test notification from Taillight",
		},
	}

	result := backend.Send(ctx, ch, payload)
	return result, nil
}

// SendSummary dispatches a summary report to the specified channels.
func (e *Engine) SendSummary(ctx context.Context, report SummaryReport, channelIDs []int64) {
	e.cacheMu.RLock()
	channels := e.resolveChannels(channelIDs)
	e.cacheMu.RUnlock()

	if len(channels) == 0 {
		e.logger.Warn("no channels for summary schedule", "schedule", report.Schedule.Name)
		return
	}

	payload := Payload{
		Kind:          "summary",
		RuleName:      report.Schedule.Name,
		Timestamp:     time.Now(),
		SummaryReport: &report,
	}

	rule := Rule{Name: report.Schedule.Name}
	for _, ch := range channels {
		if !ch.Enabled {
			continue
		}
		e.safeSendToChannel(ctx, rule, ch, payload)
	}
}

// ValidateChannel validates a channel's config against its backend.
func (e *Engine) ValidateChannel(ch Channel) error {
	backend, ok := e.backends[ch.Type]
	if !ok {
		return fmt.Errorf("unknown channel type %q", ch.Type)
	}
	return backend.Validate(ch)
}

// onFlush is the callback from the Suppressor when a fingerprint decides
// to emit a notification. The payload's IsDigest and EventCount are already set.
func (e *Engine) onFlush(ruleID int64, groupKey string, payload Payload) {
	e.cacheMu.RLock()
	var rule Rule
	for _, r := range e.rules {
		if r.ID == ruleID {
			rule = r
			break
		}
	}
	channels := e.resolveChannels(rule.ChannelIDs)
	e.cacheMu.RUnlock()

	if len(channels) == 0 {
		e.logger.Warn("no channels for rule", "rule_id", ruleID, "group_key", groupKey)
		return
	}

	kind := "alert"
	if payload.IsDigest {
		kind = "digest"
	}
	e.logger.Debug("notification flush",
		"rule_id", ruleID,
		"group_key", groupKey,
		"kind", kind,
		"count", payload.EventCount,
	)

	select {
	case e.dispatchCh <- dispatchJob{rule: rule, channels: channels, payload: payload}:
		metrics.NotifDispatchedTotal.Inc()
		metrics.NotifDispatchQueueLen.Set(float64(len(e.dispatchCh)))
	default:
		metrics.NotifSuppressedTotal.WithLabelValues("queue_full").Inc()
		e.logger.Warn("dispatch queue full, dropping notification", "rule_id", ruleID)
	}
}

// dispatchWorker processes jobs from the dispatch channel.
func (e *Engine) dispatchWorker(ctx context.Context) {
	for job := range e.dispatchCh {
		for _, ch := range job.channels {
			if !ch.Enabled {
				continue
			}
			e.safeSendToChannel(ctx, job.rule, ch, job.payload)
		}
	}
}

// safeSendToChannel wraps sendWithRetry with panic recovery so that a
// malformed template or nil-pointer dereference cannot kill a dispatch worker.
func (e *Engine) safeSendToChannel(ctx context.Context, rule Rule, ch Channel, payload Payload) {
	defer func() {
		if r := recover(); r != nil {
			e.logger.Error("dispatch worker recovered from panic",
				"channel_id", ch.ID,
				"rule_id", rule.ID,
				"panic", fmt.Sprintf("%v", r),
			)
		}
	}()
	e.sendWithRetry(ctx, rule, ch, payload)
}

// sendWithRetry attempts delivery, and on failure retries per retrySchedule.
// Every attempt is logged to notification_log so operators can see the full
// retry chain. The final outcome is counted in NotifSendAttemptsTotal.
func (e *Engine) sendWithRetry(ctx context.Context, rule Rule, ch Channel, payload Payload) {
	maxAttempts := len(retrySchedule) + 1

	for attempt := 1; attempt <= maxAttempts; attempt++ {
		if ctx.Err() != nil {
			return
		}

		status, result := e.attemptSend(ctx, rule, ch, payload)

		switch status {
		case attemptSuccess:
			outcome := "first_try"
			if attempt > 1 {
				outcome = "retry_success"
			}
			metrics.NotifSendAttemptsTotal.WithLabelValues(outcome).Inc()
			e.recordLog(ctx, rule, ch, payload, "sent", nil, result, attempt)
			return

		case attemptSuppressed:
			// Rate limit or circuit breaker — don't retry, and metrics are
			// already incremented inside attemptSend.
			metrics.NotifSendAttemptsTotal.WithLabelValues("suppressed").Inc()
			reason := result.Error
			var reasonStr *string
			if reason != nil {
				s := reason.Error()
				reasonStr = &s
			}
			e.recordLog(ctx, rule, ch, payload, "suppressed", reasonStr, result, attempt)
			return

		case attemptFailed:
			// Log this attempt's failure, then either retry or give up.
			reasonStr := ""
			if result.Error != nil {
				reasonStr = result.Error.Error()
			}
			r := reasonStr
			e.recordLog(ctx, rule, ch, payload, "failed", &r, result, attempt)

			if attempt >= maxAttempts {
				metrics.NotifSendAttemptsTotal.WithLabelValues("retry_exhausted").Inc()
				e.logger.Warn("notification send exhausted retries",
					"channel_id", ch.ID,
					"rule_id", rule.ID,
					"attempts", attempt,
					"err", result.Error,
				)
				return
			}

			delay := retrySchedule[attempt-1]
			e.logger.Debug("notification send failed, scheduling retry",
				"channel_id", ch.ID,
				"rule_id", rule.ID,
				"attempt", attempt,
				"delay", delay,
				"err", result.Error,
			)
			select {
			case <-ctx.Done():
				return
			case <-time.After(delay):
			}
		}
	}
}

type attemptStatus int

const (
	attemptSuccess attemptStatus = iota
	attemptFailed
	attemptSuppressed
)

// attemptSend does one delivery attempt through the rate limiter + circuit
// breaker + backend. The caller decides whether to retry.
func (e *Engine) attemptSend(ctx context.Context, rule Rule, ch Channel, payload Payload) (attemptStatus, SendResult) {
	if !e.rateLimiter.Allow(ch.ID, ch.Type) {
		metrics.NotifSuppressedTotal.WithLabelValues("rate_limit").Inc()
		e.logger.Debug("notification rate limited", "channel_id", ch.ID, "rule_id", rule.ID)
		return attemptSuppressed, SendResult{Error: errors.New("rate limited")}
	}

	backend, ok := e.backends[ch.Type]
	if !ok {
		e.logger.Error("no backend for channel type", "type", ch.Type)
		return attemptFailed, SendResult{Error: fmt.Errorf("no backend for type %q", ch.Type)}
	}

	cb := e.getOrCreateBreaker(ch.ID, ch.Name)
	sendCtx, cancel := context.WithTimeout(ctx, e.cfg.SendTimeout)
	defer cancel()

	result, cbErr := cb.Execute(func() (SendResult, error) {
		r := backend.Send(sendCtx, ch, payload)
		if !r.Success {
			return r, r.Error
		}
		return r, nil
	})

	if cbErr != nil {
		if cb.State() == gobreaker.StateOpen {
			metrics.NotifSuppressedTotal.WithLabelValues("circuit_breaker").Inc()
			metrics.NotifSentTotal.WithLabelValues(string(ch.Type), "failed").Inc()
			e.logger.Warn("circuit breaker open",
				"channel_id", ch.ID, "rule_id", rule.ID)
			return attemptSuppressed, SendResult{
				Error:      fmt.Errorf("circuit breaker open: %w", cbErr),
				StatusCode: result.StatusCode,
				Duration:   result.Duration,
			}
		}
		metrics.NotifSentTotal.WithLabelValues(string(ch.Type), "failed").Inc()
		result.Error = cbErr
		return attemptFailed, result
	}

	metrics.NotifSentTotal.WithLabelValues(string(ch.Type), "success").Inc()
	metrics.NotifSendDuration.Observe(result.Duration.Seconds())
	return attemptSuccess, result
}

// recordLog writes one notification_log row for a single attempt.
func (e *Engine) recordLog(
	ctx context.Context,
	rule Rule, ch Channel, payload Payload,
	status string, reason *string,
	result SendResult,
	_ int,
) {
	logPayload, marshalErr := json.Marshal(payload)
	if marshalErr != nil {
		e.logger.Warn("failed to marshal notification payload", "rule_id", rule.ID, "err", marshalErr)
	}

	var eventID int64
	switch {
	case payload.SrvlogEvent != nil:
		eventID = payload.SrvlogEvent.ID
	case payload.NetlogEvent != nil:
		eventID = payload.NetlogEvent.ID
	case payload.AppLogEvent != nil:
		eventID = payload.AppLogEvent.ID
	}

	if err := e.store.InsertNotificationLog(ctx, LogEntry{
		RuleID:     rule.ID,
		ChannelID:  ch.ID,
		EventKind:  string(payload.Kind),
		EventID:    eventID,
		Status:     status,
		Reason:     reason,
		EventCount: payload.EventCount,
		StatusCode: optionalInt(result.StatusCode),
		DurationMS: int(result.Duration.Milliseconds()),
		Payload:    logPayload,
	}); err != nil {
		e.logger.Warn("failed to insert notification log",
			"rule_id", rule.ID, "channel_id", ch.ID, "err", err)
	}
}

// refreshCache reloads rules and channels from the store.
func (e *Engine) refreshCache(ctx context.Context) error {
	rules, err := e.store.ListNotificationRules(ctx)
	if err != nil {
		return fmt.Errorf("load rules: %w", err)
	}

	channels, err := e.store.ListNotificationChannels(ctx)
	if err != nil {
		return fmt.Errorf("load channels: %w", err)
	}

	e.cacheMu.Lock()
	e.rules = rules
	e.channels = channels
	e.cacheMu.Unlock()

	return nil
}

// resolveChannels maps channel IDs to Channel objects from the cache.
func (e *Engine) resolveChannels(ids []int64) []Channel {
	idSet := make(map[int64]struct{}, len(ids))
	for _, id := range ids {
		idSet[id] = struct{}{}
	}

	var result []Channel
	for _, ch := range e.channels {
		if _, ok := idSet[ch.ID]; ok {
			result = append(result, ch)
		}
	}
	return result
}

// getOrCreateBreaker returns the circuit breaker for a channel, creating one if needed.
func (e *Engine) getOrCreateBreaker(channelID int64, channelName string) *gobreaker.CircuitBreaker[SendResult] {
	now := time.Now()

	e.breakerMu.Lock()
	defer e.breakerMu.Unlock()

	if entry, ok := e.breakers[channelID]; ok {
		entry.lastUsed = now
		return entry.cb
	}

	cb := gobreaker.NewCircuitBreaker[SendResult](gobreaker.Settings{
		Name:        fmt.Sprintf("notif-channel-%d-%s", channelID, channelName),
		MaxRequests: 2,
		Interval:    60 * time.Second,
		Timeout:     60 * time.Second,
		ReadyToTrip: func(counts gobreaker.Counts) bool {
			return counts.ConsecutiveFailures >= 5
		},
	})
	e.breakers[channelID] = &breakerEntry{cb: cb, lastUsed: now}
	return cb
}

// evictStaleBreakers removes circuit breakers that haven't been used recently.
func (e *Engine) evictStaleBreakers() {
	e.breakerMu.Lock()
	defer e.breakerMu.Unlock()
	now := time.Now()
	for id, entry := range e.breakers {
		if now.Sub(entry.lastUsed) > limiterTTL {
			delete(e.breakers, id)
		}
	}
}

func optionalInt(v int) *int {
	if v == 0 {
		return nil
	}
	return &v
}
