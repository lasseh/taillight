import { ref } from 'vue'
import { defineStore } from 'pinia'
import { api, ApiError } from '@/lib/api'
import type { AuthUser } from '@/types/auth'
import router from '@/router'

export const useAuthStore = defineStore('auth', () => {
  const user = ref<AuthUser | null>(null)
  const ready = ref(false)

  async function init() {
    try {
      const res = await api.getMe()
      user.value = res.user
      ready.value = true
    } catch (e) {
      user.value = null
      if (e instanceof ApiError && e.status === 401) {
        // Auth enabled, not logged in — definitive state.
        ready.value = true
      }
      // API unreachable (network error, 5xx): leave ready false so the
      // router guard retries init() on the next navigation.
    }
  }

  async function login(username: string, password: string) {
    const res = await api.login(username, password)
    user.value = res.user
  }

  async function logout() {
    try {
      await api.logout()
    } catch {
      // Ignore errors — clear local state regardless.
    }
    user.value = null
    router.push('/login')
  }

  return { user, ready, init, login, logout }
})
