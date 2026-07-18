import { config } from '@/lib/config'

export interface Features {
  netlog: boolean
  srvlog: boolean
  applog: boolean
  analysis: boolean
  oidc: boolean
}

// Feeds default on (fail-open) since they're enabled by default; analysis
// and oidc default off (fail-closed) since they're opt-in — on a fetch
// failure we'd rather briefly hide a working link than show a dead one on
// most deploys.
let cached: Features = { netlog: true, srvlog: true, applog: true, analysis: false, oidc: false }

export async function loadFeatures(): Promise<void> {
  try {
    const res = await fetch(`${config.apiUrl}/api/v1/config/features`, {
      signal: AbortSignal.timeout(15000),
    })
    if (!res.ok) {
      console.warn(`features fetch failed: ${res.status}`)
      return
    }
    cached = await res.json()
  } catch (e) {
    console.warn('failed to load feature flags, using defaults:', e)
  }
}

export function features(): Features {
  return cached
}
