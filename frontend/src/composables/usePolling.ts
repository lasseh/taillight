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

  function clearTimer() {
    if (timer !== null) {
      clearTimeout(timer)
      timer = null
    }
  }

  async function tick() {
    try {
      const next = await fetcher()
      if (cancelled) return
      data.value = next
      error.value = null
      if (shouldContinue(next)) {
        timer = setTimeout(tick, intervalMs)
      } else {
        active.value = false
        clearTimer()
      }
    } catch (e) {
      if (cancelled) return
      error.value = e
      // Back off but keep polling so transient failures self-heal.
      timer = setTimeout(tick, intervalMs)
    }
  }

  async function start() {
    if (cancelled) return
    clearTimer()
    active.value = true
    await tick()
  }

  function stop() {
    active.value = false
    clearTimer()
  }

  onBeforeUnmount(() => {
    cancelled = true
    clearTimer()
  })

  return { data, error, active, start, stop }
}
