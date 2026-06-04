import { ref } from 'vue'
import { config } from '@/lib/config'

const INITIAL_BACKOFF = 1000
const MAX_BACKOFF = 30000
const HEARTBEAT_TIMEOUT = 35000 // ~2x server heartbeat (15s)
// Reconnects after this much offline time trigger a full history refresh in
// consumers, since the server's SSE backfill is capped at 100 events and a
// long outage leaves an invisible gap between backfill and live events.
const RECONNECT_REFRESH_MS = 30000

export function createEventStream<T>(path: string, eventName: string) {
  // Module-level singleton state.
  let es: EventSource | null = null
  let retryTimer: ReturnType<typeof setTimeout> | null = null
  let watchdog: ReturnType<typeof setInterval> | null = null
  let lastEventAt = 0
  let lastEventId = ''
  let backoff = INITIAL_BACKOFF
  let started = false
  // Earliest time the stream became unhealthy in the current outage. Captures
  // the start of the gap, not intermediate retries.
  let disconnectedSince: number | null = null
  const connected = ref(false)
  // Bumped each time we reconnect after an outage longer than
  // RECONNECT_REFRESH_MS. Consumers watch this to trigger a history refresh.
  const reconnectAfterGap = ref(0)
  const listeners = new Set<(event: T) => void>()

  // When the page becomes visible after sleep/tab switch, immediately
  // attempt reconnection with reset backoff so users don't see the
  // disconnected banner while the network catches up.
  function onVisibilityChange() {
    if (document.visibilityState === 'visible' && started && !connected.value && !es) {
      if (retryTimer) {
        clearTimeout(retryTimer)
        retryTimer = null
      }
      backoff = INITIAL_BACKOFF
      open()
    }
  }
  document.addEventListener('visibilitychange', onVisibilityChange)

  function open() {
    const baseUrl = `${config.apiUrl}${path}`
    // Device-scoped streams pass a path that already has a query string
    // (e.g. ?hostname=...), so the lastEventId param must be joined with & in
    // that case, not a second ?. Bare-path global streams use ?.
    const connectUrl = lastEventId
      ? `${baseUrl}${baseUrl.includes('?') ? '&' : '?'}lastEventId=${encodeURIComponent(lastEventId)}`
      : baseUrl
    es = new EventSource(connectUrl)

    es.addEventListener(eventName, (e: MessageEvent) => {
      lastEventAt = Date.now()
      if (e.lastEventId) lastEventId = e.lastEventId
      let event: T
      try {
        event = JSON.parse(e.data)
      } catch (err) {
        console.error(`${eventName} stream: failed to parse event`, err)
        return
      }
      for (const cb of listeners) {
        cb(event)
      }
    })

    es.addEventListener('heartbeat', () => {
      lastEventAt = Date.now()
    })

    es.onopen = () => {
      connected.value = true
      backoff = INITIAL_BACKOFF
      lastEventAt = Date.now()
      if (disconnectedSince !== null) {
        if (Date.now() - disconnectedSince >= RECONNECT_REFRESH_MS) {
          reconnectAfterGap.value++
        }
        disconnectedSince = null
      }
      startWatchdog()
    }

    es.onerror = () => {
      connected.value = false
      teardown()
      scheduleRetry()
    }
  }

  function startWatchdog() {
    stopWatchdog()
    watchdog = setInterval(() => {
      if (Date.now() - lastEventAt > HEARTBEAT_TIMEOUT) {
        connected.value = false
        teardown()
        scheduleRetry()
      }
    }, 5000)
  }

  function stopWatchdog() {
    if (watchdog) {
      clearInterval(watchdog)
      watchdog = null
    }
  }

  function scheduleRetry() {
    stopWatchdog()
    if (retryTimer) return
    retryTimer = setTimeout(() => {
      retryTimer = null
      open()
      backoff = Math.min(backoff * 2, MAX_BACKOFF) * (0.5 + Math.random() * 0.5)
    }, backoff)
  }

  function teardown() {
    stopWatchdog()
    if (es) {
      es.close()
      es = null
    }
    // Mark the start of the outage on the first teardown; later retries don't
    // overwrite, so we measure from when the stream first went unhealthy.
    if (started && disconnectedSince === null) {
      disconnectedSince = Date.now()
    }
  }

  function start() {
    if (es || retryTimer) return
    started = true
    backoff = INITIAL_BACKOFF
    disconnectedSince = null
    open()
  }

  function stop() {
    started = false
    if (retryTimer) {
      clearTimeout(retryTimer)
      retryTimer = null
    }
    teardown()
    connected.value = false
    document.removeEventListener('visibilitychange', onVisibilityChange)
  }

  function subscribe(cb: (event: T) => void) {
    listeners.add(cb)
    return () => listeners.delete(cb)
  }

  return { connected, reconnectAfterGap, start, stop, subscribe }
}
