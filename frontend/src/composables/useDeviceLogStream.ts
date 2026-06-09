import { ref, watch, type Ref, onUnmounted } from 'vue'
import { createEventStream } from './useEventStream'

interface DeviceLogStreamConfig<T extends { id: number }> {
  /** Fetch the initial page of logs for a host (api.getSrvlogs / getNetlogs / getAppLogs). */
  fetch: (params: URLSearchParams, signal: AbortSignal) => Promise<{ data: T[] }>
  /** SSE stream path prefix, e.g. '/api/v1/srvlog/stream'. */
  streamPath: string
  /** SSE event name, e.g. 'srvlog'. */
  streamName: string
  /** Query-param key for the hostname. 'hostname' for srvlog/netlog, 'host' for applog. */
  paramKey?: string
}

/**
 * Builds a device-scoped live-tail composable: initial REST page + SSE stream,
 * id-deduped, newest-first, capped at 200, with abort + load-version race
 * guarding and full teardown on host change / unmount. The three feeds differ
 * only in the config below; the live-tail logic lives here once.
 */
export function createDeviceLogStream<T extends { id: number }>(config: DeviceLogStreamConfig<T>) {
  const paramKey = config.paramKey ?? 'hostname'

  return function useDeviceLogStream(hostname: Ref<string>) {
    const events = ref<T[]>([]) as Ref<T[]>
    const connected = ref(false)

    const seenIds = new Set<number>()
    let stream: ReturnType<typeof createEventStream<T>> | null = null
    let unsubscribe: (() => void) | null = null
    let stopConnectedSync: (() => void) | null = null
    let abortController: AbortController | null = null
    let initialLoadComplete = false
    let loadVersion = 0

    function addEvent(event: T) {
      if (!initialLoadComplete) return
      if (seenIds.has(event.id)) return
      seenIds.add(event.id)
      events.value.unshift(event)
      // Cap at 200 entries to avoid unbounded growth.
      if (events.value.length > 200) {
        const removed = events.value.splice(200)
        for (const e of removed) seenIds.delete(e.id)
      }
    }

    async function fetchInitial(host: string) {
      abortController = new AbortController()
      try {
        const params = new URLSearchParams({ [paramKey]: host, limit: '100' })
        const res = await config.fetch(params, abortController.signal)
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

    watch(
      hostname,
      async (host) => {
        cleanup()
        events.value = []
        seenIds.clear()
        const version = ++loadVersion
        if (!host) return

        const path = `${config.streamPath}?${paramKey}=${encodeURIComponent(host)}`
        stream = createEventStream<T>(path, config.streamName)
        unsubscribe = stream.subscribe(addEvent)
        stopConnectedSync = watch(stream.connected, (v) => {
          connected.value = v
        })
        stream.start()

        await fetchInitial(host)
        if (version !== loadVersion) return
        initialLoadComplete = true
      },
      { immediate: true },
    )

    onUnmounted(() => {
      cleanup()
    })

    return { events, connected }
  }
}
