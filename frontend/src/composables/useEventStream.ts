import { ref } from 'vue'
import { config } from '@/lib/config'

const INITIAL_BACKOFF = 1000
const MAX_BACKOFF = 30000
const HEARTBEAT_TIMEOUT = 35000 // ~2x server heartbeat (15s)

export function createEventStream<T>(path: string, eventName: string) {
  // Module-level singleton state.
  let es: EventSource | null = null
  let retryTimer: ReturnType<typeof setTimeout> | null = null
  let watchdog: ReturnType<typeof setInterval> | null = null
  let lastEventAt = 0
  let lastEventId = ''
  let backoff = INITIAL_BACKOFF
  const connected = ref(false)
  const listeners = new Set<(event: T) => void>()

  function open() {
    const baseUrl = `${config.apiUrl}${path}`
    const connectUrl = lastEventId ? `${baseUrl}?lastEventId=${lastEventId}` : baseUrl
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
  }

  function start() {
    if (es || retryTimer) return
    backoff = INITIAL_BACKOFF
    open()
  }

  function stop() {
    if (retryTimer) {
      clearTimeout(retryTimer)
      retryTimer = null
    }
    teardown()
    connected.value = false
  }

  function subscribe(cb: (event: T) => void) {
    listeners.add(cb)
    return () => listeners.delete(cb)
  }

  return { connected, start, stop, subscribe }
}
