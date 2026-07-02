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
  // source_ip is the resolved client IP captured by the ingest handler.
  // Absent on rows ingested before this field was added.
  source_ip?: string
  // api_key_id identifies the API key that ingested this row. null when
  // inserted via session auth or before this field was added.
  api_key_id: string | null
}

export interface AppLogListResponse {
  data: AppLogEvent[]
  cursor?: string
  has_more: boolean
}

export interface SingleAppLogResponse {
  data: AppLogEvent
}
