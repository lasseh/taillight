import { describe, it, expect, vi, beforeEach, afterEach } from 'vitest'
import { formatNumber, formatRelativeTime, lastSeenColorClass, formatAttrs } from '../format'

describe('formatNumber', () => {
  it('returns plain number for small values', () => {
    expect(formatNumber(0)).toBe('0')
    expect(formatNumber(42)).toBe('42')
    expect(formatNumber(999)).toBe('999')
  })

  it('formats thousands with k suffix', () => {
    expect(formatNumber(1000)).toBe('1.0k')
    expect(formatNumber(1500)).toBe('1.5k')
    expect(formatNumber(999999)).toBe('1000.0k')
  })

  it('formats millions with M suffix', () => {
    expect(formatNumber(1000000)).toBe('1.0M')
    expect(formatNumber(2500000)).toBe('2.5M')
  })
})

describe('formatRelativeTime', () => {
  beforeEach(() => {
    vi.useFakeTimers()
    vi.setSystemTime(new Date('2025-01-15T12:00:00Z'))
  })

  afterEach(() => {
    vi.useRealTimers()
  })

  it('formats seconds ago', () => {
    expect(formatRelativeTime('2025-01-15T11:59:30Z')).toBe('30s ago')
  })

  it('formats minutes ago', () => {
    expect(formatRelativeTime('2025-01-15T11:55:00Z')).toBe('5 min ago')
  })

  it('formats hours ago', () => {
    expect(formatRelativeTime('2025-01-15T10:00:00Z')).toBe('2h ago')
  })

  it('formats days ago', () => {
    expect(formatRelativeTime('2025-01-13T12:00:00Z')).toBe('2d ago')
  })
})

describe('lastSeenColorClass', () => {
  beforeEach(() => {
    vi.useFakeTimers()
    vi.setSystemTime(new Date('2025-01-15T12:00:00Z'))
  })

  afterEach(() => {
    vi.useRealTimers()
  })

  it('returns green for recent timestamps (< 15 min)', () => {
    expect(lastSeenColorClass('2025-01-15T11:50:00Z')).toBe('text-t-green')
  })

  it('returns yellow for moderate staleness (< 2h)', () => {
    expect(lastSeenColorClass('2025-01-15T10:30:00Z')).toBe('text-t-yellow')
  })

  it('returns red for stale timestamps (> 2h)', () => {
    expect(lastSeenColorClass('2025-01-15T09:00:00Z')).toBe('text-t-red')
  })
})

describe('formatAttrs', () => {
  it('formats key-value pairs', () => {
    expect(formatAttrs({ a: 'b', c: 'd' })).toBe('a=b c=d')
  })

  it('stringifies non-string values', () => {
    expect(formatAttrs({ count: 42, flag: true })).toBe('count=42 flag=true')
  })

  it('handles empty object', () => {
    expect(formatAttrs({})).toBe('')
  })
})
