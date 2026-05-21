import { onBeforeUnmount, ref } from 'vue'

/**
 * usePolling repeatedly calls `fetcher` and re-schedules itself only while
 * `shouldContinue` returns true against the latest result.
 *
 * - The first fetch happens immediately on `start()`.
 * - After each successful fetch, the helper checks `shouldContinue`; if false,
 *   it stops and waits for the next manual `start()` (or restart on data change).
 * - Always stops on component unmount, so it's safe to call from a setup block.
 *
 * Designed for the analysis report list/detail polling pattern: keep ticking
 * while at least one row is pending/running, otherwise sleep.
 */
export function usePolling<T>(
  fetcher: () => Promise<T>,
  shouldContinue: (value: T) => boolean,
  intervalMs = 3000,
) {
  const data = ref<T | null>(null) as { value: T | null }
  const error = ref<unknown>(null)
  const active = ref(false)

  let timer: ReturnType<typeof setTimeout> | null = null
  let cancelled = false
  // runId disambiguates concurrent fetches across start() calls. A tick whose
  // id no longer matches the current run is discarded, preventing two parallel
  // timer chains when start() is called while a previous tick is in flight.
  let runId = 0

  function clearTimer() {
    if (timer !== null) {
      clearTimeout(timer)
      timer = null
    }
  }

  async function tick(id: number) {
    try {
      const next = await fetcher()
      if (cancelled || id !== runId) return
      data.value = next
      error.value = null
      if (shouldContinue(next)) {
        timer = setTimeout(() => tick(id), intervalMs)
      } else {
        active.value = false
        clearTimer()
      }
    } catch (e) {
      if (cancelled || id !== runId) return
      error.value = e
      // Back off but keep polling so transient failures self-heal.
      timer = setTimeout(() => tick(id), intervalMs)
    }
  }

  async function start() {
    if (cancelled) return
    clearTimer()
    runId += 1
    active.value = true
    await tick(runId)
  }

  function stop() {
    runId += 1 // invalidates any in-flight tick
    active.value = false
    clearTimer()
  }

  onBeforeUnmount(() => {
    cancelled = true
    clearTimer()
  })

  return { data, error, active, start, stop }
}
