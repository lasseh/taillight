// @vitest-environment jsdom
// The store imports the router, which needs a DOM history.
import { describe, it, expect, beforeEach, vi } from 'vitest'
import { setActivePinia, createPinia } from 'pinia'
import { api, ApiError } from '@/lib/api'
import { useAuthStore } from '../auth'

beforeEach(() => {
  setActivePinia(createPinia())
  vi.restoreAllMocks()
})

describe('auth.init', () => {
  // A 503 means the server could not check the session, not that the session
  // is gone: apiError keeps the login view on its retrying "cannot connect"
  // state instead of showing the sign-in form.
  it('reports an unavailable session check as an API error, not a logout', async () => {
    vi.spyOn(api, 'getMe').mockRejectedValue(
      new ApiError(503, 'auth_unavailable', 'credential check unavailable, retry shortly'),
    )
    const auth = useAuthStore()
    await auth.init()
    expect(auth.user).toBeNull()
    expect(auth.apiError).toBe('503 — credential check unavailable, retry shortly')
  })

  it('treats 401 as not logged in', async () => {
    vi.spyOn(api, 'getMe').mockRejectedValue(new ApiError(401, 'unauthorized', 'unauthorized'))
    const auth = useAuthStore()
    await auth.init()
    expect(auth.user).toBeNull()
    expect(auth.apiError).toBeNull()
  })
})
