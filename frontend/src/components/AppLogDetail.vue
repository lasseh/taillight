<script setup lang="ts">
import { computed } from 'vue'
import type { AppLogEvent } from '@/types/applog'
import { levelBorderClass, levelColorClass } from '@/lib/applog-constants'
import { highlightAttrs } from '@/lib/format'
import { useAppLogFilterStore } from '@/stores/applog-filters'

const props = defineProps<{
  event: AppLogEvent
}>()

const filterStore = useAppLogFilterStore()

interface Field {
  label: string
  key: keyof AppLogEvent
  color?: string
  filter?: string // filter store key to set on click
}

const fields: Field[] = [
  { label: 'received', key: 'received_at' },
  { label: 'timestamp', key: 'timestamp' },
  { label: 'level', key: 'level', filter: 'level' },
  { label: 'host', key: 'host', color: 'text-t-teal' },
  { label: 'service', key: 'service', color: 'text-t-purple', filter: 'service' },
  { label: 'component', key: 'component', color: 'text-t-yellow', filter: 'component' },
  { label: 'source', key: 'source', color: 'text-t-blue' },
]

const borderClass = levelBorderClass[props.event.level] ?? 'border-t-border'
const lvlClass = levelColorClass[props.event.level] ?? 'text-t-fg'

const copyText = computed(() => {
  const lines = fields.map((f) => `${f.label}: ${props.event[f.key] ?? '–'}`)
  lines.push(`message: ${props.event.msg}`)
  if (props.event.attrs && Object.keys(props.event.attrs).length > 0)
    lines.push(`attrs: ${JSON.stringify(props.event.attrs, null, 2)}`)
  return lines.join('\n')
})

function onCopy(e: ClipboardEvent) {
  const sel = window.getSelection()?.toString() ?? ''
  if (!sel.includes('\n')) return // single field: use browser default
  e.preventDefault()
  e.clipboardData?.setData('text/plain', copyText.value)
}

function fieldColor(field: Field): string {
  if (field.key === 'level') return lvlClass
  return field.color ?? 'text-t-fg'
}

function applyFilter(field: Field) {
  if (!field.filter) return
  const value = props.event[field.key]
  if (value != null) {
    ;(filterStore.filters as Record<string, string>)[field.filter] = String(value)
  }
}
</script>

<template>
  <div
    class="bg-t-bg-dark relative border mx-2 my-1 rounded py-1.5 pl-4 pr-4"
    :class="borderClass"
    @copy="onCopy"
  >
    <!-- permalink -->
    <RouterLink
      :to="{ name: 'applog-detail', params: { id: event.id } }"
      class="absolute right-3 top-1.5 text-xs font-normal leading-none text-t-purple transition-all hover:font-extrabold hover:brightness-125"
      title="permalink"
      @click.stop
    >
      Details
    </RouterLink>

    <!-- fields -->
    <div
      v-for="field in fields"
      :key="field.key"
      class="flex gap-2 py-0.5 text-sm"
    >
      <span class="text-t-fg-dark w-24 shrink-0 text-right">{{ field.label }}</span>
      <RouterLink
        v-if="field.key === 'host' && event.host"
        :to="{ name: 'applog-device-detail', params: { hostname: event.host } }"
        class="min-w-0 break-all hover:underline"
        :class="fieldColor(field)"
        @click.stop
      >
        {{ event.host }} →
      </RouterLink>
      <button
        v-else-if="field.filter"
        class="min-w-0 break-all text-left cursor-pointer hover:underline" :class="fieldColor(field)"
        @click.stop="applyFilter(field)"
      >
        {{ event[field.key] ?? '–' }}
      </button>
      <span v-else class="min-w-0 break-all" :class="fieldColor(field)">{{ event[field.key] ?? '–' }}</span>
    </div>

    <!-- message -->
    <div class="flex gap-2 py-0.5 text-sm">
      <span class="text-t-fg-dark w-24 shrink-0 text-right">message</span>
      <span class="text-t-fg min-w-0 break-all font-mono text-xs">{{ event.msg }}</span>
    </div>

    <!-- attrs -->
    <div v-if="event.attrs && Object.keys(event.attrs).length > 0" class="flex gap-2 py-0.5 text-sm">
      <span class="text-t-fg-dark w-24 shrink-0 text-right">attrs</span>
      <pre class="language-json text-t-fg min-w-0 break-all font-mono text-xs" v-html="highlightAttrs(event.attrs)"></pre>
    </div>
  </div>
</template>
