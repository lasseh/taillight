import { createApp } from 'vue'
import { createPinia } from 'pinia'
import App from './App.vue'
import { loadFeatures } from '@/lib/features'
import '@fontsource/jetbrains-mono/400.css'
import '@fontsource/jetbrains-mono/500.css'
import '@fontsource/jetbrains-mono/600.css'
import '@fontsource/jetbrains-mono/700.css'
import './style.css'
import './lib/prism-junos.css'

async function bootstrap() {
  // Features are loaded before the router is built so feature-gated routes see
  // real values. Defaults (all enabled) are used if the fetch fails.
  await loadFeatures()

  const app = createApp(App)
  app.use(createPinia())

  const { default: router } = await import('./router')
  app.use(router)

  app.mount('#app')
}

bootstrap()
