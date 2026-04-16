import { config } from '@/lib/config'

export interface Features {
  netlog: boolean
  srvlog: boolean
  applog: boolean
}

let cached: Features = { netlog: true, srvlog: true, applog: true }

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
