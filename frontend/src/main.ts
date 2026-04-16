import { createApp } from 'vue'
import { createPinia } from 'pinia'
import App from './App.vue'
import { useFeaturesStore } from '@/stores/features'
import '@fontsource/jetbrains-mono/400.css'
import '@fontsource/jetbrains-mono/500.css'
import '@fontsource/jetbrains-mono/600.css'
import '@fontsource/jetbrains-mono/700.css'
import './style.css'
import './lib/prism-junos.css'

const app = createApp(App)
app.use(createPinia())

// Load feature flags before building the router so route tables see real
// values. On failure we keep the default (all enabled) so the UI stays
// usable if the backend is temporarily down.
const features = useFeaturesStore()
await features.load()
if (features.error) {
  console.warn('failed to load feature flags, using defaults:', features.error)
}

// Router must be imported after features are loaded because router.ts reads
// the feature flags at module evaluation time.
const { default: router } = await import('./router')
app.use(router)

app.mount('#app')
