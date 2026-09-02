import { describe, expect, it } from 'vitest'
import {
  ANALYSIS_FEEDS,
  ANALYSIS_FEED_ORDER,
  feedAllowsFrequency,
  feedAllowsMode,
  feedOptions,
  feedScopeNoun,
  feedSpec,
} from '@/lib/analysis-feeds'
import { feedBadgeClass, reportTitle } from '@/lib/analysis-format'

describe('analysis feed spec', () => {
  it('lists every feed once, in picker order', () => {
    expect(feedOptions.map((o) => o.value)).toEqual([...ANALYSIS_FEED_ORDER])
    expect(new Set(ANALYSIS_FEED_ORDER).size).toBe(Object.keys(ANALYSIS_FEEDS).length)
  })

  it('keeps applog daily-only and service-scoped', () => {
    expect(feedAllowsMode('applog', 'daily')).toBe(true)
    expect(feedAllowsMode('applog', 'weekly')).toBe(false)
    expect(feedAllowsFrequency('applog', 'monthly')).toBe(false)
    expect(feedAllowsFrequency('srvlog', 'monthly')).toBe(true)
    expect(feedScopeNoun('applog')).toBe('service')
    expect(feedScopeNoun('netlog')).toBe('host')
  })

  it('falls back for a feed this build does not know', () => {
    expect(feedSpec('future')).toBeUndefined()
    expect(feedBadgeClass('future' as never)).toBe('bg-t-fg-dark/10 text-t-fg-dark')
  })

  it('titles reports with the feed label and its own scope kind', () => {
    expect(reportTitle({ feed: 'applog', prompt_mode: 'daily', services: ['api', 'worker'] })).toBe(
      'Applog daily brief · 2 services',
    )
    expect(reportTitle({ feed: 'netlog', prompt_mode: 'weekly', hosts: ['a'] })).toBe(
      'Netlog weekly review · 1 host',
    )
  })
})
