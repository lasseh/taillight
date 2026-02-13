import { ref, watch, type Ref, onUnmounted } from 'vue'
import { config } from '@/lib/config'
import { api } from '@/lib/api'
import type { AppLogEvent } from '@/types/applog'

const INITIAL_BACKOFF = 1000
const MAX_BACKOFF = 30000
const HEARTBEAT_TIMEOUT = 35000

export function useAppLogDeviceLogs(hostname: Ref<string>) {
  const events = ref<AppLogEvent[]>([])
  const connected = ref(false)

  let es: EventSource | null = null
  let retryTimer: ReturnType<typeof setTimeout> | null = null
  let watchdog: ReturnType<typeof setInterval> | null = null
  let lastEventAt = 0
  let backoff = INITIAL_BACKOFF
  let abortController: AbortController | null = null
  let disposed = false

  const seenIds = new Set<number>()

  function addEvent(event: AppLogEvent) {
    if (seenIds.has(event.id)) return
    seenIds.add(event.id)
    events.value.unshift(event)
    if (events.value.length > 200) {
      const removed = events.value.splice(200)
      for (const e of removed) seenIds.delete(e.id)
    }
  }

  async function fetchInitial(host: string) {
    abortController = new AbortController()
    try {
      const params = new URLSearchParams({ host, limit: '50' })
      const res = await api.getAppLogs(params, abortController.signal)
      for (const e of res.data) {
        if (!seenIds.has(e.id)) {
          seenIds.add(e.id)
          events.value.push(e)
        }
      }
    } catch {
      // Silently ignore — SSE will fill in live data.
    } finally {
      abortController = null
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
    if (retryTimer || disposed) return
    retryTimer = setTimeout(() => {
      retryTimer = null
      if (!disposed) openStream()
      backoff = Math.min(backoff * 2, MAX_BACKOFF)
    }, backoff)
  }

  function teardown() {
    stopWatchdog()
    if (es) {
      es.close()
      es = null
    }
  }

  function openStream() {
    const host = hostname.value
    if (!host) return
    const url = `${config.apiUrl}/api/v1/applog/stream?host=${encodeURIComponent(host)}`
    es = new EventSource(url)

    es.addEventListener('applog', (e: MessageEvent) => {
      lastEventAt = Date.now()
      try {
        const event: AppLogEvent = JSON.parse(e.data)
        addEvent(event)
      } catch {
        // Ignore parse errors.
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

  function start() {
    events.value = []
    seenIds.clear()
    backoff = INITIAL_BACKOFF
    const host = hostname.value
    if (!host) return
    fetchInitial(host)
    openStream()
  }

  function stop() {
    if (retryTimer) {
      clearTimeout(retryTimer)
      retryTimer = null
    }
    if (abortController) {
      abortController.abort()
      abortController = null
    }
    teardown()
    connected.value = false
  }

  watch(hostname, () => {
    disposed = false
    stop()
    start()
  }, { immediate: true })

  onUnmounted(() => {
    disposed = true
    stop()
  })

  return { events, connected }
}
