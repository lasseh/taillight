<script setup lang="ts">
import { RouterLink } from 'vue-router'
import type { AppLogEvent } from '@/types/applog'
import { levelColorClass, levelBgClass } from '@/lib/applog-constants'
import { formatTime, formatAttrs } from '@/lib/format'

defineProps<{
  events: AppLogEvent[]
  title?: string
  flashIds?: Set<number>
  highlightLevel?: boolean
  hideHeader?: boolean
}>()
</script>

<template>
  <div class="bg-t-bg-dark border-t-border rounded border">
    <h3 v-if="!hideHeader" class="text-t-fg-dark border-t-border border-b px-4 py-1.5 text-xs font-semibold uppercase tracking-wide">{{ title ?? 'Recent Logs' }}</h3>
    <div>
      <div v-if="events.length === 0" class="text-t-fg-dark px-4 py-2 text-center text-xs">
        No recent events
      </div>
      <RouterLink
        v-for="event in events"
        :key="event.id"
        :to="{ name: 'applog-detail', params: { id: event.id } }"
        class="hover:bg-t-bg-hover flex cursor-pointer items-baseline gap-3 px-4 py-px leading-snug"
        :class="[
          flashIds?.has(event.id) ? 'row-flash' : '',
          highlightLevel ? (levelBgClass[event.level] ?? '') : '',
        ]"
      >
        <span class="text-t-fg-dark w-[8ch] shrink-0">{{ formatTime(event.received_at) }}</span>
        <span class="w-[6ch] shrink-0 uppercase" :class="levelColorClass[event.level] ?? 'text-t-fg'">{{ event.level }}</span>
        <span class="text-t-teal hidden w-[16ch] shrink-0 truncate md:inline">{{ event.host }}</span>
        <span class="text-t-purple hidden w-[12ch] shrink-0 truncate md:inline">{{ event.service }}</span>
        <span class="text-t-yellow hidden w-[10ch] shrink-0 truncate lg:inline">{{ event.component }}</span>
        <span class="min-w-0 flex-1 truncate" :title="event.msg + (event.attrs ? ' ' + formatAttrs(event.attrs) : '')">
          {{ event.msg }}
          <span v-if="event.attrs" class="text-t-fg-dark">{{ formatAttrs(event.attrs) }}</span>
        </span>
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
