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

// Engine is the central notification orchestrator.
type Engine struct {
	store       Store
	backends    map[ChannelType]Notifier
	channels    []Channel
	rules       []Rule
	cacheMu     sync.RWMutex
	bursts      *BurstWatcher
	cooldowns   *CooldownTracker
	rateLimiter *PerKeyLimiter
	breakers    map[int64]*gobreaker.CircuitBreaker[SendResult]
	breakerMu   sync.RWMutex
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
		cooldowns:   NewCooldownTracker(),
		rateLimiter: NewPerKeyLimiter(),
		breakers:    make(map[int64]*gobreaker.CircuitBreaker[SendResult]),
		dispatchCh:  make(chan dispatchJob, cfg.DispatchBuffer),
		logger:      logger.With("component", "notification-engine"),
		cfg:         cfg,
	}

	e.bursts = NewBurstWatcher(cfg.DefaultBurstWindow, e.onBurstFlush)
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
	e.wg.Add(1)
	go func() {
		defer e.wg.Done()
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
	}()

	// Cooldown drain goroutine.
	e.wg.Add(1)
	go func() {
		defer e.wg.Done()
		ticker := time.NewTicker(10 * time.Second)
		defer ticker.Stop()
		for {
			select {
			case <-ctx.Done():
				return
			case <-ticker.C:
				e.drainExpiredCooldowns(ctx)
			}
		}
	}()

	// Dispatch workers.
	for i := 0; i < e.cfg.DispatchWorkers; i++ {
		e.wg.Add(1)
		go func() {
			defer e.wg.Done()
			e.dispatchWorker(ctx)
		}()
	}

	e.logger.Info("notification engine started",
		"workers", e.cfg.DispatchWorkers,
		"burst_window", e.cfg.DefaultBurstWindow,
		"cooldown", e.cfg.DefaultCooldown,
	)
}

// Shutdown stops the engine and waits for in-flight dispatches to complete.
func (e *Engine) Shutdown(_ context.Context) error {
	e.bursts.Stop()
	if e.cancel != nil {
		e.cancel()
	}
	close(e.dispatchCh)
	e.wg.Wait()
	e.logger.Info("notification engine stopped")
	return nil
}

// HandleSyslogEvent evaluates all syslog rules against the event.
func (e *Engine) HandleSyslogEvent(event model.SyslogEvent) {
	e.cacheMu.RLock()
	rules := e.rules
	e.cacheMu.RUnlock()

	for _, r := range rules {
		if !r.Enabled || r.EventKind != EventKindSyslog {
			continue
		}
		metrics.NotifRulesEvaluatedTotal.Inc()

		if r.MatchesSyslog(event) {
			metrics.NotifRulesMatchedTotal.Inc()

			payload := Payload{
				Kind:        EventKindSyslog,
				RuleName:    r.Name,
				Timestamp:   event.ReceivedAt,
				EventCount:  1,
				SyslogEvent: &event,
			}

			window := time.Duration(r.BurstWindow) * time.Second
			e.bursts.Add(r.ID, window, payload)
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

			window := time.Duration(r.BurstWindow) * time.Second
			e.bursts.Add(r.ID, window, payload)
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
		Kind:       EventKindSyslog,
		RuleName:   "test",
		Timestamp:  time.Now(),
		EventCount: 1,
		SyslogEvent: &model.SyslogEvent{
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

// onBurstFlush is the callback from BurstWatcher when a burst window closes.
func (e *Engine) onBurstFlush(ruleID int64, first Payload, count int) {
	first.EventCount = count

	// Cooldown check.
	if e.cooldowns.Check(ruleID) {
		e.cooldowns.Suppress(ruleID)
		metrics.NotifSuppressedTotal.WithLabelValues("cooldown").Inc()
		e.logger.Debug("notification suppressed by cooldown", "rule_id", ruleID, "count", count)
		return
	}

	// Look up the rule to get channel IDs and cooldown duration.
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
		e.logger.Warn("no channels for rule", "rule_id", ruleID)
		return
	}

	// Activate cooldown.
	cooldown := time.Duration(rule.CooldownSeconds) * time.Second
	if cooldown <= 0 {
		cooldown = e.cfg.DefaultCooldown
	}
	e.cooldowns.Activate(ruleID, cooldown)

	// Send to dispatch queue.
	metrics.NotifDispatchedTotal.Inc()
	select {
	case e.dispatchCh <- dispatchJob{rule: rule, channels: channels, payload: first}:
		metrics.NotifDispatchQueueLen.Set(float64(len(e.dispatchCh)))
	default:
		e.logger.Warn("dispatch queue full, dropping notification", "rule_id", ruleID)
	}
}

// drainExpiredCooldowns checks for expired cooldowns and logs suppressed summaries.
func (e *Engine) drainExpiredCooldowns(ctx context.Context) {
	expired := e.cooldowns.DrainExpired()
	for _, ec := range expired {
		e.logger.Info("cooldown expired with suppressed events",
			"rule_id", ec.RuleID,
			"suppressed_count", ec.SuppressedCount,
		)

		// Log the suppression to audit trail.
		_ = e.store.InsertNotificationLog(ctx, LogEntry{
			RuleID:     ec.RuleID,
			EventKind:  "summary",
			EventID:    0,
			Status:     "suppressed",
			Reason:     strPtr(fmt.Sprintf("%d events suppressed during cooldown", ec.SuppressedCount)),
			EventCount: ec.SuppressedCount,
		})
	}
}

// dispatchWorker processes jobs from the dispatch channel.
func (e *Engine) dispatchWorker(ctx context.Context) {
	for job := range e.dispatchCh {
		for _, ch := range job.channels {
			if !ch.Enabled {
				continue
			}
			e.sendToChannel(ctx, job.rule, ch, job.payload)
		}
	}
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
	if payload.SyslogEvent != nil {
		eventID = payload.SyslogEvent.ID
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

		logPayload, _ := json.Marshal(payload)
		_ = e.store.InsertNotificationLog(ctx, LogEntry{
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
		})
		return
	}

	metrics.NotifSentTotal.WithLabelValues(string(ch.Type), "success").Inc()
	metrics.NotifSendDuration.Observe(result.Duration.Seconds())

	logPayload, _ := json.Marshal(payload)
	_ = e.store.InsertNotificationLog(ctx, LogEntry{
		RuleID:     rule.ID,
		ChannelID:  ch.ID,
		EventKind:  string(payload.Kind),
		EventID:    eventID,
		Status:     "sent",
		EventCount: payload.EventCount,
		StatusCode: optionalInt(result.StatusCode),
		DurationMS: int(result.Duration.Milliseconds()),
		Payload:    logPayload,
	})
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
	e.breakerMu.RLock()
	cb, ok := e.breakers[channelID]
	e.breakerMu.RUnlock()
	if ok {
		return cb
	}

	e.breakerMu.Lock()
	defer e.breakerMu.Unlock()

	// Double-check.
	if cb, ok = e.breakers[channelID]; ok {
		return cb
	}

	cb = gobreaker.NewCircuitBreaker[SendResult](gobreaker.Settings{
		Name:        fmt.Sprintf("notif-channel-%d-%s", channelID, channelName),
		MaxRequests: 2,
		Interval:    60 * time.Second,
		Timeout:     60 * time.Second,
		ReadyToTrip: func(counts gobreaker.Counts) bool {
			return counts.ConsecutiveFailures >= 5
		},
	})
	e.breakers[channelID] = cb
	return cb
}

func strPtr(s string) *string { return &s }

func optionalInt(v int) *int {
	if v == 0 {
		return nil
	}
	return &v
}
