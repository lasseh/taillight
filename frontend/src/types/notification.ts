export interface NotificationChannel {
  id: number
  name: string
  type: 'slack' | 'webhook'
  config: Record<string, unknown>
  enabled: boolean
  created_at: string
  updated_at: string
}

export interface NotificationRule {
  id: number
  name: string
  enabled: boolean
  event_kind: 'syslog' | 'applog'

  // Syslog filter fields.
  hostname?: string
  programname?: string
  severity?: number | null
  severity_max?: number | null
  facility?: number | null
  syslogtag?: string
  msgid?: string

  // AppLog filter fields.
  service?: string
  component?: string
  host?: string
  level?: string

  // Shared filter field.
  search?: string

  // Notification behavior.
  channel_ids: number[]
  burst_window: number
  cooldown_seconds: number

  created_at: string
  updated_at: string
}

export interface NotificationLogEntry {
  id: number
  created_at: string
  rule_id: number
  channel_id: number
  event_kind: string
  event_id: number
  status: string
  reason?: string | null
  event_count: number
  status_code?: number | null
  duration_ms: number
  payload?: Record<string, unknown> | null
}

export interface ChannelListResponse {
  data: NotificationChannel[]
}

export interface ChannelResponse {
  data: NotificationChannel
}

export interface RuleListResponse {
  data: NotificationRule[]
}

export interface RuleResponse {
  data: NotificationRule
}

export interface LogListResponse {
  data: NotificationLogEntry[]
}

export interface TestChannelResult {
  success: boolean
  status_code: number
  error?: string
  duration_ms: number
}
