import { config } from '@/lib/config'

export interface Features {
  netlog: boolean
  srvlog: boolean
  applog: boolean
  analysis: boolean
  oidc: boolean
}

// Defaults that stand in until the real flags arrive. Feeds default on (they're
// always enabled server-side); analysis and oidc default off since they're
// opt-in — we'd rather briefly hide a working link than show a dead one.
//
// These defaults are indistinguishable from a real "off" answer, so anything
// that reports a feature as unavailable must consult featuresLoaded() first.
let cached: Features = { netlog: true, srvlog: true, applog: true, analysis: false, oidc: false }
let loaded = false

// The router is built once from this response, so a single failed fetch hides a
// feature for the life of the page. An API restart mid-deploy is exactly that
// window, hence the retries. Delays are per-retry; total worst case stays close
// to the previous single 15s attempt.
const ATTEMPT_TIMEOUT_MS = 5000
const RETRY_DELAYS_MS = [250, 750]

export async function loadFeatures(): Promise<void> {
  for (let attempt = 0; ; attempt++) {
    try {
      const res = await fetch(`${config.apiUrl}/api/v1/config/features`, {
        signal: AbortSignal.timeout(ATTEMPT_TIMEOUT_MS),
      })
      if (!res.ok) {
        throw new Error(`unexpected status ${res.status}`)
      }
      cached = await res.json()
      loaded = true
      return
    } catch (e) {
      const delay = RETRY_DELAYS_MS[attempt]
      if (delay === undefined) {
        console.warn('failed to load feature flags, using defaults:', e)
        return
      }
      console.warn(`features fetch failed, retrying in ${delay}ms:`, e)
      await new Promise((resolve) => setTimeout(resolve, delay))
    }
  }
}

export function features(): Features {
  return cached
}

// featuresLoaded reports whether features() reflects the server's answer or the
// fallback defaults. False means we never found out: a feature that looks
// disabled may well be enabled, so tell the user to reload rather than claiming
// the instance doesn't have it.
export function featuresLoaded(): boolean {
  return loaded
}
