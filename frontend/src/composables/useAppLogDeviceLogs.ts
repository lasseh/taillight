import { api } from '@/lib/api'
import type { AppLogEvent } from '@/types/applog'
import { createDeviceLogStream } from './useDeviceLogStream'

export const useAppLogDeviceLogs = createDeviceLogStream<AppLogEvent>({
  fetch: api.getAppLogs,
  streamPath: '/api/v1/applog/stream',
  streamName: 'applog',
  paramKey: 'host',
})
