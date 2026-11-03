import { ref } from 'vue'
import { defineStore } from 'pinia'
import { api } from '@/lib/api'

export const useAppLogMetaStore = defineStore('applog-meta', () => {
  const services = ref<string[]>([])
  const components = ref<string[]>([])
  const hosts = ref<string[]>([])
  const loading = ref(false)
  const error = ref<string | null>(null)

  async function fetchAll() {
    loading.value = true
    error.value = null
    try {
      const [s, c, h] = await Promise.all([
        api.getAppLogServices(),
        api.getAppLogComponents(),
        api.getAppLogHosts(),
      ])
      services.value = s.data
      components.value = c.data
      hosts.value = h.data
    } catch (e) {
      error.value = e instanceof Error ? e.message : 'failed to load metadata'
    } finally {
      loading.value = false
    }
  }

  // The loaded lists are capped; these look typed text up on the server.
  async function searchServices(q: string) {
    return (await api.getAppLogServices(q)).data
  }

  async function searchComponents(q: string) {
    return (await api.getAppLogComponents(q)).data
  }

  async function searchHosts(q: string) {
    return (await api.getAppLogHosts(q)).data
  }

  return {
    services,
    components,
    hosts,
    loading,
    error,
    fetchAll,
    searchServices,
    searchComponents,
    searchHosts,
  }
})
