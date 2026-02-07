export interface SyslogEvent {
  id: number
  received_at: string
  reported_at: string
  hostname: string
  fromhost_ip: string
  programname: string
  msgid: string
  severity: number
  severity_label: string
  facility: number
  facility_label: string
  syslogtag: string
  structured_data: string | null
  message: string
  raw_message: string | null
  juniper_ref?: JuniperSyslogRef[]
}

export interface SyslogListResponse {
  data: SyslogEvent[]
  cursor?: string
  has_more: boolean
}

export interface JuniperSyslogRef {
  id: number
  name: string
  message: string
  description: string
  type: string
  severity: string
  cause: string
  action: string
  os: string
  created_at: string
}

export interface SingleSyslogResponse {
  data: SyslogEvent
}

export interface MetaResponse<T> {
  data: T[]
}

export interface SyslogFilters {
  hostname: string
  programname: string
  syslogtag: string
  facility: string
  severity_max: string
  search: string
}

/** Option entry for filter dropdowns. */
export interface FilterOption {
  value: string
  label: string
}
