// @vitest-environment jsdom
//
// XSS-regression tests for the highlighter sanitization boundary (issue #38).
// highlighter.ts is the single sanitization chokepoint for the v-html bindings:
// raw log messages from arbitrary network devices flow through highlight()/
// highlightMessage()/highlightJson(), so the {span, class} allow-list must hold
// even if Prism or a grammar extension misbehaves.
import { describe, it, expect, vi, afterEach } from 'vitest'
import { Prism } from '@/lib/prism-global'
import { highlight, highlightJson, highlightMessage } from '@/lib/highlighter'

/** Render highlighter output the way v-html would. */
function render(html: string): HTMLElement {
  const div = document.createElement('div')
  div.innerHTML = html
  return div
}

/** Assert only <span class="..."> markup survived — the PRISM_SANITIZE contract. */
function expectSpanClassOnly(root: HTMLElement) {
  for (const el of Array.from(root.querySelectorAll('*'))) {
    expect(el.tagName).toBe('SPAN')
    for (const attr of el.getAttributeNames()) {
      expect(attr).toBe('class')
    }
  }
}

afterEach(() => {
  vi.restoreAllMocks()
})

describe('highlight — hostile log messages stay inert', () => {
  it('renders an img/onerror payload as text, not markup', () => {
    const payload = '<img src=x onerror=alert(1)>'
    const root = render(highlight(payload))
    expect(root.querySelector('img')).toBeNull()
    expectSpanClassOnly(root)
    // The payload survives as inert text (escaped), not as an element.
    expect(root.textContent).toBe(payload)
  })

  it('renders a script payload as text, not markup', () => {
    const payload = 'kernel: <script>alert(document.cookie)</script> panic'
    const root = render(highlight(payload))
    expect(root.querySelector('script')).toBeNull()
    expectSpanClassOnly(root)
    expect(root.textContent).toBe(payload)
  })

  it('highlights a plain message without altering its text', () => {
    const msg = 'Interface ge-0/0/0 up'
    const root = render(highlight(msg))
    expect(root.querySelector('span')).not.toBeNull() // tokens produced
    expectSpanClassOnly(root)
    expect(root.textContent).toBe(msg)
  })
})

describe('highlightJson — hostile structured payloads stay inert', () => {
  it('strips script markup from JSON string values', () => {
    const root = render(highlightJson({ cmd: '<script>alert(1)</script>' }))
    expect(root.querySelector('script')).toBeNull()
    expectSpanClassOnly(root)
    expect(root.textContent).toContain('<script>alert(1)</script>') // inert text only
  })

  it('returns an empty string for null', () => {
    expect(highlightJson(null)).toBe('')
  })
})

describe('sanitization boundary — DOMPurify allow-list holds even if Prism misbehaves', () => {
  it('strips non-span tags and non-class attrs from raw highlighter output', () => {
    vi.spyOn(Prism, 'highlight').mockReturnValue(
      '<img src=x onerror=alert(1)><span class="token" onclick="evil()">ok</span><a href="javascript:x">link</a>',
    )
    const root = render(highlight('anything'))
    expect(root.querySelector('img')).toBeNull()
    expect(root.querySelector('a')).toBeNull()
    const span = root.querySelector('span')
    expect(span).not.toBeNull()
    expect(span!.getAttribute('class')).toBe('token')
    expect(span!.getAttributeNames()).toEqual(['class'])
    expectSpanClassOnly(root)
  })
})

describe('highlightMessage — memoization and eviction', () => {
  it('memoizes by key: same key served from cache, new key re-highlights', () => {
    const spy = vi.spyOn(Prism, 'highlight').mockReturnValue('<span class="token">x</span>')
    const first = highlightMessage('memo:1', 'error message')
    expect(spy).toHaveBeenCalledTimes(1)
    expect(highlightMessage('memo:1', 'error message')).toBe(first)
    expect(spy).toHaveBeenCalledTimes(1) // cache hit — no re-highlight
    highlightMessage('memo:2', 'error message')
    expect(spy).toHaveBeenCalledTimes(2) // different key — re-highlight
  })

  it('evicts the oldest 500 entries once the cache exceeds 3000', () => {
    const spy = vi.spyOn(Prism, 'highlight').mockReturnValue('<span class="token">x</span>')
    for (let i = 0; i < 3200; i++) {
      highlightMessage(`ev:${i}`, 'msg')
    }
    spy.mockClear()
    highlightMessage('ev:3199', 'msg')
    expect(spy).not.toHaveBeenCalled() // newest entry still cached
    highlightMessage('ev:0', 'msg')
    expect(spy).toHaveBeenCalledTimes(1) // oldest entry was evicted
  })
})
