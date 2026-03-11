import { ref, computed, type ComputedRef, type Ref } from 'vue'
import { defineStore } from 'pinia'
import type { VolumeBucket, VolumeDataRecord, VolumeResponse } from '@/types/stats'

/** Base return type for volume dashboard stores */
export interface VolumeDashboardState {
  interval: Ref<string>
  range: Ref<string>
  buckets: Ref<VolumeBucket[]>
  loading: Ref<boolean>
  error: Ref<string | null>
  groups: ComputedRef<string[]>
  chartData: ComputedRef<VolumeDataRecord[]>
  fetchVolume: () => Promise<void>
  setPreset: (r: string, i: string) => void
}

export function createVolumeDashboardStore<K extends string>(
  id: string,
  fetchFn: (params: URLSearchParams) => Promise<VolumeResponse>,
  groupKey: K,
) {
  return defineStore(id, (): VolumeDashboardState & Record<K, ComputedRef<string[]>> => {
    const interval = ref('1m')
    const range = ref('1h')
    const buckets = ref<VolumeBucket[]>([])
    const loading = ref(false)
    const error = ref<string | null>(null)

    /** Unique group names across all buckets, sorted alphabetically. */
    const groups = computed<string[]>(() => {
      const set = new Set<string>()
      for (const b of buckets.value) {
        for (const k of Object.keys(b.by_host)) {
          set.add(k)
        }
      }
      return [...set].sort()
    })

    /** Flat records for Unovis charts. */
    const chartData = computed<VolumeDataRecord[]>(() =>
      buckets.value.map((b) => {
        const rec: VolumeDataRecord = {
          x: new Date(b.time).getTime(),
          total: b.total,
        }
        for (const g of groups.value) {
          rec[g] = b.by_host[g] ?? 0
        }
        return rec
      }),
    )

    async function fetchVolume() {
      loading.value = true
      error.value = null
      try {
        const params = new URLSearchParams({ interval: interval.value, range: range.value })
        const res = await fetchFn(params)
        buckets.value = res.data
      } catch (e) {
        error.value = e instanceof Error ? e.message : `failed to load ${groupKey} volume`
      } finally {
        loading.value = false
      }
    }

    function setPreset(r: string, i: string) {
      range.value = r
      interval.value = i
      fetchVolume()
    }

    // Return with aliased group key for backward compatibility.
    return {
      interval,
      range,
      buckets,
      loading,
      error,
      groups,
      [groupKey]: groups,
      chartData,
      fetchVolume,
      setPreset,
    } as VolumeDashboardState & Record<K, ComputedRef<string[]>>
  })
}
