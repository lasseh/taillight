// @vitest-environment jsdom
import { describe, expect, it, vi } from 'vitest'

// router.ts is evaluated as part of the initial static import graph, which in a
// production bundle happens before main.ts awaits loadFeatures() — so the flags
// it sees at that moment are the pre-fetch fallbacks (analysis: false). These
// tests pin the two properties that keep that harmless: the analysis routes are
// registered regardless, and the gate is re-read per navigation.
const flags = vi.hoisted(() => ({ analysis: false }))

vi.mock('@/lib/features', () => ({
  features: () => ({
    netlog: true,
    srvlog: true,
    applog: true,
    analysis: flags.analysis,
    oidc: false,
  }),
  featuresLoaded: () => true,
}))

type PropsFn = (to: { params: Record<string, string> }) => Record<string, unknown>

async function propsFor(name: string): Promise<PropsFn> {
  const { default: router } = await import('@/router')
  const record = router.getRoutes().find((r) => r.name === name)
  if (!record) throw new Error(`no route named ${name}`)
  return record.props.default as PropsFn
}

describe('analysis routes', () => {
  it('are registered even though the flags read false when the table is built', async () => {
    const { default: router } = await import('@/router')

    expect(flags.analysis).toBe(false)
    expect(router.hasRoute('analysis')).toBe(true)
    expect(router.hasRoute('analysis-report')).toBe(true)
  })

  it('gate on the flag as it stands at navigation time, not at import time', async () => {
    const analysis = await propsFor('analysis')
    const report = await propsFor('analysis-report')
    const to = { params: { slug: 'daily-2026-08-10' } }

    // Same module instance, flag flipped after it was imported.
    flags.analysis = false
    expect(analysis(to)).toEqual({ feature: 'analysis' })
    expect(report(to)).toEqual({ feature: 'analysis' })

    flags.analysis = true
    expect(analysis(to)).toEqual({})
    expect(report(to)).toEqual({ slug: 'daily-2026-08-10' })
  })
})
