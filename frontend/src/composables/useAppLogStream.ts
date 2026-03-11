import { createEventStream } from '@/composables/useEventStream'
import type { AppLogEvent } from '@/types/applog'

const stream = createEventStream<AppLogEvent>('/api/v1/applog/stream', 'applog')

export function useAppLogStream() {
  return stream
}
