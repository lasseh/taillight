import { createEventStream } from '@/composables/useEventStream'
import type { SrvlogEvent } from '@/types/srvlog'

const stream = createEventStream<SrvlogEvent>('/api/v1/srvlog/stream', 'srvlog')

export function useSrvlogStream() {
  return stream
}
