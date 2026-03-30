import type { JuniperNetlogRef, MetaResponse, FilterOption } from '@/types/srvlog'

export type { JuniperNetlogRef, MetaResponse, FilterOption }

export interface NetlogEvent {
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

export interface NetlogListResponse {
  data: NetlogEvent[]
  cursor?: string
  has_more: boolean
}

export interface SingleNetlogResponse {
  data: {
    event: NetlogEvent
    juniper_ref?: JuniperNetlogRef[]
  }
}
