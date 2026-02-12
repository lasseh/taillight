import type { SeverityCount, TopSource } from '@/types/stats'

export interface TopMessage {
  pattern: string
  sample: string
  count: number
  latest_id: number
}

export interface DeviceSummary {
  hostname: string
  last_seen_at: string | null
  total_count: number
  critical_count: number
  severity_breakdown: SeverityCount[]
  top_programs: TopSource[]
  top_messages: TopMessage[]
}

export interface DeviceSummaryResponse {
  data: DeviceSummary
}
