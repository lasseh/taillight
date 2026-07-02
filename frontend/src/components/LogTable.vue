<script setup lang="ts">
import { computed, type Component } from 'vue'
import { useColumnVisibility } from '@/composables/useColumnVisibility'
import EventTable from '@/components/EventTable.vue'

// Shared srvlog/netlog live table. The two feeds are identical except for the
// event store and route/row identifiers, which arrive as props from the thin
// per-feed wrappers (SrvlogTable, NetlogTable).
interface LogEventStore {
  events: { id: number; hostname: string; programname: string }[]
  loading: boolean
  error: string | null
  hasMore: boolean
  atCap: boolean
  loadHistory: (reset: boolean, wrapMerge?: (mutate: () => void) => void) => void
  reattach: () => void
}

const props = defineProps<{
  store: LogEventStore
  routeName: string
  row: Component
}>()

const events = props.store
const { visible: showProgram } = useColumnVisibility(props.routeName, 'program')

const colWidths = computed(() => {
  let maxHost = 0
  let maxProg = 0
  for (const e of events.events) {
    if (e.hostname.length > maxHost) maxHost = e.hostname.length
    if (e.programname.length > maxProg) maxProg = e.programname.length
  }
  return {
    '--col-host': `${Math.min(20, Math.max(8, maxHost + 1))}ch`,
    '--col-prog': showProgram.value ? `${Math.min(16, Math.max(6, maxProg + 1))}ch` : '0',
  }
})
</script>

<template>
  <EventTable
    :route-name="props.routeName"
    :events="events.events"
    :loading="events.loading"
    :error="events.error"
    :has-more="events.hasMore"
    :at-cap="events.atCap"
    :load-history="events.loadHistory"
    :reattach="events.reattach"
    :style="colWidths"
  >
    <template #default="{ item }">
      <component :is="props.row" :event="item" :show-program="showProgram" />
    </template>
  </EventTable>
</template>
