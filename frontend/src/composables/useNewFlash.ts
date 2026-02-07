import { reactive, watch, onUnmounted } from 'vue'
import type { Ref } from 'vue'

const FLASH_DURATION = 1000
const GRACE_PERIOD = 2000

/**
 * Tracks new items in a reactive array and exposes a Set of "new" IDs
 * that auto-clear after a short flash duration.
 * Skips the initial load via a grace period.
 */
export function useNewFlash<T extends { id: number }>(source: Ref<T[]> | (() => T[])) {
  const newIds = reactive(new Set<number>())
  let timers: Record<number, ReturnType<typeof setTimeout>> = {}
  let ready = false
  let readyTimer: ReturnType<typeof setTimeout> | null = null

  watch(
    typeof source === 'function' ? source : () => source.value,
    (curr, prev) => {
      if (!ready) {
        if (curr.length > 0 && !readyTimer) {
          readyTimer = setTimeout(() => { ready = true }, GRACE_PERIOD)
        }
        return
      }
      const prevIds = new Set((prev ?? []).map(e => e.id))
      for (const e of curr) {
        if (!prevIds.has(e.id)) {
          newIds.add(e.id)
          timers[e.id] = setTimeout(() => {
            newIds.delete(e.id)
            delete timers[e.id]
          }, FLASH_DURATION)
        }
      }
    },
  )

  onUnmounted(() => {
    Object.values(timers).forEach(clearTimeout)
    timers = {}
    if (readyTimer) clearTimeout(readyTimer)
  })

  return newIds
}
