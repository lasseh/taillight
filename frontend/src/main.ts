import { createApp } from 'vue'
import { createPinia } from 'pinia'
import App from './App.vue'
import { loadFeatures } from '@/lib/features'
import './assets/fonts/jetbrains-mono.css'
import './style.css'
import './lib/prism-junos.css'

async function bootstrap() {
  // Features are loaded before the router is built so feature-gated routes see
  // real values. If the fetch fails after retries the defaults in lib/features
  // stand (feeds on, analysis and oidc off) and featuresLoaded() reports false,
  // which is how the gated views tell "disabled" from "never found out".
  await loadFeatures()

  const app = createApp(App)
  app.use(createPinia())

  const { default: router } = await import('./router')
  app.use(router)

  app.mount('#app')
}

bootstrap()
