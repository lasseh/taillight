import { watch, type Ref } from 'vue'

// Drives new live-tail events into a device summary so its stat cards update in
// real time between the 30s polls. `deviceLogs` is newest-first and mutated in
// place by createDeviceLogStream (unshift/splice), so we trigger on the newest
// id rather than `watch(deviceLogs)` — a shallow watch on the ref never fires on
// in-place mutation, and watching `.length` misses additions once the buffer is
// capped at 200. `apply` owns the feed-specific mutation (severity vs level
// breakdown, critical vs error bucket); this composable owns only the
// fire-once-per-new-event mechanics and the high-water cursor.
export function useDeviceSummaryLive<T extends { id: number }>(
  deviceLogs: Ref<T[]>,
  apply: (event: T) => void,
) {
  // Only events newer than the cursor are applied. The DB poll already counts
  // everything in the buffer at fetch time, so callers re-baseline on each fresh
  // summary load to avoid replaying the whole buffer (a 200-event jump) or
  // double-counting events the poll already saw.
  let cursor = 0

  function baseline() {
    cursor = deviceLogs.value[0]?.id ?? 0
  }

  watch(
    () => deviceLogs.value[0]?.id,
    () => {
      const logs = deviceLogs.value
      if (logs.length === 0) return
      // Newest-first: walk from the front until we reach an already-seen id.
      for (const event of logs) {
        if (event.id <= cursor) break
        apply(event)
      }
      cursor = logs[0]!.id
    },
  )

  return { baseline }
}
