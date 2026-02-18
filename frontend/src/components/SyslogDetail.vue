<script setup lang="ts">
import { computed } from 'vue'
import type { SyslogEvent } from '@/types/syslog'
import { severityBorderClass, severityColorClass } from '@/lib/constants'
import { highlight } from '@/lib/highlighter'

const props = defineProps<{
  event: SyslogEvent
}>()

const fields: { label: string; key: keyof SyslogEvent; color?: string }[] = [
  { label: 'received', key: 'received_at' },
  { label: 'reported', key: 'reported_at' },
  { label: 'hostname', key: 'hostname', color: 'text-t-teal' },
  { label: 'ip', key: 'fromhost_ip', color: 'text-t-blue' },
  { label: 'program', key: 'programname', color: 'text-t-purple' },
  { label: 'msgid', key: 'msgid' },
  { label: 'severity', key: 'severity_label' },
  { label: 'facility', key: 'facility_label', color: 'text-t-orange' },
  { label: 'tag', key: 'syslogtag' },
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
  e.preventDefault()
  e.clipboardData?.setData('text/plain', copyText.value)
}

function fieldColor(field: (typeof fields)[number]): string {
  if (field.key === 'severity_label') return sevClass
  return field.color ?? 'text-t-fg'
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
        {{ event.hostname || '–' }}
      </RouterLink>
      <span v-else class="min-w-0 break-all" :class="fieldColor(field)">{{ event[field.key] ?? '–' }}</span>
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
