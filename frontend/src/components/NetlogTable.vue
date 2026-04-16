<script setup lang="ts">
import { computed } from 'vue'
import { useNetlogEventStore } from '@/stores/netlog-events'
import { useColumnVisibility } from '@/composables/useColumnVisibility'
import EventTable from '@/components/EventTable.vue'
import NetlogRow from '@/components/NetlogRow.vue'

const events = useNetlogEventStore()
const { visible: showProgram } = useColumnVisibility('netlog', 'program')

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
    route-name="netlog"
    :events="events.events"
    :loading="events.loading"
    :error="events.error"
    :has-more="events.hasMore"
    :load-history="events.loadHistory"
    :style="colWidths"
  >
    <template #default="{ item }">
      <NetlogRow :event="item" :show-program="showProgram" />
    </template>
  </EventTable>
</template>
