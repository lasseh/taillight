// Golden-fixture contract tests (architecture review X7).
//
// The JSON fixtures are generated and byte-verified by the Go side in
// api/internal/handler/golden_test.go (regenerate with
// `go test ./internal/handler -run TestGolden -update` from api/, then re-run
// this suite). They are imported here via relative path — no copy step — so
// both sides assert the exact same files and a contract change breaks
// whichever side wasn't updated.
//
// Each golden is locked in two directions:
// - compile time: assigning it to the wire type fails `npm run type-check`
//   if the golden lost a field the type requires or a field changed type;
// - runtime: expectOnlyKnownKeys fails if the golden gained a key the TS
//   type does not declare (the key lists are `satisfies`-checked against
//   the types, so they cannot list stale keys either).
import { describe, expect, it } from 'vitest'

import type { AppLogEvent } from '@/types/applog'
import type { NetlogEvent } from '@/types/netlog'
import type { SingleSrvlogResponse, SrvlogEvent, SrvlogListResponse } from '@/types/srvlog'

import applogEventGolden from '../../../../api/internal/handler/testdata/golden/applog_event.json'
import applogEventNilGolden from '../../../../api/internal/handler/testdata/golden/applog_event_nil_fields.json'
import applogEventTruncatedGolden from '../../../../api/internal/handler/testdata/golden/applog_event_attrs_truncated.json'
import ingestLimitsGolden from '../../../../api/internal/handler/testdata/golden/applog_ingest_limits.json'
import ingestRequestGolden from '../../../../api/internal/handler/testdata/golden/applog_ingest_request.json'
import detailEnvelopeGolden from '../../../../api/internal/handler/testdata/golden/detail_envelope.json'
import listEnvelopeGolden from '../../../../api/internal/handler/testdata/golden/list_envelope.json'
import listEnvelopeLastPageGolden from '../../../../api/internal/handler/testdata/golden/list_envelope_last_page.json'
import netlogEventGolden from '../../../../api/internal/handler/testdata/golden/netlog_event.json'
import netlogEventNilGolden from '../../../../api/internal/handler/testdata/golden/netlog_event_nil_fields.json'
import srvlogEventGolden from '../../../../api/internal/handler/testdata/golden/srvlog_event.json'
import srvlogEventNilGolden from '../../../../api/internal/handler/testdata/golden/srvlog_event_nil_fields.json'

function expectOnlyKnownKeys(golden: object, known: readonly string[]) {
  const unknown = Object.keys(golden).filter((key) => !known.includes(key))
  expect(unknown, 'golden has keys the TS type does not declare').toEqual([])
}

// srvlog and netlog share the same wire shape by design.
const syslogEventKeys = [
  'id',
  'received_at',
  'reported_at',
  'hostname',
  'fromhost_ip',
  'programname',
  'msgid',
  'severity',
  'severity_label',
  'facility',
  'facility_label',
  'syslogtag',
  'structured_data',
  'message',
  'raw_message',
] as const satisfies readonly (keyof SrvlogEvent)[] satisfies readonly (keyof NetlogEvent)[]

const applogEventKeys = [
  'id',
  'received_at',
  'timestamp',
  'level',
  'service',
  'component',
  'host',
  'msg',
  'source',
  'attrs',
  'attrs_truncated',
  'source_ip',
  'api_key_id',
] as const satisfies readonly (keyof AppLogEvent)[]

