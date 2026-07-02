<script setup lang="ts">
// The dashboard time-range button row, repeated once per dashboard section.
import { rangePresets } from '@/lib/ranges'

defineProps<{ range: string }>()
defineEmits<{ select: [value: string] }>()
</script>

<template>
  <span class="flex items-center gap-1">
    <button
      v-for="p in rangePresets"
      :key="p.value"
      class="rounded px-1.5 py-0.5 text-xs transition-colors"
      :class="
        range === p.value ? 'bg-t-bg-highlight text-t-purple' : 'text-t-fg-dark hover:text-t-fg'
      "
      @click="$emit('select', p.value)"
    >
      {{ p.label }}
    </button>
  </span>
</template>
