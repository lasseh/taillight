package notification

import (
	"fmt"
	"strings"
	"sync"
	"time"

	"github.com/lasseh/taillight/internal/metrics"
)

// maxFingerprintsPerRule caps distinct fingerprints tracked per rule, preventing
// memory exhaustion from attacker-controlled unique keys (e.g. a too-specific
// group_by on a request ID).
const maxFingerprintsPerRule = 10000

// maxFingerprintsGlobal caps distinct fingerprints across all rules combined.
// The per-rule cap alone multiplies by the number of enabled rules, so total
// suppressor memory would otherwise be unbounded as rules are added.
const maxFingerprintsGlobal = 5 * maxFingerprintsPerRule

// maxRetainedMsgBytes caps the applog Msg copy retained per fingerprint
// between flushes. Ingest allows 64KB messages, but digest formatters render
// at most a few hundred characters, so retaining the full message for every
// live fingerprint would dominate suppressor memory.
const maxRetainedMsgBytes = 4096

// Suppressor manages the first-match-immediate + silence-window-digest lifecycle
// for notification fingerprints keyed by "ruleID:groupKey".
//
// Lifecycle per fingerprint:
//
//	clean:                  first match → fire immediately (or after Coalesce),
//	                        count=N, start silence.
//	silenced (in-window):   accumulate, do not send.
//	silence expires + N>0:  fire a digest with count=N, grow silence by Silence
//	                        (capped at SilenceMax), restart silence timer.
//	silence expires + N=0:  close the fingerprint. Next match fires immediately.
type Suppressor struct {
	mu                   sync.Mutex
	fingerprints         map[string]*fingerprint
	ruleFingerprintCount map[int64]int
	stopped              bool
	inflightWg           sync.WaitGroup
	onFlush              func(ruleID int64, groupKey string, payload Payload)
}

// fingerprint tracks one rule+groupKey's current state.
type fingerprint struct {
	phase phase

	// first/last carry the earliest/latest event payloads for this run.
	// `first` holds the full payload only while a coalesce window is open
	// and is cleared after the initial alert fires; from then on the digest
	// path uses `last` — a trimmed copy (see trimForDigest) — as the
	// representative event.
	first Payload
	last  Payload

	// count is the number of matches accumulated in the current window
	// (coalesce or silence). Reset after each flush.
	count int

	// currentSilence is the active silence window duration (grows per
	// flush by +baseSilence, capped at maxSilence).
	currentSilence time.Duration
	baseSilence    time.Duration
	maxSilence     time.Duration
	coalesce       time.Duration

	timer *time.Timer
}

type phase int

const (
	phaseCoalesce phase = iota // pre-first-send: counting inside coalesce window
	phaseSilence               // post-send: counting during silence window
)

// NewSuppressor creates a Suppressor that invokes onFlush whenever a fingerprint
// decides to emit a notification. The callback receives a Payload with Count
// and IsDigest already populated.
func NewSuppressor(onFlush func(ruleID int64, groupKey string, payload Payload)) *Suppressor {
	return &Suppressor{
		fingerprints:         make(map[string]*fingerprint),
		ruleFingerprintCount: make(map[int64]int),
		onFlush:              onFlush,
	}
}

// Record ingests one matching event for the given rule and group key. If the
// fingerprint is clean, this schedules an immediate flush (modulo coalesce).
// If the fingerprint is in a silence window, this just increments the counter.
//
// `silence` and `silenceMax` must be > 0. `coalesce` may be 0 (fire first match
// immediately).
func (s *Suppressor) Record(ruleID int64, groupKey string, silence, silenceMax, coalesce time.Duration, payload Payload) {
	key := fingerprintKey(ruleID, groupKey)

	s.mu.Lock()
	if s.stopped {
		s.mu.Unlock()
		return
	}

	fp, ok := s.fingerprints[key]
	if ok {
		// Already tracking this fingerprint — just accumulate.
		fp.count++
		fp.last = trimForDigest(payload)
		s.mu.Unlock()
		return
	}

	// New fingerprint. Enforce the per-rule cap and the global ceiling.
	if s.ruleFingerprintCount[ruleID] >= maxFingerprintsPerRule ||
		len(s.fingerprints) >= maxFingerprintsGlobal {
		metrics.NotifFingerprintsDroppedTotal.Inc()
		s.mu.Unlock()
		return
	}

	fp = &fingerprint{
		last:           trimForDigest(payload),
		count:          1,
		baseSilence:    silence,
		currentSilence: silence,
		maxSilence:     silenceMax,
		coalesce:       coalesce,
	}

	if coalesce > 0 {
		// Keep the full payload for the initial alert; flushInitial
		// clears it once that alert fires.
		fp.first = payload
		fp.phase = phaseCoalesce
		fp.timer = time.AfterFunc(coalesce, func() {
			s.flushInitial(ruleID, groupKey, key)
		})
		s.fingerprints[key] = fp
		s.ruleFingerprintCount[ruleID]++
		s.mu.Unlock()
		return
	}

	// coalesce == 0 → fire synchronously under the lock's protection but via
	// inflight wg so Stop() can drain us. We install the fingerprint into
	// the silence phase before releasing the lock.
	fp.phase = phaseSilence
	fp.count = 0 // reset after the immediate send
	fp.timer = time.AfterFunc(fp.currentSilence, func() {
		s.flushSilence(ruleID, groupKey, key)
	})
	s.fingerprints[key] = fp
	s.ruleFingerprintCount[ruleID]++

	// Build the alert payload before releasing the lock so we don't race
	// with concurrent Record() mutations.
	alert := payload
	alert.EventCount = 1
	alert.GroupKey = groupKey
	alert.IsDigest = false

	s.inflightWg.Add(1)
	s.mu.Unlock()

	defer s.inflightWg.Done()
	s.onFlush(ruleID, groupKey, alert)
}

