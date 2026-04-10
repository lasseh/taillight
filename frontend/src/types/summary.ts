export interface SummarySchedule {
  id: number
  name: string
  enabled: boolean
  frequency: 'daily' | 'weekly' | 'monthly'
  day_of_week?: number | null
  day_of_month?: number | null
  time_of_day: string
  timezone: string
  event_kinds: string[]
  severity_max?: number | null
  hostname?: string
  top_n: number
  channel_ids: number[]
  last_run_at?: string | null
  created_at: string
  updated_at: string
}

export interface SummaryScheduleListResponse {
  data: SummarySchedule[]
}

export interface SummaryScheduleResponse {
  data: SummarySchedule
}
