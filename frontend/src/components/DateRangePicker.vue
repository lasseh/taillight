<script setup lang="ts">
import { computed } from 'vue'
import { VueDatePicker } from '@vuepic/vue-datepicker'
import '@vuepic/vue-datepicker/dist/main.css'
import { useTheme } from '@/composables/useTheme'

const props = defineProps<{
  from: string
  to: string
}>()

const emit = defineEmits<{
  'update:from': [value: string]
  'update:to': [value: string]
}>()

const { isDark } = useTheme()

const hasRange = computed(() => Boolean(props.from || props.to))

const dateRange = computed({
  get: (): string[] | null => {
    if (!props.from && !props.to) return null
    return [props.from || '', props.to || '']
  },
  set: (val: string[] | null) => {
    if (!val || !Array.isArray(val)) {
      emit('update:from', '')
      emit('update:to', '')
      return
    }
    const [start, end] = val
    emit('update:from', start || '')
    emit('update:to', end || '')
  },
})

function formatRange(from: string, to: string): string {
  const fmt = (iso: string) => {
    const d = new Date(iso)
    const month = String(d.getMonth() + 1).padStart(2, '0')
    const day = String(d.getDate()).padStart(2, '0')
    const hours = String(d.getHours()).padStart(2, '0')
    const mins = String(d.getMinutes()).padStart(2, '0')
    return `${month}/${day} ${hours}:${mins}`
  }
  if (from && to) return `${fmt(from)} – ${fmt(to)}`
  if (from) return `from ${fmt(from)}`
  if (to) return `until ${fmt(to)}`
  return 'all time'
}

function clear() {
  emit('update:from', '')
  emit('update:to', '')
}
</script>

<template>
  <div class="relative flex items-center gap-1">
    <span class="text-t-fg-dark text-xs">time</span>
    <VueDatePicker
      v-model="dateRange"
      range
      enable-time-picker
      auto-apply
      model-type="iso"
      :max-date="new Date()"
      :dark="isDark"
      teleport="body"
      :enable-seconds="false"
    >
      <template #dp-input="{}">
        <button
          type="button"
          aria-label="Filter by time range"
          class="bg-t-bg-dark border-t-border cursor-pointer border px-2 py-0.5 text-left text-xs transition-colors"
          :class="hasRange ? 'border-t-blue text-t-blue' : 'text-t-fg hover:border-t-terminal'"
        >
          {{ formatRange(from, to) }}
        </button>
      </template>
    </VueDatePicker>
    <button
      v-if="hasRange"
      type="button"
      aria-label="Clear time range"
      class="text-t-fg-dark hover:text-t-red text-xs"
      @click="clear"
    >
      ✕
    </button>
  </div>
</template>

<style>
.dp__theme_dark {
  --dp-background-color: var(--color-t-bg-dark);
  --dp-text-color: var(--color-t-fg);
  --dp-hover-color: var(--color-t-bg-hover);
  --dp-hover-text-color: var(--color-t-fg);
  --dp-primary-color: var(--color-t-blue);
  --dp-primary-text-color: #fff;
  --dp-secondary-color: var(--color-t-fg-dark);
  --dp-border-color: var(--color-t-border);
  --dp-menu-border-color: var(--color-t-border);
  --dp-border-color-hover: var(--color-t-terminal);
  --dp-disabled-color: var(--color-t-bg-highlight);
  --dp-highlight-color: var(--color-t-blue);
  --dp-range-between-dates-background-color: color-mix(in srgb, var(--color-t-blue) 15%, transparent);
  --dp-range-between-dates-text-color: var(--color-t-fg);
  --dp-range-between-border-color: color-mix(in srgb, var(--color-t-blue) 30%, transparent);
}

.dp__theme_light {
  --dp-background-color: var(--color-t-bg);
  --dp-text-color: var(--color-t-fg);
  --dp-hover-color: var(--color-t-bg-hover);
  --dp-hover-text-color: var(--color-t-fg);
  --dp-primary-color: var(--color-t-blue);
  --dp-primary-text-color: #fff;
  --dp-secondary-color: var(--color-t-fg-dark);
  --dp-border-color: var(--color-t-border);
  --dp-menu-border-color: var(--color-t-border);
  --dp-border-color-hover: var(--color-t-terminal);
  --dp-disabled-color: var(--color-t-bg-highlight);
  --dp-highlight-color: var(--color-t-blue);
  --dp-range-between-dates-background-color: color-mix(in srgb, var(--color-t-blue) 15%, transparent);
  --dp-range-between-dates-text-color: var(--color-t-fg);
  --dp-range-between-border-color: color-mix(in srgb, var(--color-t-blue) 30%, transparent);
}
</style>