// flushInitial fires when the coalesce window closes. It emits the first alert
// and transitions the fingerprint into the silence phase.
func (s *Suppressor) flushInitial(ruleID int64, groupKey, key string) {
	s.mu.Lock()
	if s.stopped {
		s.mu.Unlock()
		return
	}
	fp, ok := s.fingerprints[key]
	if !ok {
		s.mu.Unlock()
		return
	}

	alert := fp.first
	alert.EventCount = fp.count
	alert.GroupKey = groupKey
	alert.IsDigest = false

	fp.first = Payload{} // release the full payload; digests use fp.last
	fp.phase = phaseSilence
	fp.count = 0
	fp.timer = time.AfterFunc(fp.currentSilence, func() {
		s.flushSilence(ruleID, groupKey, key)
	})

	s.inflightWg.Add(1)
	s.mu.Unlock()

	defer s.inflightWg.Done()
	s.onFlush(ruleID, groupKey, alert)
}

// flushSilence fires when the silence window closes. If matches accumulated,
// it emits a digest and grows the silence window (capped). If silent, it
// closes the fingerprint — the next match will fire immediately.
func (s *Suppressor) flushSilence(ruleID int64, groupKey, key string) {
	s.mu.Lock()
	if s.stopped {
		s.mu.Unlock()
		return
	}
	fp, ok := s.fingerprints[key]
	if !ok {
		s.mu.Unlock()
		return
	}

	if fp.count == 0 {
		// Fully quiet window — close the fingerprint.
		delete(s.fingerprints, key)
		if s.ruleFingerprintCount[ruleID] > 0 {
			s.ruleFingerprintCount[ruleID]--
		}
		s.mu.Unlock()
		return
	}

	digest := fp.last
	digest.EventCount = fp.count
	digest.GroupKey = groupKey
	digest.IsDigest = true
	// currentSilence is exactly the interval over which these events accumulated;
	// the digest formatters render it as "N more events in the last <Window>".
	// Set it here (before the bump below) so production digests stop reporting
	// "in the last 0 seconds".
	digest.Window = fp.currentSilence

	// Linear bump of the silence window, capped at maxSilence.
	next := fp.currentSilence + fp.baseSilence
	if fp.maxSilence > 0 && next > fp.maxSilence {
		next = fp.maxSilence
	}
	fp.currentSilence = next
	fp.count = 0
	fp.timer = time.AfterFunc(fp.currentSilence, func() {
		s.flushSilence(ruleID, groupKey, key)
	})

	s.inflightWg.Add(1)
	s.mu.Unlock()

	defer s.inflightWg.Done()
	s.onFlush(ruleID, groupKey, digest)
}

// Stop cancels all pending timers and waits for in-flight flush callbacks.
func (s *Suppressor) Stop() {
	s.mu.Lock()
	s.stopped = true
	for key, fp := range s.fingerprints {
		if fp.timer != nil {
			fp.timer.Stop()
		}
		delete(s.fingerprints, key)
	}
	s.ruleFingerprintCount = make(map[int64]int)
	s.mu.Unlock()

	s.inflightWg.Wait()
}

func fingerprintKey(ruleID int64, groupKey string) string {
	return fmt.Sprintf("%d:%s", ruleID, groupKey)
}

// trimForDigest returns a copy of payload reduced to what the digest path
// renders. Applog events can carry up to 64KB each of Msg and Attrs; no
// notification backend renders Attrs, and formatters display only a slice of
// Msg, so the retained copy strips Attrs and caps Msg at maxRetainedMsgBytes.
// The event is copied — the caller's payload is never mutated.
func trimForDigest(payload Payload) Payload {
	ev := payload.AppLogEvent
	if ev == nil || (len(ev.Attrs) == 0 && len(ev.Msg) <= maxRetainedMsgBytes) {
		return payload
	}
	trimmed := ev.WithAttrsPreview(0)
	if len(trimmed.Msg) > maxRetainedMsgBytes {
		// strings.Clone drops the reference to the original backing array.
		trimmed.Msg = strings.Clone(trimmed.Msg[:maxRetainedMsgBytes])
	}
	payload.AppLogEvent = &trimmed
	return payload
}
