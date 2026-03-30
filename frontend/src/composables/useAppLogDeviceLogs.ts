import { ref, watch, type Ref, onUnmounted } from 'vue'
import { api } from '@/lib/api'
import type { AppLogEvent } from '@/types/applog'
import { createEventStream } from './useEventStream'

export function useAppLogDeviceLogs(hostname: Ref<string>) {
  const events = ref<AppLogEvent[]>([])
  const connected = ref(false)

  const seenIds = new Set<number>()
  let stream: ReturnType<typeof createEventStream<AppLogEvent>> | null = null
  let unsubscribe: (() => void) | null = null
  let stopConnectedSync: (() => void) | null = null
  let abortController: AbortController | null = null
  let initialLoadComplete = false
  let loadVersion = 0

  function addEvent(event: AppLogEvent) {
    if (!initialLoadComplete) return
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
      const params = new URLSearchParams({ host, limit: '100' })
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

  function cleanup() {
    unsubscribe?.()
    unsubscribe = null
    stream?.stop()
    stream = null
    stopConnectedSync?.()
    stopConnectedSync = null
    if (abortController) {
      abortController.abort()
      abortController = null
    }
    initialLoadComplete = false
    connected.value = false
  }

  watch(hostname, async (host) => {
    cleanup()
    events.value = []
    seenIds.clear()
    const version = ++loadVersion
    if (!host) return

    const path = `/api/v1/applog/stream?host=${encodeURIComponent(host)}`
    stream = createEventStream<AppLogEvent>(path, 'applog')
    unsubscribe = stream.subscribe(addEvent)
    stopConnectedSync = watch(stream.connected, (v) => { connected.value = v })
    stream.start()

    await fetchInitial(host)
    if (version !== loadVersion) return
    initialLoadComplete = true
  }, { immediate: true })

  onUnmounted(() => {
    cleanup()
  })

  return { events, connected }
}
