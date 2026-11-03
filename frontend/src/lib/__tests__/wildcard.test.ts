import { describe, it, expect } from 'vitest'
import { wildcardMatch } from '../wildcard'

describe('wildcardMatch', () => {
  it('matches exact strings (case-insensitive)', () => {
    expect(wildcardMatch('hello', 'hello')).toBe(true)
    expect(wildcardMatch('Hello', 'hello')).toBe(true)
    expect(wildcardMatch('hello', 'Hello')).toBe(true)
  })

  it('rejects non-matching exact strings', () => {
    expect(wildcardMatch('hello', 'world')).toBe(false)
  })

  it('matches everything with single wildcard', () => {
    expect(wildcardMatch('anything', '*')).toBe(true)
    expect(wildcardMatch('', '*')).toBe(true)
  })

  it('matches prefix wildcard', () => {
    expect(wildcardMatch('server-01', '*-01')).toBe(true)
    expect(wildcardMatch('server-02', '*-01')).toBe(false)
  })

  it('matches suffix wildcard', () => {
    expect(wildcardMatch('server-01', 'server-*')).toBe(true)
    expect(wildcardMatch('client-01', 'server-*')).toBe(false)
  })

  it('matches middle wildcard', () => {
    expect(wildcardMatch('server-web-01', 'server-*-01')).toBe(true)
    expect(wildcardMatch('server-db-02', 'server-*-01')).toBe(false)
  })

  it('matches multiple wildcards', () => {
    expect(wildcardMatch('a-b-c', '*-*-*')).toBe(true)
    expect(wildcardMatch('abc', '*-*-*')).toBe(false)
  })

  it('escapes regex metacharacters in non-wildcard parts', () => {
    expect(wildcardMatch('file.log', 'file.log')).toBe(true)
    expect(wildcardMatch('filexlog', 'file.log')).toBe(false)
    expect(wildcardMatch('[test]', '[test]')).toBe(true)
  })

  it('handles empty pattern without wildcard', () => {
    expect(wildcardMatch('', '')).toBe(true)
    expect(wildcardMatch('x', '')).toBe(false)
  })

  it('does not let the first and last segments overlap', () => {
    expect(wildcardMatch('aba', 'ab*ba')).toBe(false)
    expect(wildcardMatch('a', 'a*a')).toBe(false)
    expect(wildcardMatch('aa', 'a*a')).toBe(true)
    expect(wildcardMatch('aab', 'a*ab')).toBe(true)
    expect(wildcardMatch('a', 'a*')).toBe(true)
    expect(wildcardMatch('', '**')).toBe(true)
  })

  // Filters arrive from the URL. A backtracking regex takes seconds on each of
  // these; the segment scan takes microseconds, so the bound is generous.
  it.each([
    ['a run of stars', 'router01', '*'.repeat(40) + '!'],
    ['stars between literals', 'a'.repeat(28), '*a'.repeat(14) + '*!'],
  ])('rejects a pattern with %s quickly', (_name, value, pattern) => {
    const start = performance.now()
    expect(wildcardMatch(value, pattern)).toBe(false)
    expect(performance.now() - start).toBeLessThan(100)
  })
})
