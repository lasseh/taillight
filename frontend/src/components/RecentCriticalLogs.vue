<script setup lang="ts">
import { RouterLink } from 'vue-router'
import type { SrvlogEvent } from '@/types/srvlog'
import { severityColorClassByLabel, severityBgClass, severityBgClassByLabel } from '@/lib/constants'
import { formatTime } from '@/lib/format'
import { highlightMessage } from '@/lib/highlighter'

const props = withDefaults(defineProps<{
  events: SrvlogEvent[]
  title?: string
  showHostname?: boolean
  flashIds?: Set<number>
  highlightSeverity?: boolean
  hideHeader?: boolean
  routeName?: string
}>(), {
  routeName: 'srvlog-detail',
})
</script>

<template>
  <div class="bg-t-bg-dark border-t-border rounded border">
    <h3 v-if="!hideHeader" class="text-t-fg-dark border-t-border border-b px-4 py-1.5 text-xs font-semibold uppercase tracking-wide">{{ title ?? 'Recent High-Severity' }}</h3>
    <div>
      <div v-if="events.length === 0" class="text-t-fg-dark px-4 py-2 text-center text-xs">
        No recent high-severity events (emerg, alert, crit)
      </div>
      <!-- Mobile: color bar + hostname + message -->
      <RouterLink
        v-for="event in events"
        :key="'m-' + event.id"
        :to="{ name: props.routeName, params: { id: event.id } }"
        class="hover:bg-t-bg-hover flex gap-2 py-1 pr-2 md:hidden"
        :class="[
          flashIds?.has(event.id) ? 'row-flash' : '',
          highlightSeverity ? (severityBgClass[event.severity] ?? '') : '',
        ]"
      >
        <div class="w-[3px] shrink-0 rounded-r" :class="severityBgClassByLabel[event.severity_label] ?? 'bg-sev-info'" />
        <div class="min-w-0 flex-1">
          <div v-if="showHostname" class="text-t-teal/60 truncate text-[10px] leading-tight">{{ event.hostname }}</div>
          <div class="min-w-0 truncate text-xs leading-snug" v-html="highlightMessage(event.id, event.message)" />
        </div>
      </RouterLink>
      <!-- Desktop: single-line layout -->
      <RouterLink
        v-for="event in events"
        :key="'d-' + event.id"
        :to="{ name: props.routeName, params: { id: event.id } }"
        class="hover:bg-t-bg-hover hidden cursor-pointer items-baseline gap-3 px-4 py-px leading-snug md:flex"
        :class="[
          flashIds?.has(event.id) ? 'row-flash' : '',
          highlightSeverity ? (severityBgClass[event.severity] ?? '') : '',
        ]"
      >
        <span class="text-t-fg-dark w-[8ch] shrink-0">{{ formatTime(event.received_at) }}</span>
        <span class="w-[8ch] shrink-0 uppercase" :class="severityColorClassByLabel[event.severity_label] ?? 'text-t-fg'">{{ event.severity_label }}</span>
        <span v-if="showHostname" class="text-t-teal w-[20ch] shrink-0 truncate">{{ event.hostname }}</span>
        <span class="text-t-purple w-[10ch] shrink-0 truncate">{{ event.programname }}</span>
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
