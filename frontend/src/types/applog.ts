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
  // attrs_truncated is true when the server stripped a large attrs blob from
  // a list / SSE response. Detail panels should fetch the full event by id.
  attrs_truncated?: boolean
}

export interface AppLogListResponse {
  data: AppLogEvent[]
  cursor?: string
  has_more: boolean
}

export interface SingleAppLogResponse {
  data: AppLogEvent
}
