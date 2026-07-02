<script setup lang="ts">
import { computed } from 'vue'
import type { SrvlogEvent } from '@/types/srvlog'
import type { NetlogEvent } from '@/types/netlog'
import { severityBorderClass, severityColorClass } from '@/lib/constants'
import { highlight } from '@/lib/highlighter'
import { formatDateTime } from '@/lib/format'
import { selectedRowsText } from '@/lib/copy'

// Shared srvlog/netlog inline detail panel. The two feeds are identical except
// for the filter store and route names, which arrive as props from the thin
// per-feed wrappers (SrvlogDetail, NetlogDetail).
type LogEvent = SrvlogEvent | NetlogEvent

const props = defineProps<{
  event: LogEvent
  filterStore: { filters: Record<'programname' | 'facility' | 'severity_max', string> }
  detailRoute: string
  deviceRoute: string
}>()

const filterStore = props.filterStore

interface Field {
  label: string
  key: keyof LogEvent
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

function onCopy(e: ClipboardEvent) {
  const container = e.currentTarget as Element | null
  if (!container) return
  const text = selectedRowsText(container, window.getSelection())
  if (text == null) return
  e.preventDefault()
  e.clipboardData?.setData('text/plain', text)
}

function rowCopyText(field: Field): string {
  return `${field.label}: ${props.event[field.key] ?? '–'}`
}

function fieldValue(field: Field): string {
  const val = props.event[field.key]
  if (field.key === 'received_at' && typeof val === 'string') return formatDateTime(val)
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
      :to="{ name: props.detailRoute, params: { id: event.id } }"
      title="Open detail page"
      aria-label="Open detail page"
      class="absolute right-2 top-1.5 inline-flex items-center justify-center rounded border border-t-fuchsia/30 bg-t-fuchsia/10 p-1 text-t-fuchsia transition-colors hover:bg-t-fuchsia/20 hover:brightness-125"
      @click.stop
    >
      <svg
        xmlns="http://www.w3.org/2000/svg"
        class="h-5 w-5"
        viewBox="0 0 24 24"
        fill="none"
        stroke="currentColor"
        stroke-width="2"
        stroke-linecap="round"
        stroke-linejoin="round"
        aria-hidden="true"
      >
        <path d="M21 5H3" />
        <path d="M10 12H3" />
        <path d="M10 19H3" />
        <circle cx="17" cy="15" r="3" />
        <path d="m21 19-1.9-1.9" />
      </svg>
    </RouterLink>

    <!-- fields -->
    <div
      v-for="field in fields"
      :key="field.key"
      class="flex gap-2 py-0.5 text-sm"
      :data-copytext="rowCopyText(field)"
    >
      <span class="text-t-fg-dark w-24 shrink-0 text-right">{{ field.label }}</span>
      <RouterLink
        v-if="field.key === 'hostname'"
        :to="{ name: props.deviceRoute, params: { hostname: event.hostname } }"
        class="min-w-0 break-all hover:underline"
        :class="fieldColor(field)"
        @click.stop
      >
        {{ event.hostname || '–' }} →
      </RouterLink>
      <button
        v-else-if="field.filter"
        class="min-w-0 break-all text-left cursor-pointer hover:underline"
        :class="fieldColor(field)"
        @click.stop="applyFilter(field)"
      >
        {{ fieldValue(field) }}
      </button>
      <span v-else class="min-w-0 break-all" :class="fieldColor(field)">{{
        fieldValue(field)
      }}</span>
    </div>

    <!-- message -->
    <div class="flex gap-2 py-0.5 text-sm" :data-copytext="`message: ${event.message}`">
      <span class="text-t-fg-dark w-24 shrink-0 text-right">message</span>
      <span class="text-t-fg min-w-0 break-all font-mono text-xs" v-html="highlightedMsg"></span>
    </div>
  </div>
</template>
