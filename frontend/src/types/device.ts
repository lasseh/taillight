import type { LevelCount, SeverityCount } from '@/types/stats'
import type { SrvlogEvent } from '@/types/srvlog'
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

export interface ActivityBucket {
  time: string
  count: number
}

export interface DeviceSummary {
  hostname: string
  fromhost_ip: string
  last_seen_at: string | null
  total_count: number
  critical_count: number
  severity_breakdown: SeverityCount[]
  top_messages: TopMessage[]
  critical_logs: SrvlogEvent[]
  activity: ActivityBucket[]
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
  activity: ActivityBucket[]
}

export interface AppLogDeviceSummaryResponse {
  data: AppLogDeviceSummary
}
