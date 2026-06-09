import { api } from '@/lib/api'
import type { SrvlogEvent } from '@/types/srvlog'
import { createDeviceLogStream } from './useDeviceLogStream'

export const useDeviceLogs = createDeviceLogStream<SrvlogEvent>({
  fetch: api.getSrvlogs,
  streamPath: '/api/v1/srvlog/stream',
  streamName: 'srvlog',
})
