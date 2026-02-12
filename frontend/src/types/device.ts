import type { SeverityCount } from '@/types/stats'

export interface TopMessage {
  pattern: string
  sample: string
  count: number
}

export interface DeviceSummary {
  hostname: string
  last_seen_at: string | null
  critical_count: number
  severity_breakdown: SeverityCount[]
  top_messages: TopMessage[]
}

export interface DeviceSummaryResponse {
  data: DeviceSummary
}
