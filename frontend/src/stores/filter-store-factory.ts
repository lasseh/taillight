import { computed, reactive, watch } from 'vue'
import { defineStore } from 'pinia'
import { useRoute, useRouter } from 'vue-router'

type StringRecord<K extends string> = { [P in K]: string }

/**
 * Creates a Pinia filter store with URL sync.
 *
 * @param id - Pinia store identifier
 * @param filterKeys - List of filter field names
 * @param routeName - Route name for URL sync guard
 */
export function createFilterStore<K extends string>(
  id: string,
  filterKeys: readonly K[],
  routeName: string,
) {
  return defineStore(id, () => {
    const route = useRoute()
    const router = useRouter()

    const filters = reactive(
      Object.fromEntries(filterKeys.map((k) => [k, ''])),
    ) as StringRecord<K>

    /** Non-empty filter entries as a plain record for URLSearchParams. */
    const activeFilters = computed(() => {
      const result: Record<string, string> = {}
      for (const key of filterKeys) {
        if (filters[key]) {
          result[key] = filters[key]
        }
      }
      return result
    })

    const hasActiveFilters = computed(() => Object.keys(activeFilters.value).length > 0)

    function clearAll() {
      for (const key of filterKeys) {
        (filters as Record<string, string>)[key] = ''
      }
    }

    /** Read filter state from URL query params on initial mount. */
    function initFromURL() {
      const query = route.query
      for (const key of filterKeys) {
        const val = query[key as string]
        if (typeof val === 'string' && val) {
          (filters as Record<string, string>)[key] = val
        }
      }
    }

    /** Sync filter state back to URL (replace, no navigation). */
    function syncToURL() {
      if (route.name !== routeName) return
      const query: Record<string, string> = {}
      for (const key of filterKeys) {
        if (filters[key]) {
          query[key] = filters[key]
        }
      }
      router.replace({ name: route.name ?? undefined, query })
    }

    // Auto-sync to URL whenever filters change.
    watch(filters, syncToURL, { deep: true })

    return { filters, activeFilters, hasActiveFilters, clearAll, initFromURL }
  })
}