describe('event shape goldens', () => {
  it('SrvlogEvent matches the golden wire shape', () => {
    const full: SrvlogEvent = srvlogEventGolden
    expect(full.id).toBe(1001)
    expect(full.structured_data).toBeTypeOf('string')
    expectOnlyKnownKeys(srvlogEventGolden, syslogEventKeys)

    // Nil pointer fields (structured_data, raw_message) are omitted, not null.
    const minimal: SrvlogEvent = srvlogEventNilGolden
    expect(minimal.structured_data).toBeUndefined()
    expect(minimal.raw_message).toBeUndefined()
    expectOnlyKnownKeys(srvlogEventNilGolden, syslogEventKeys)
  })

  it('NetlogEvent matches the golden wire shape', () => {
    const full: NetlogEvent = netlogEventGolden
    expect(full.id).toBe(2001)
    expectOnlyKnownKeys(netlogEventGolden, syslogEventKeys)

    const minimal: NetlogEvent = netlogEventNilGolden
    expect(minimal.structured_data).toBeUndefined()
    expect(minimal.raw_message).toBeUndefined()
    expectOnlyKnownKeys(netlogEventNilGolden, syslogEventKeys)
  })

  it('AppLogEvent matches the golden wire shape', () => {
    const full: AppLogEvent = applogEventGolden
    expect(full.id).toBe(3001)
    expect(full.attrs).toBeTypeOf('object')
    expect(full.api_key_id).toBeTypeOf('string')
    expectOnlyKnownKeys(applogEventGolden, applogEventKeys)

    // List/SSE preview: oversized attrs stripped to null and flagged.
    const truncated: AppLogEvent = applogEventTruncatedGolden
    expect(truncated.attrs).toBeNull()
    expect(truncated.attrs_truncated).toBe(true)
    expectOnlyKnownKeys(applogEventTruncatedGolden, applogEventKeys)

    // Session-auth / pre-migration rows: attrs and api_key_id are null,
    // source_ip and attrs_truncated are omitted.
    const minimal: AppLogEvent = applogEventNilGolden
    expect(minimal.attrs).toBeNull()
    expect(minimal.api_key_id).toBeNull()
    expect(minimal.source_ip).toBeUndefined()
    expect(minimal.attrs_truncated).toBeUndefined()
    expectOnlyKnownKeys(applogEventNilGolden, applogEventKeys)
  })
})

describe('envelope goldens', () => {
  it('list envelope matches {data, cursor, has_more}', () => {
    const page: SrvlogListResponse = listEnvelopeGolden
    expect(page.data).toHaveLength(2)
    expect(page.cursor).toBeTypeOf('string')
    expect(page.has_more).toBe(true)
    expectOnlyKnownKeys(listEnvelopeGolden, ['data', 'cursor', 'has_more'])
  })

  it('last-page list envelope omits cursor and keeps data as []', () => {
    const page: SrvlogListResponse = listEnvelopeLastPageGolden
    expect(page.data).toEqual([])
    expect(page.cursor).toBeUndefined()
    expect(page.has_more).toBe(false)
    expectOnlyKnownKeys(listEnvelopeLastPageGolden, ['data', 'has_more'])
  })

  it('detail envelope matches {data: event}', () => {
    const detail: SingleSrvlogResponse = detailEnvelopeGolden
    expect(detail.data.id).toBe(1001)
    expectOnlyKnownKeys(detailEnvelopeGolden, ['data'])
    expectOnlyKnownKeys(detailEnvelopeGolden.data, syslogEventKeys)
  })
})

describe('applog ingest request golden', () => {
  // The SPA never ingests; this locks the contract the shippers and SDKs
  // build against (limits are generated from the server's own constants and
  // proven against the real handler in golden_test.go).
  it('obeys the server ingest rules', () => {
    expect(ingestRequestGolden.logs.length).toBeGreaterThan(0)
    expect(ingestRequestGolden.logs.length).toBeLessThanOrEqual(ingestLimitsGolden.max_batch_size)

    for (const entry of ingestRequestGolden.logs) {
      for (const field of ingestLimitsGolden.required_fields) {
        expect(entry, `entry is missing required field ${field}`).toHaveProperty(field)
      }
      expect(entry.service).not.toBe('')
      expect(new TextEncoder().encode(entry.msg).length).toBeLessThanOrEqual(
        ingestLimitsGolden.max_msg_bytes,
      )
    }
  })
})
