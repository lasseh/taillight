export interface AppLogEvent {
  id: number
  received_at: string
  timestamp: string
  level: string
  service: string
  component: string
  host: string
  msg: string
  source: string
  attrs: Record<string, unknown> | null
}

export interface AppLogListResponse {
  data: AppLogEvent[]
  cursor?: string
  has_more: boolean
}

export interface SingleAppLogResponse {
  data: AppLogEvent
}

export interface AppLogFilters {
  service: string
  component: string
  host: string
  level: string
  search: string
}
