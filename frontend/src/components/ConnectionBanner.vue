<script setup lang="ts">
import { ref, watch, onUnmounted } from 'vue'

const props = defineProps<{
  connected: boolean
}>()

// Track whether we've ever been connected. Until the first successful
// connection, we suppress the banner to avoid flashing on page load.
const hasConnected = ref(false)
const showBanner = ref(false)

let graceTimer: ReturnType<typeof setTimeout> | null = null

watch(() => props.connected, (val) => {
  if (val) {
    hasConnected.value = true
    if (graceTimer) {
      clearTimeout(graceTimer)
      graceTimer = null
    }
    showBanner.value = false
  } else if (hasConnected.value) {
    // Only show after a 2s delay to avoid flicker during brief reconnects.
    if (!graceTimer) {
      graceTimer = setTimeout(() => {
        graceTimer = null
        if (!props.connected) {
          showBanner.value = true
        }
      }, 2000)
    }
  }
}, { immediate: true })

onUnmounted(() => {
  if (graceTimer) clearTimeout(graceTimer)
})
</script>

<template>
  <Transition
    enter-active-class="transition-all duration-300 ease-out"
    leave-active-class="transition-all duration-200 ease-in"
    enter-from-class="opacity-0 -translate-y-2"
    enter-to-class="opacity-100 translate-y-0"
    leave-from-class="opacity-100 translate-y-0"
    leave-to-class="opacity-0 -translate-y-2"
  >
    <div
      v-if="showBanner"
      role="alert"
      class="bg-t-red/10 border-t-red/30 flex items-center justify-center gap-3 border-b px-4 py-2"
    >
      <svg class="text-t-red h-5 w-5 shrink-0" viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="1.5" stroke-linecap="round" stroke-linejoin="round">
        <line x1="1" y1="1" x2="23" y2="23" />
        <path d="M16.72 11.06A10.94 10.94 0 0 1 19 12.55" />
        <path d="M5 12.55a10.94 10.94 0 0 1 5.17-2.39" />
        <path d="M10.71 5.05A16 16 0 0 1 22.56 9" />
        <path d="M1.42 9a15.91 15.91 0 0 1 4.7-2.88" />
        <path d="M8.53 16.11a6 6 0 0 1 6.95 0" />
        <line x1="12" y1="20" x2="12.01" y2="20" />
      </svg>
      <div class="text-center">
        <p class="text-t-fg text-sm font-semibold">Cannot connect to server</p>
        <p class="text-t-fg-dark text-xs">The API may be down or restarting. Retrying automatically...</p>
      </div>
    </div>
  </Transition>
</template>
