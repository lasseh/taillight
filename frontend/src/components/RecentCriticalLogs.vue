<script setup lang="ts">
import { RouterLink } from 'vue-router'
import type { SyslogEvent } from '@/types/syslog'
import { severityColorClassByLabel, severityBgClass } from '@/lib/constants'
import { formatTime } from '@/lib/format'
import { highlightMessage } from '@/lib/highlighter'

defineProps<{
  events: SyslogEvent[]
  title?: string
  showHostname?: boolean
  flashIds?: Set<number>
  highlightSeverity?: boolean
  hideHeader?: boolean
}>()
</script>

<template>
  <div class="bg-t-bg-dark border-t-border rounded border">
    <h3 v-if="!hideHeader" class="text-t-fg-dark border-t-border border-b px-4 py-1.5 text-xs font-semibold uppercase tracking-wide">{{ title ?? 'Recent High-Severity' }}</h3>
    <div>
      <div v-if="events.length === 0" class="text-t-fg-dark px-4 py-2 text-center text-xs">
        No recent high-severity events (emerg, alert, crit)
      </div>
      <RouterLink
        v-for="event in events"
        :key="event.id"
        :to="{ name: 'syslog-detail', params: { id: event.id } }"
        class="hover:bg-t-bg-hover flex cursor-pointer items-baseline gap-3 px-4 py-px leading-snug"
        :class="[
          flashIds?.has(event.id) ? 'row-flash' : '',
          highlightSeverity ? (severityBgClass[event.severity] ?? '') : '',
        ]"
      >
        <span class="text-t-fg-dark w-[8ch] shrink-0">{{ formatTime(event.received_at) }}</span>
        <span class="w-[8ch] shrink-0 uppercase" :class="severityColorClassByLabel[event.severity_label] ?? 'text-t-fg'">{{ event.severity_label }}</span>
        <span v-if="showHostname" class="text-t-teal hidden w-[20ch] shrink-0 truncate md:inline">{{ event.hostname }}</span>
        <span class="text-t-purple hidden w-[10ch] shrink-0 truncate md:inline">{{ event.programname }}</span>
        <span class="min-w-0 flex-1 truncate" v-html="highlightMessage(event.id, event.message)" />
      </RouterLink>
    </div>
  </div>
</template>

<style scoped>
.row-flash {
  animation: row-flash 1s ease-out;
}

@keyframes row-flash {
  0% { background-color: var(--color-t-bg-highlight); }
  100% { background-color: transparent; }
}
</style>
