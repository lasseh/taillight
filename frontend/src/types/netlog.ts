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
  structured_data?: string
  message: string
  raw_message?: string
  /** Id of the mute rule that matched; absent (never null) when unmuted. */
  muted_by?: number
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
