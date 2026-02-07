<script setup lang="ts">
defineProps<{
  connected: boolean
}>()
</script>

<template>
  <span
    role="status"
    :aria-label="connected ? 'Stream connected' : 'Stream disconnected'"
    class="inline-flex items-center gap-1.5 rounded px-1.5 py-0.5 text-xs"
    :class="connected ? 'bg-t-green/15 text-t-green' : 'bg-t-red/15 text-t-red'"
  >
    <span
      class="inline-block h-1.5 w-1.5 rounded-full"
      aria-hidden="true"
      :class="connected ? 'bg-t-green animate-pulse' : 'bg-t-red'"
    />
    {{ connected ? 'connected' : 'disconnected' }}
  </span>
</template>
