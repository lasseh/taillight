import { defineStore } from 'pinia'
import { ref } from 'vue'
import { config } from '@/lib/config'

interface Features {
  netlog: boolean
  srvlog: boolean
  applog: boolean
}

// useFeaturesStore loads feature flags from the backend so developers don't
// create git diffs when toggling feeds locally. Defaults keep every feed
// enabled so the UI stays functional if the backend is temporarily down.
export const useFeaturesStore = defineStore('features', () => {
  const features = ref<Features>({ netlog: true, srvlog: true, applog: true })
  const loaded = ref(false)
  const error = ref<string | null>(null)

  async function load() {
    try {
      const res = await fetch(`${config.apiUrl}/api/v1/config/features`, {
        signal: AbortSignal.timeout(15000),
      })
      if (!res.ok) throw new Error(`features fetch failed: ${res.status}`)
      features.value = await res.json()
      loaded.value = true
      error.value = null
    } catch (e) {
      error.value = e instanceof Error ? e.message : 'unknown'
    }
  }

  return { features, loaded, error, load }
})
