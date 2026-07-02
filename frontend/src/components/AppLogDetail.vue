<script setup lang="ts">
import { computed, ref, watch } from 'vue'
import type { AppLogEvent } from '@/types/applog'
import { levelBorderClass, levelColorClass } from '@/lib/applog-constants'
import { formatDateTime, highlightAttrs } from '@/lib/format'
import { selectedRowsText } from '@/lib/copy'
import { useAppLogFilterStore } from '@/stores/applog-filters'
import { api } from '@/lib/api'

const props = defineProps<{
  event: AppLogEvent
}>()

const filterStore = useAppLogFilterStore()

// When the row arrived with attrs stripped (oversized blob held back by the
// list/SSE preview), fetch the full event so the detail panel can show it.
const fullAttrs = ref<Record<string, unknown> | null>(null)
const attrsLoading = ref(false)

const displayAttrs = computed<Record<string, unknown> | null>(() =>
  props.event.attrs_truncated ? fullAttrs.value : props.event.attrs,
)

watch(
  () => [props.event.id, props.event.attrs_truncated] as const,
  ([id, truncated]) => {
    fullAttrs.value = null
    if (!truncated) return
    attrsLoading.value = true
    api
      .getAppLog(id)
      .then((res) => {
        if (props.event.id === id) fullAttrs.value = res.data.attrs
      })
      .catch(() => {})
      .finally(() => {
        attrsLoading.value = false
      })
  },
  { immediate: true },
)

interface Field {
  label: string
  key: keyof AppLogEvent
  color?: string
  filter?: string // filter store key to set on click
}

const fields: Field[] = [
  { label: 'time', key: 'received_at' },
  { label: 'level', key: 'level', filter: 'level' },
  { label: 'host', key: 'host', color: 'text-t-teal' },
  { label: 'service', key: 'service', color: 'text-t-purple', filter: 'service' },
  { label: 'component', key: 'component', color: 'text-t-yellow', filter: 'component' },
  { label: 'source', key: 'source', color: 'text-t-blue' },
]

const borderClass = levelBorderClass[props.event.level] ?? 'border-t-border'
const lvlClass = levelColorClass[props.event.level] ?? 'text-t-fg'

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

function attrsCopyText(): string {
  return `attrs: ${JSON.stringify(displayAttrs.value, null, 2)}`
}

function fieldValue(field: Field): string {
  const val = props.event[field.key]
  if (field.key === 'received_at' && typeof val === 'string') return formatDateTime(val)
  return String(val ?? '–')
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
    <div class="flex gap-2 py-0.5 text-sm" :data-copytext="`message: ${event.msg}`">
      <span class="text-t-fg-dark w-24 shrink-0 text-right">message</span>
      <span class="text-t-fg min-w-0 break-all font-mono text-xs">{{ event.msg }}</span>
    </div>

    <!-- attrs -->
    <div v-if="attrsLoading" class="flex gap-2 py-0.5 text-sm">
      <span class="text-t-fg-dark w-24 shrink-0 text-right">attrs</span>
      <span class="text-t-fg-dark min-w-0 font-mono text-xs italic">loading…</span>
    </div>
    <div
      v-else-if="displayAttrs && Object.keys(displayAttrs).length > 0"
      class="flex gap-2 py-0.5 text-sm"
      :data-copytext="attrsCopyText()"
    >
      <span class="text-t-fg-dark w-24 shrink-0 text-right">attrs</span>
      <pre
        class="language-json text-t-fg min-w-0 break-all font-mono text-xs"
        v-html="highlightAttrs(displayAttrs)"
      ></pre>
    </div>
  </div>
</template>
