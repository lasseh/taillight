// @vitest-environment jsdom
//
// Smoke test for the per-file jsdom environment pragma. The global test env is
// 'node' (see vite.config.ts); DOM-dependent units opt in with the pragma above.
// DOMPurify v3 needs a real DOM (`sanitize` is not a function under bare node),
// so this confirms the pragma wiring before the highlighter sanitization tests
// (issue #38) build on it.
import { describe, it, expect } from 'vitest'
import DOMPurify from 'dompurify'

describe('jsdom environment pragma', () => {
  it('provides a DOM so DOMPurify.sanitize is callable', () => {
    expect(typeof DOMPurify.sanitize).toBe('function')
  })

  it('strips a script payload, keeping safe markup', () => {
    const out = DOMPurify.sanitize('<img src=x onerror=alert(1)><b>ok</b><script>alert(2)</script>')
    expect(out).toContain('<b>ok</b>')
    expect(out).not.toContain('onerror')
    expect(out).not.toContain('<script>')
  })
})
