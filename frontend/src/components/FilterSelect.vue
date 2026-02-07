<script setup lang="ts">
import type { FilterOption } from '@/types/syslog'

defineProps<{
  label: string
  options: FilterOption[]
}>()

const model = defineModel<string>({ required: true })
</script>

<template>
  <label class="flex items-center gap-1">
    <span class="text-t-fg-dark text-xs">{{ label }}</span>
    <select
      v-model="model"
      class="bg-t-bg-dark border-t-border text-t-fg hover:border-t-terminal focus:border-t-blue cursor-pointer appearance-none border px-2 py-0.5 text-xs outline-none"
    >
      <option value="">all</option>
      <option v-for="opt in options" :key="opt.value" :value="opt.value">
        {{ opt.label }}
      </option>
    </select>
  </label>
</template>
