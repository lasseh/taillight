import type { LocationQuery } from 'vue-router'
import type { MuteFeed } from '@/types/mute'

// "Mute similar" hands an event to the Mutes tab through the URL. The keys
// are mute_-prefixed because the feed filter stores read hostname, search
// etc. from the URL on boot, whatever the route, so unprefixed keys would
// set the feed filters.

export interface MuteMatch {
  hostname: string
  programname: string
  search: string
}

export interface MutePrefill extends MuteMatch {
  feed: MuteFeed
}

// 120 code points is at most 480 UTF-8 bytes, under the server's 500-byte
// field cap. Slicing by code point never splits a surrogate pair, which
// would make encodeURIComponent throw.
const PREFILL_SEARCH_CODE_POINTS = 120

const HOUR_MS = 60 * 60 * 1000

/** Expiry choices for create and "expire in…", sent as duration_ms. */
export const EXPIRY_PRESETS = [
  { label: '1h', ms: HOUR_MS },
  { label: '4h', ms: 4 * HOUR_MS },
  { label: '24h', ms: 24 * HOUR_MS },
  { label: '7d', ms: 7 * 24 * HOUR_MS },
] as const

export const DEFAULT_DURATION_MS = 24 * HOUR_MS

/** Query for /notifications that opens the Mutes tab with a create form prefilled from the event. */
export function muteSimilarQuery(
  feed: MuteFeed,
  event: { hostname: string; programname: string; message: string },
): Record<string, string> {
  return {
    tab: 'mutes',
    mute_feed: feed,
    mute_hostname: event.hostname,
    mute_programname: event.programname,
    mute_search: Array.from(event.message).slice(0, PREFILL_SEARCH_CODE_POINTS).join(''),
  }
}

function queryString(v: LocationQuery[string] | undefined): string {
  return typeof v === 'string' ? v : ''
}

/** Reads a prefill back from the query; null unless mute_feed is netlog or srvlog. */
export function parseMutePrefill(query: LocationQuery): MutePrefill | null {
  const feed = query.mute_feed
  if (feed !== 'netlog' && feed !== 'srvlog') return null
  return {
    feed,
    hostname: queryString(query.mute_hostname),
    programname: queryString(query.mute_programname),
    search: queryString(query.mute_search),
  }
}

/**
 * Mirrors the server's rule validation: at least one match field must be
 * non-empty after trimming, and a hostname made only of '*' counts as empty
 * because it would match every host.
 */
export function hasMatchField(m: MuteMatch): boolean {
  return (
    m.hostname.trim().replace(/^\*+|\*+$/g, '') !== '' ||
    m.programname.trim() !== '' ||
    m.search.trim() !== ''
  )
}
