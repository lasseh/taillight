import { createEventStream } from '@/composables/useEventStream'
import type { NetlogEvent } from '@/types/netlog'

const stream = createEventStream<NetlogEvent>('/api/v1/netlog/stream', 'netlog')

export function useNetlogStream() {
  return stream
}
