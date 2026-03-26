package notification

import (
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"sync"
	"time"

	"github.com/lasseh/taillight/internal/metrics"
	"github.com/lasseh/taillight/internal/model"
	"github.com/sony/gobreaker/v2"
)

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
	groups      *GroupTracker
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

	e.groups = NewGroupTracker(e.onGroupFlush)
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
	for i := 0; i < e.cfg.DispatchWorkers; i++ {
		e.wg.Go(func() {
			e.dispatchWorker(ctx)
		})
	}

	e.logger.Info("notification engine started",
		"workers", e.cfg.DispatchWorkers,
		"burst_window", e.cfg.DefaultBurstWindow,
		"cooldown", e.cfg.DefaultCooldown,
	)
}

// Shutdown stops the engine and waits for in-flight dispatches to complete.
// If the context deadline is reached, shutdown returns the context error.
func (e *Engine) Shutdown(ctx context.Context) error {
	e.groups.Stop()
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
			window := time.Duration(r.BurstWindow) * time.Second
			cooldown := time.Duration(r.CooldownSeconds) * time.Second
			maxCooldown := time.Duration(r.MaxCooldownSeconds) * time.Second
			if window <= 0 {
				window = e.cfg.DefaultBurstWindow
			}
			if cooldown <= 0 {
				cooldown = e.cfg.DefaultCooldown
			}
			if maxCooldown <= 0 {
				maxCooldown = e.cfg.DefaultMaxCooldown
			}

			e.groups.Add(r.ID, groupKey, window, cooldown, maxCooldown, payload)
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
			window := time.Duration(r.BurstWindow) * time.Second
			cooldown := time.Duration(r.CooldownSeconds) * time.Second
			maxCooldown := time.Duration(r.MaxCooldownSeconds) * time.Second
			if window <= 0 {
				window = e.cfg.DefaultBurstWindow
			}
			if cooldown <= 0 {
				cooldown = e.cfg.DefaultCooldown
			}
			if maxCooldown <= 0 {
				maxCooldown = e.cfg.DefaultMaxCooldown
			}

			e.groups.Add(r.ID, groupKey, window, cooldown, maxCooldown, payload)
		}
	}
}

// SendTestNotification sends a test notification to a channel, bypassing burst/cooldown.
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

// ValidateChannel validates a channel's config against its backend.
func (e *Engine) ValidateChannel(ch Channel) error {
	backend, ok := e.backends[ch.Type]
	if !ok {
		return fmt.Errorf("unknown channel type %q", ch.Type)
	}
	return backend.Validate(ch)
}

// onGroupFlush is the callback from GroupTracker when a burst or cooldown window closes.
func (e *Engine) onGroupFlush(ruleID int64, groupKey string, fp FlushPayload) {
	// Build the final payload from the flush data.
	var payload Payload
	if fp.IsDigest {
		// Digest: use the last event for context.
		payload = fp.Last
		payload.IsDigest = true
	} else {
		// Initial: use the first event.
		payload = fp.First
	}
	payload.EventCount = fp.Count
	payload.GroupKey = groupKey
	payload.Window = fp.Window

	// Look up the rule to get channel IDs.
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

	kind := "initial"
	if fp.IsDigest {
		kind = "digest"
	}
	e.logger.Debug("notification flush",
		"rule_id", ruleID,
		"group_key", groupKey,
		"kind", kind,
		"count", fp.Count,
		"window", fp.Window,
	)

	// Send to dispatch queue.
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

// safeSendToChannel wraps sendToChannel with panic recovery so that a
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
	e.sendToChannel(ctx, rule, ch, payload)
}

// sendToChannel delivers a notification to a single channel with rate limiting and circuit breaking.
func (e *Engine) sendToChannel(ctx context.Context, rule Rule, ch Channel, payload Payload) {
	// Rate limit check.
	if !e.rateLimiter.Allow(ch.ID, ch.Type) {
		metrics.NotifSuppressedTotal.WithLabelValues("rate_limit").Inc()
		e.logger.Debug("notification rate limited", "channel_id", ch.ID, "rule_id", rule.ID)
		return
	}

	backend, ok := e.backends[ch.Type]
	if !ok {
		e.logger.Error("no backend for channel type", "type", ch.Type)
		return
	}

	// Circuit breaker.
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

	eventID := int64(0)
	if payload.SrvlogEvent != nil {
		eventID = payload.SrvlogEvent.ID
	} else if payload.AppLogEvent != nil {
		eventID = payload.AppLogEvent.ID
	}

	if cbErr != nil {
		status := "failed"
		reason := cbErr.Error()
		if cb.State() == gobreaker.StateOpen {
			metrics.NotifSuppressedTotal.WithLabelValues("circuit_breaker").Inc()
			reason = "circuit breaker open"
		}
		metrics.NotifSentTotal.WithLabelValues(string(ch.Type), "failed").Inc()
		e.logger.Warn("notification send failed",
			"channel_id", ch.ID,
			"rule_id", rule.ID,
			"err", cbErr,
		)

		logPayload, marshalErr := json.Marshal(payload)
		if marshalErr != nil {
			e.logger.Warn("failed to marshal notification payload", "rule_id", rule.ID, "err", marshalErr)
		}
		if err := e.store.InsertNotificationLog(ctx, LogEntry{
			RuleID:     rule.ID,
			ChannelID:  ch.ID,
			EventKind:  string(payload.Kind),
			EventID:    eventID,
			Status:     status,
			Reason:     &reason,
			EventCount: payload.EventCount,
			StatusCode: optionalInt(result.StatusCode),
			DurationMS: int(result.Duration.Milliseconds()),
			Payload:    logPayload,
		}); err != nil {
			e.logger.Warn("failed to insert notification log", "rule_id", rule.ID, "channel_id", ch.ID, "err", err)
		}
		return
	}

	metrics.NotifSentTotal.WithLabelValues(string(ch.Type), "success").Inc()
	metrics.NotifSendDuration.Observe(result.Duration.Seconds())

	logPayload, marshalErr := json.Marshal(payload)
	if marshalErr != nil {
		e.logger.Warn("failed to marshal notification payload", "rule_id", rule.ID, "err", marshalErr)
	}
	if err := e.store.InsertNotificationLog(ctx, LogEntry{
		RuleID:     rule.ID,
		ChannelID:  ch.ID,
		EventKind:  string(payload.Kind),
		EventID:    eventID,
		Status:     "sent",
		EventCount: payload.EventCount,
		StatusCode: optionalInt(result.StatusCode),
		DurationMS: int(result.Duration.Milliseconds()),
		Payload:    logPayload,
	}); err != nil {
		e.logger.Warn("failed to insert notification log", "rule_id", rule.ID, "channel_id", ch.ID, "err", err)
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

	// Circuit breaker per channel: allow 2 half-open probes, open after 5
	// consecutive failures, and retry after 60s.
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
