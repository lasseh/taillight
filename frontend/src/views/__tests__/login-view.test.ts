// @vitest-environment jsdom
import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest'
import { mount, type VueWrapper } from '@vue/test-utils'
import { createPinia, setActivePinia } from 'pinia'
import { createMemoryHistory, createRouter } from 'vue-router'
import LoginView from '../LoginView.vue'
import { useAuthStore } from '@/stores/auth'
import { slashT } from '@/components/logo/mark'

// The auth store imports the app's router; the view gets its own below.
vi.mock('@/router', () => ({ default: {} }))

const drawnCells = slashT.join('').split('#').length - 1

// jsdom has no 2D canvas and no matchMedia.
const ctx = { setTransform: vi.fn(), clearRect: vi.fn(), fillRect: vi.fn(), fillStyle: '' }
function stubBrowser(reducedMotion: boolean) {
  vi.spyOn(HTMLCanvasElement.prototype, 'getContext').mockReturnValue(
    ctx as unknown as CanvasRenderingContext2D,
  )
  vi.stubGlobal('matchMedia', () => ({ matches: reducedMotion }))
  vi.stubGlobal(
    'requestAnimationFrame',
    vi.fn(() => 1),
  )
  vi.stubGlobal('cancelAnimationFrame', vi.fn())
}

let wrapper: VueWrapper | undefined

async function mountLogin(apiError: string | null = null) {
  setActivePinia(createPinia())
  useAuthStore().apiError = apiError
  const router = createRouter({
    history: createMemoryHistory(),
    routes: [{ path: '/login', component: LoginView }],
  })
  await router.push('/login')
  wrapper = mount(LoginView, { global: { plugins: [router] } })
  return wrapper
}

beforeEach(() => {
  ctx.fillRect.mockClear()
})

afterEach(() => {
  wrapper?.unmount()
  wrapper = undefined
  vi.restoreAllMocks()
  vi.unstubAllGlobals()
})

describe('LoginView mark', () => {
  it('draws the mark over the card and keeps the [Taillight] heading', async () => {
    stubBrowser(false)
    const w = await mountLogin()
    const mark = w.find('canvas')
    expect(mark.exists()).toBe(true)
    expect(mark.attributes('aria-hidden')).toBe('true')
    expect(mark.element.nextElementSibling?.classList.contains('login-card')).toBe(true)
    expect(w.find('h1').text()).toBe('[Taillight]')
    expect(w.find('form').exists()).toBe(true)
  })

  it('keeps the mark when the server cannot be reached', async () => {
    stubBrowser(false)
    const w = await mountLogin('502 — Bad Gateway')
    expect(w.text()).toContain('Cannot connect to server')
    expect(w.find('canvas').exists()).toBe(true)
    expect(w.find('h1').text()).toBe('[Taillight]')
  })

  it('runs the traffic frame by frame', async () => {
    stubBrowser(false)
    await mountLogin()
    expect(requestAnimationFrame).toHaveBeenCalled()
  })

  it('stands still in its ink under reduced motion', async () => {
    stubBrowser(true)
    await mountLogin()
    expect(requestAnimationFrame).not.toHaveBeenCalled()
    expect(ctx.fillRect).toHaveBeenCalledTimes(drawnCells)
  })
})
