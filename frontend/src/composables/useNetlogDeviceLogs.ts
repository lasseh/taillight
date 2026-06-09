import { api } from '@/lib/api'
import type { NetlogEvent } from '@/types/netlog'
import { createDeviceLogStream } from './useDeviceLogStream'

export const useNetlogDeviceLogs = createDeviceLogStream<NetlogEvent>({
  fetch: api.getNetlogs,
  streamPath: '/api/v1/netlog/stream',
  streamName: 'netlog',
})
