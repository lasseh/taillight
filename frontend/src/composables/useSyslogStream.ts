import { createEventStream } from '@/composables/useEventStream'
import type { SyslogEvent } from '@/types/syslog'

const stream = createEventStream<SyslogEvent>('/api/v1/syslog/stream', 'syslog')

export function useSyslogStream() {
  return stream
}
