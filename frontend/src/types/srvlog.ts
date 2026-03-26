export interface SrvlogEvent {
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
  juniper_ref?: JuniperNetlogRef[]
}

export interface SrvlogListResponse {
  data: SrvlogEvent[]
  cursor?: string
  has_more: boolean
}

export interface JuniperNetlogRef {
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

export interface SingleSrvlogResponse {
  data: SrvlogEvent
}

export interface MetaResponse<T> {
  data: T[]
}

/** Option entry for filter dropdowns. */
export interface FilterOption {
  value: string
  label: string
  colorClass?: string
}
