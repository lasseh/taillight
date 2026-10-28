export type MuteFeed = 'netlog' | 'srvlog'

/**
 * A mute rule. Match fields are always present; "" means the field is not
 * matched on. A rule is active while server now < expires_at; there is no
 * active flag.
 */
export interface MuteRule {
  id: number
  feed: MuteFeed
  hostname: string
  programname: string
  search: string
  reason: string
  created_by: string
  created_at: string
  expires_at: string
}

/** POST /api/v1/mute-rules body. The server computes expires_at from duration_ms. */
export interface CreateMuteRuleRequest {
  feed: MuteFeed
  hostname: string
  programname: string
  search: string
  reason: string
  duration_ms: number
}

export interface MuteRuleListResponse {
  data: MuteRule[]
}

export interface MuteRuleResponse {
  data: MuteRule
}
