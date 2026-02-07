import { watchEffect, type Ref } from 'vue'

// Tailwind emerald-500 / pink-500
const COLOR_CONNECTED = '#10b981'
const COLOR_DISCONNECTED = '#ec4899'

export function useFavicon(connected: Ref<boolean>) {
  let link = document.querySelector<HTMLLinkElement>('link[rel="icon"]')
  if (!link) {
    link = document.createElement('link')
    link.rel = 'icon'
    link.type = 'image/svg+xml'
    document.head.appendChild(link)
  }

  watchEffect(() => {
    const color = connected.value ? COLOR_CONNECTED : COLOR_DISCONNECTED

    const svg = `<svg xmlns="http://www.w3.org/2000/svg" viewBox="0 0 24 24" fill="${color}"><path fill-rule="evenodd" d="M3 6.75A.75.75 0 0 1 3.75 6h16.5a.75.75 0 0 1 0 1.5H3.75A.75.75 0 0 1 3 6.75ZM3 12a.75.75 0 0 1 .75-.75H12a.75.75 0 0 1 0 1.5H3.75A.75.75 0 0 1 3 12Zm0 5.25a.75.75 0 0 1 .75-.75h16.5a.75.75 0 0 1 0 1.5H3.75a.75.75 0 0 1-.75-.75Z" clip-rule="evenodd"/></svg>`
    link!.href = `data:image/svg+xml,${encodeURIComponent(svg)}`
  })
}
