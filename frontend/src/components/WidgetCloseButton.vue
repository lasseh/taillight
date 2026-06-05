<script setup lang="ts">
// Remove button shown on dashboard widgets in edit mode. Keep `v-if="editing"`
// on the call site; this component owns only the button markup so the SVG lives
// once instead of being copy-pasted per widget.
defineEmits<{ close: [] }>()
</script>

<template>
  <button class="absolute -right-1.5 -top-1.5 z-10 flex h-5 w-5 items-center justify-center rounded-full border border-t-border bg-t-bg-dark text-t-fg-dark transition-colors hover:border-t-red hover:text-t-red" @click="$emit('close')"><svg class="h-3 w-3" viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2" stroke-linecap="round" stroke-linejoin="round"><line x1="18" y1="6" x2="6" y2="18" /><line x1="6" y1="6" x2="18" y2="18" /></svg></button>
</template>
