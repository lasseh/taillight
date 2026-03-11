<script setup lang="ts">
import { computed } from 'vue'
import type { SyslogEvent } from '@/types/syslog'
import { severityBorderClass, severityColorClass } from '@/lib/constants'
import { highlight } from '@/lib/highlighter'
import { formatTime } from '@/lib/format'
import { useSyslogFilterStore } from '@/stores/syslog-filters'

const props = defineProps<{
  event: SyslogEvent
}>()

const filterStore = useSyslogFilterStore()

interface Field {
  label: string
  key: keyof SyslogEvent
  color?: string
  filter?: string // filter store key to set on click
}

const fields: Field[] = [
  { label: 'time', key: 'received_at' },
  { label: 'hostname', key: 'hostname', color: 'text-t-teal' },
  { label: 'ip', key: 'fromhost_ip', color: 'text-t-blue' },
  { label: 'program', key: 'programname', color: 'text-t-purple', filter: 'programname' },
  { label: 'msgid', key: 'msgid' },
  { label: 'severity', key: 'severity_label', filter: 'severity_max' },
  { label: 'facility', key: 'facility_label', color: 'text-t-orange', filter: 'facility' },
]

const borderClass = severityBorderClass[props.event.severity] ?? 'border-t-border'
const sevClass = severityColorClass[props.event.severity] ?? 'text-t-fg'

const highlightedMsg = computed(() => highlight(props.event.message))
const highlightedRaw = computed(() =>
  props.event.raw_message ? highlight(props.event.raw_message) : '',
)

const copyText = computed(() => {
  const lines = fields.map((f) => `${f.label}: ${props.event[f.key] ?? '–'}`)
  lines.push(`message: ${props.event.message}`)
  if (props.event.raw_message) lines.push(`raw message: ${props.event.raw_message}`)
  return lines.join('\n')
})

function onCopy(e: ClipboardEvent) {
  const sel = window.getSelection()?.toString() ?? ''
  if (!sel.includes('\n')) return // single field: use browser default
  e.preventDefault()
  e.clipboardData?.setData('text/plain', copyText.value)
}

function fieldValue(field: Field): string {
  const val = props.event[field.key]
  if (field.key === 'received_at' && typeof val === 'string') return formatTime(val)
  return String(val ?? '–')
}

function fieldColor(field: Field): string {
  if (field.key === 'severity_label') return sevClass
  return field.color ?? 'text-t-fg'
}

function applyFilter(field: Field) {
  if (!field.filter) return
  if (field.filter === 'severity_max') {
    filterStore.filters.severity_max = String(props.event.severity)
  } else if (field.filter === 'facility') {
    filterStore.filters.facility = String(props.event.facility)
  } else {
    const value = props.event[field.key]
    if (value != null) {
      ;(filterStore.filters as Record<string, string>)[field.filter] = String(value)
    }
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
      :to="{ name: 'syslog-detail', params: { id: event.id } }"
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
        v-if="field.key === 'hostname'"
        :to="{ name: 'device-detail', params: { hostname: event.hostname } }"
        class="min-w-0 break-all hover:underline" :class="fieldColor(field)"
        @click.stop
      >
        {{ event.hostname || '–' }} →
      </RouterLink>
      <button
        v-else-if="field.filter"
        class="min-w-0 break-all text-left cursor-pointer hover:underline" :class="fieldColor(field)"
        @click.stop="applyFilter(field)"
      >
        {{ fieldValue(field) }}
      </button>
      <span v-else class="min-w-0 break-all" :class="fieldColor(field)">{{ fieldValue(field) }}</span>
    </div>

    <!-- message -->
    <div class="flex gap-2 py-0.5 text-sm">
      <span class="text-t-fg-dark w-24 shrink-0 text-right">message</span>
      <span class="text-t-fg min-w-0 break-all font-mono text-xs" v-html="highlightedMsg"></span>
    </div>

    <!-- raw message -->
    <div v-if="event.raw_message" class="flex gap-2 py-0.5 text-sm">
      <span class="text-t-fg-dark w-24 shrink-0 text-right">raw message</span>
      <span class="text-t-fg min-w-0 break-all font-mono text-xs" v-html="highlightedRaw"></span>
    </div>
  </div>
</template>
