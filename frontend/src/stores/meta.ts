import { ref } from 'vue'
import { defineStore } from 'pinia'
import { api } from '@/lib/api'

export const useMetaStore = defineStore('meta', () => {
  const hosts = ref<string[]>([])
  const programs = ref<string[]>([])
  const facilities = ref<number[]>([])
  const loading = ref(false)
  const error = ref<string | null>(null)

  async function fetchAll() {
    loading.value = true
    error.value = null
    try {
      const [h, p, f] = await Promise.all([
        api.getHosts(),
        api.getPrograms(),
        api.getFacilities(),
      ])
      hosts.value = h.data
      programs.value = p.data
      facilities.value = f.data
    } catch (e) {
      error.value = e instanceof Error ? e.message : 'failed to load metadata'
    } finally {
      loading.value = false
    }
  }

  return { hosts, programs, facilities, loading, error, fetchAll }
})
