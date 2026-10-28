import { describe, it, expect } from 'vitest'
import { hasMatchField, muteSimilarQuery, parseMutePrefill } from '@/lib/mute-rules'

const flap = {
  hostname: '00a-leaf-d6e11-02',
  programname: 'kernel',
  message: 'lag_bundlestate_ifd_change: bundle ae30: bundle IFD minimum links not met',
}

function hasLoneSurrogate(s: string): boolean {
  for (let i = 0; i < s.length; i++) {
    const c = s.charCodeAt(i)
    if (c >= 0xd800 && c <= 0xdbff) {
      const next = s.charCodeAt(i + 1)
      if (!(next >= 0xdc00 && next <= 0xdfff)) return true
      i++
    } else if (c >= 0xdc00 && c <= 0xdfff) {
      return true
    }
  }
  return false
}

describe('muteSimilarQuery / parseMutePrefill', () => {
  it('round-trips a prefill', () => {
    const query = muteSimilarQuery('netlog', flap)
    expect(query.tab).toBe('mutes')
    expect(parseMutePrefill(query)).toEqual({
      feed: 'netlog',
      hostname: '00a-leaf-d6e11-02',
      programname: 'kernel',
      search: 'lag_bundlestate_ifd_change: bundle ae30: bundle IFD minimum links not met',
    })
  })

  it('round-trips through a real URL', () => {
    const query = muteSimilarQuery('srvlog', { ...flap, message: 'a&b=c %20 ü' })
    const back = Object.fromEntries(new URLSearchParams(query))
    expect(parseMutePrefill(back)).toEqual({
      feed: 'srvlog',
      hostname: '00a-leaf-d6e11-02',
      programname: 'kernel',
      search: 'a&b=c %20 ü',
    })
  })

  it('uses only mute_-prefixed keys besides tab', () => {
    const keys = Object.keys(muteSimilarQuery('netlog', flap))
    expect(keys.filter((k) => k !== 'tab').every((k) => k.startsWith('mute_'))).toBe(true)
  })

  it.each([
    ['missing', {}],
    ['applog', { mute_feed: 'applog', mute_hostname: 'h' }],
    ['empty', { mute_feed: '', mute_hostname: 'h' }],
    ['array', { mute_feed: ['netlog'], mute_hostname: 'h' }],
    ['null', { mute_feed: null, mute_hostname: 'h' }],
  ])('gives no prefill for a %s feed', (_name, query) => {
    expect(parseMutePrefill(query)).toBeNull()
  })

  it('reads non-string field values as empty', () => {
    expect(
      parseMutePrefill({ mute_feed: 'srvlog', mute_hostname: ['a', 'b'], mute_search: null }),
    ).toEqual({ feed: 'srvlog', hostname: '', programname: '', search: '' })
  })

  it.each([
    ['119 ASCII then emoji', 'a'.repeat(119) + '😀'.repeat(5)],
    ['all emoji', '🏳️‍🌈😀👍🏽'.repeat(80)],
    ['CJK', '链路聚合组成员链路断开'.repeat(30)],
  ])('truncates a %s message to 120 code points', (_name, message) => {
    const search = muteSimilarQuery('netlog', { ...flap, message }).mute_search!
    expect(Array.from(search)).toHaveLength(120)
    expect(message.startsWith(search)).toBe(true)
    expect(hasLoneSurrogate(search)).toBe(false)
    expect(() => encodeURIComponent(search)).not.toThrow()
    expect(new TextEncoder().encode(search).length).toBeLessThanOrEqual(500)
  })

  it('keeps a short message whole', () => {
    expect(muteSimilarQuery('netlog', { ...flap, message: 'bundle ae30' }).mute_search).toBe(
      'bundle ae30',
    )
  })
})

describe('hasMatchField', () => {
  const none = { hostname: '', programname: '', search: '' }

  it.each(['*', '**', ' * ', '   '])('treats hostname %j as empty', (hostname) => {
    expect(hasMatchField({ ...none, hostname })).toBe(false)
  })

  it.each(['*a', 'router*', 'a*', '* *'])('treats hostname %j as set', (hostname) => {
    expect(hasMatchField({ ...none, hostname })).toBe(true)
  })

  it('treats whitespace-only program and search as empty', () => {
    expect(hasMatchField({ hostname: '', programname: '  ', search: '\t' })).toBe(false)
  })

  it('treats a program or search as set', () => {
    expect(hasMatchField({ ...none, programname: 'rpd' })).toBe(true)
    expect(hasMatchField({ ...none, search: 'bundle ae30' })).toBe(true)
  })
})
