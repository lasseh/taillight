import type { LevelCount, SeverityCount } from '@/types/stats'
import type { SyslogEvent } from '@/types/syslog'
import type { AppLogEvent } from '@/types/applog'

export interface TopMessage {
  pattern: string
  sample: string
  count: number
  latest_id: number
  latest_at: string
  severity: number
  severity_label: string
}

export interface DeviceSummary {
  hostname: string
  last_seen_at: string | null
  total_count: number
  critical_count: number
  severity_breakdown: SeverityCount[]
  top_messages: TopMessage[]
  critical_logs: SyslogEvent[]
}

export interface DeviceSummaryResponse {
  data: DeviceSummary
}

export interface AppLogTopMessage {
  pattern: string
  sample: string
  count: number
  latest_id: number
  latest_at: string
  level: string
}

export interface AppLogDeviceSummary {
  host: string
  last_seen_at: string | null
  total_count: number
  error_count: number
  level_breakdown: LevelCount[]
  top_messages: AppLogTopMessage[]
  error_logs: AppLogEvent[]
}

export interface AppLogDeviceSummaryResponse {
  data: AppLogDeviceSummary
}
