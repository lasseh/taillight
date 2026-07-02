// @vitest-environment jsdom
//
// Tests for createEventStream's resilience logic (issue #37): jittered
// exponential backoff capped at MAX_BACKOFF (30s), the reconnectAfterGap
// counter gated on outages longer than RECONNECT_REFRESH_MS (30s), and the
// heartbeat watchdog declaring disconnect past HEARTBEAT_TIMEOUT (35s).
// Uses a FakeEventSource stub, fake timers, and a stubbed Math.random.
import { describe, it, expect, vi, beforeEach, afterEach } from 'vitest'
import { createEventStream } from '@/composables/useEventStream'

class FakeEventSource {
  static instances: FakeEventSource[] = []
  url: string
  onopen: (() => void) | null = null
  onerror: (() => void) | null = null
  closed = false
  private listeners = new Map<string, Set<(e: MessageEvent) => void>>()

  constructor(url: string) {
    this.url = url
    FakeEventSource.instances.push(this)
  }

  addEventListener(name: string, cb: (e: MessageEvent) => void) {
    let set = this.listeners.get(name)
    if (!set) {
      set = new Set()
      this.listeners.set(name, set)
    }
    set.add(cb)
  }

  close() {
    this.closed = true
  }

  // Test drivers.
  open() {
    this.onopen?.()
  }
  fail() {
    this.onerror?.()
  }
  emit(name: string, init: MessageEventInit = {}) {
    for (const cb of this.listeners.get(name) ?? []) {
      cb(new MessageEvent(name, init))
    }
  }
}

function last(): FakeEventSource {
  return FakeEventSource.instances[FakeEventSource.instances.length - 1] as FakeEventSource
}

let stream: ReturnType<typeof createEventStream<{ id: number }>> | null = null

beforeEach(() => {
  FakeEventSource.instances = []
  vi.useFakeTimers()
  vi.stubGlobal('EventSource', FakeEventSource)
  // random=1 → jitter multiplier (0.5 + random*0.5) = 1.0, deterministic.
  vi.spyOn(Math, 'random').mockReturnValue(1)
})

afterEach(() => {
  stream?.stop()
  stream = null
  vi.useRealTimers()
  vi.unstubAllGlobals()
  vi.restoreAllMocks()
})

describe('backoff', () => {
  it('doubles the retry delay and caps it at MAX_BACKOFF (30s)', () => {
    stream = createEventStream('/api/v1/srvlog/stream', 'srvlog')
    stream.start()
    expect(FakeEventSource.instances.length).toBe(1)

    // First retry fires after INITIAL_BACKOFF; each subsequent delay doubles
    // (jitter pinned to 1.0) until the 30s cap holds.
    const expectedDelays = [1000, 2000, 4000, 8000, 16000, 30000, 30000]
    for (const delay of expectedDelays) {
      const count = FakeEventSource.instances.length
      last().fail()
      vi.advanceTimersByTime(delay - 1)
      expect(FakeEventSource.instances.length).toBe(count) // not yet
      vi.advanceTimersByTime(1)
      expect(FakeEventSource.instances.length).toBe(count + 1) // reconnect at exactly the backoff
    }
  })

  it('applies the jitter lower bound (0.5x) to the doubled delay', () => {
    vi.spyOn(Math, 'random').mockReturnValue(0) // multiplier 0.5
    stream = createEventStream('/api/v1/srvlog/stream', 'srvlog')
    stream.start()

    last().fail()
    vi.advanceTimersByTime(1000) // first retry: INITIAL_BACKOFF, no jitter yet
    expect(FakeEventSource.instances.length).toBe(2)

    last().fail()
    vi.advanceTimersByTime(999) // doubled 2000 * 0.5 jitter = 1000
    expect(FakeEventSource.instances.length).toBe(2)
    vi.advanceTimersByTime(1)
    expect(FakeEventSource.instances.length).toBe(3)
  })

  it('resets backoff after a successful reconnect', () => {
    stream = createEventStream('/api/v1/srvlog/stream', 'srvlog')
    stream.start()

    last().fail()
    vi.advanceTimersByTime(1000)
    last().fail()
    vi.advanceTimersByTime(2000) // backoff had grown to 2000
    expect(FakeEventSource.instances.length).toBe(3)

    last().open() // successful connect resets backoff to INITIAL_BACKOFF
    last().fail()
    vi.advanceTimersByTime(1000)
    expect(FakeEventSource.instances.length).toBe(4)
  })
})

describe('reconnectAfterGap', () => {
  it('does not increment after a short outage', () => {
    stream = createEventStream('/api/v1/srvlog/stream', 'srvlog')
    stream.start()
    last().open()
    expect(stream.reconnectAfterGap.value).toBe(0)

    last().fail()
    vi.advanceTimersByTime(1000)
    last().open() // outage 1s < 30s threshold
    expect(stream.reconnectAfterGap.value).toBe(0)
  })

  it('increments once when the outage exceeds RECONNECT_REFRESH_MS (30s)', () => {
    stream = createEventStream('/api/v1/srvlog/stream', 'srvlog')
    stream.start()
    last().open()

    last().fail() // outage starts now
    vi.advanceTimersByTime(1000) // retry opens a new (not yet connected) source
    vi.advanceTimersByTime(29000) // connection stays down; total gap 30s
    last().open()
    expect(stream.reconnectAfterGap.value).toBe(1)

    // The gap start is cleared on reconnect: a following short outage
    // does not increment again.
    last().fail()
    vi.advanceTimersByTime(1000)
    last().open()
    expect(stream.reconnectAfterGap.value).toBe(1)
  })
})

describe('heartbeat watchdog', () => {
  it('stays connected while heartbeats arrive, then declares disconnect past HEARTBEAT_TIMEOUT', () => {
    stream = createEventStream('/api/v1/srvlog/stream', 'srvlog')
    stream.start()
    const es = last()
    es.open()
    expect(stream.connected.value).toBe(true)

    // A heartbeat at t=30s defers the watchdog: without it, the 35s timeout
    // would trip at t=40s.
    vi.advanceTimersByTime(30000)
    es.emit('heartbeat')
    vi.advanceTimersByTime(30000)
    expect(stream.connected.value).toBe(true)
    expect(es.closed).toBe(false)

    // Silence past 35s since the heartbeat → watchdog tears down (t=70s).
    vi.advanceTimersByTime(10000)
    expect(stream.connected.value).toBe(false)
    expect(es.closed).toBe(true)

    // ...and a reconnect is scheduled.
    const count = FakeEventSource.instances.length
    vi.advanceTimersByTime(1000)
    expect(FakeEventSource.instances.length).toBe(count + 1)
  })
})

describe('subscribe', () => {
  it('delivers parsed events, stops after unsubscribe, and resumes with lastEventId', () => {
    stream = createEventStream('/api/v1/srvlog/stream', 'srvlog')
    const events: { id: number }[] = []
    const unsubscribe = stream.subscribe((e) => events.push(e))
    stream.start()
    last().open()

    last().emit('srvlog', { data: JSON.stringify({ id: 1 }), lastEventId: '42' })
    expect(events).toEqual([{ id: 1 }])

    unsubscribe()
    last().emit('srvlog', { data: JSON.stringify({ id: 2 }) })
    expect(events).toEqual([{ id: 1 }])

    // Reconnect resumes from the last seen event id.
    last().fail()
    vi.advanceTimersByTime(1000)
    expect(last().url).toContain('lastEventId=42')
  })
})
